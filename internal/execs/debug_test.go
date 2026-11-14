package execs_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type debugEnv struct {
	t       *testing.T
	fakes   *fakekube.Clients
	clock   *clock.Fake
	out     *bytes.Buffer
	dbg     *execs.Debugger
	deps    execs.DebuggerDeps
	updates []*corev1.Pod
	watcher chan *watch.FakeWatcher
}

func debugPod(name string, mutate func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", ResourceVersion: "10"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}, {Name: "side"}}},
	}
	if mutate != nil {
		mutate(p)
	}
	return p
}

func newDebugEnv(t *testing.T, pods ...*corev1.Pod) *debugEnv {
	t.Helper()
	fk := fakekube.New()
	for _, p := range pods {
		_, err := fk.Kube.CoreV1().Pods(p.Namespace).Create(t.Context(), p, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	e := &debugEnv{t: t, fakes: fk, clock: clock.NewFake(t0), out: &bytes.Buffer{}, watcher: make(chan *watch.FakeWatcher, 4)}
	db := storetest.New(t)
	n := 0
	rec := audit.NewRecorder(db, e.clock, e.out, func() string { n++; return fmt.Sprintf("a%d", n) })
	on := true
	e.deps = execs.DebuggerDeps{
		Clients: fk, Clock: e.clock, Audit: rec, Rand: testutil.Rand("debug"),
		Debug:     config.Debug{Image: "debug:1", Shell: "/bin/bash"},
		NodeShell: config.NodeShell{Enabled: &on, Image: "nodeshell:1"},
	}
	fk.Kube.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "ephemeralcontainers" {
			return false, nil, nil
		}
		p := a.(clienttesting.UpdateAction).GetObject().(*corev1.Pod).DeepCopy()
		e.updates = append(e.updates, p)
		require.NoError(t, fk.Kube.Tracker().Update(a.GetResource(), p, a.GetNamespace()))
		return true, p, nil
	})
	fk.Kube.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		w := watch.NewFake()
		e.watcher <- w
		return true, w, nil
	})
	e.dbg = execs.NewDebugger(e.deps)
	return e
}

func (e *debugEnv) rebuild() { e.dbg = execs.NewDebugger(e.deps) }

func running(p *corev1.Pod, name string) *corev1.Pod {
	p = p.DeepCopy()
	p.Status.EphemeralContainerStatuses = append(p.Status.EphemeralContainerStatuses,
		corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	p.ResourceVersion = "11"
	return p
}

// add runs AddContainer and, once the watch exists, tells the pod that the container runs.
func (e *debugEnv) add(req execs.PodRequest, outcome func(w *watch.FakeWatcher, p *corev1.Pod)) (execs.PodResult, error) {
	type res struct {
		r   execs.PodResult
		err error
	}
	done := make(chan res, 1)
	go func() {
		r, err := e.dbg.AddContainer(context.Background(), sess, "c", req)
		done <- res{r, err}
	}()
	if outcome != nil {
		w := testutil.Recv(e.t, e.watcher)
		require.Len(e.t, e.updates, 1)
		outcome(w, e.updates[0])
	}
	r := testutil.Recv(e.t, done)
	return r.r, r.err
}

func okEvent(w *watch.FakeWatcher, p *corev1.Pod) {
	w.Modify(running(p, p.Spec.EphemeralContainers[len(p.Spec.EphemeralContainers)-1].Name))
}

var req = execs.PodRequest{Namespace: "ns", Pod: "web-1", Target: "app"}

func TestDebug_AddsAnEphemeralContainerWithTheGeneralProfile(t *testing.T) {
	e := newDebugEnv(t, debugPod("web-1", nil))
	got, err := e.add(req, okEvent)
	require.NoError(t, err)
	assert.False(t, got.Reused)
	assert.Regexp(t, `^kubelook-debug-[a-z]{5}$`, got.Container)
	require.Len(t, e.updates, 1)
	ec := e.updates[0].Spec.EphemeralContainers[0]
	assert.Equal(t, got.Container, ec.Name)
	assert.Equal(t, "debug:1", ec.Image)
	assert.Equal(t, []string{"/bin/bash"}, ec.Command)
	assert.True(t, ec.Stdin)
	assert.True(t, ec.TTY)
	assert.Equal(t, "app", ec.TargetContainerName)
	assert.Nil(t, ec.SecurityContext)
	assert.Contains(t, e.out.String(), `"verb":"debug-container"`)
	assert.Contains(t, e.out.String(), `"profile":"general"`)
	assert.Contains(t, e.out.String(), got.Container)
	assert.Contains(t, e.out.String(), `"image":"debug:1"`)
}

func TestDebug_ProfilesSetTheSecurityContext(t *testing.T) {
	tests := map[string]func(t *testing.T, sc *corev1.SecurityContext){
		"baseline": func(t *testing.T, sc *corev1.SecurityContext) { assert.Nil(t, sc) },
		"restricted": func(t *testing.T, sc *corev1.SecurityContext) {
			require.NotNil(t, sc)
			assert.True(t, *sc.RunAsNonRoot)
			assert.False(t, *sc.AllowPrivilegeEscalation)
			assert.Equal(t, []corev1.Capability{"ALL"}, sc.Capabilities.Drop)
			assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, sc.SeccompProfile.Type)
		},
		"netadmin": func(t *testing.T, sc *corev1.SecurityContext) {
			assert.Equal(t, []corev1.Capability{"NET_ADMIN", "NET_RAW"}, sc.Capabilities.Add)
		},
		"sysadmin": func(t *testing.T, sc *corev1.SecurityContext) { assert.True(t, *sc.Privileged) },
	}
	for profile, check := range tests {
		t.Run(profile, func(t *testing.T) {
			e := newDebugEnv(t, debugPod("web-1", nil))
			r := req
			r.Profile = profile
			_, err := e.add(r, okEvent)
			require.NoError(t, err)
			check(t, e.updates[0].Spec.EphemeralContainers[0].SecurityContext)
		})
	}
}

func TestDebug_ANamedProfileSelectsTheProfileAndWhetherTheTargetIsSet(t *testing.T) {
	e := newDebugEnv(t, debugPod("web-1", nil))
	e.deps.Debug.Profiles = []config.DebugProfile{{Name: "net", Profile: "netadmin", ShareProcessNamespace: false}}
	e.rebuild()
	r := req
	r.Profile = "net"
	_, err := e.add(r, okEvent)
	require.NoError(t, err)
	ec := e.updates[0].Spec.EphemeralContainers[0]
	assert.Empty(t, ec.TargetContainerName)
	assert.Equal(t, []corev1.Capability{"NET_ADMIN", "NET_RAW"}, ec.SecurityContext.Capabilities.Add)
	assert.Contains(t, e.out.String(), `"profile":"net (netadmin)"`)

	e2 := newDebugEnv(t, debugPod("web-1", nil))
	e2.deps.Debug.Profiles = []config.DebugProfile{{Name: "shared", Profile: "general", ShareProcessNamespace: true}}
	e2.rebuild()
	r.Profile = "shared"
	_, err = e2.add(r, okEvent)
	require.NoError(t, err)
	assert.Equal(t, "app", e2.updates[0].Spec.EphemeralContainers[0].TargetContainerName)
}

func TestDebug_ReusesARunningDebugContainerWithoutAnUpdate(t *testing.T) {
	p := debugPod("web-1", func(p *corev1.Pod) {
		p.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "kubelook-debug-abcde"}}}
		p.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{{Name: "kubelook-debug-abcde", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	})
	e := newDebugEnv(t, p)
	got, err := e.add(req, nil)
	require.NoError(t, err)
	assert.Equal(t, execs.PodResult{Container: "kubelook-debug-abcde", Reused: true}, got)
	assert.Empty(t, e.updates)
	assert.Empty(t, e.out.String(), "reusing is no new action")
}

func TestDebug_ABrokenDebugContainerIsNotReusedAndStaysInThePod(t *testing.T) {
	for _, reason := range []string{"ErrImagePull", "ImagePullBackOff"} {
		p := debugPod("web-1", func(p *corev1.Pod) {
			p.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "kubelook-debug-aaaaa"}}}
			p.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{{Name: "kubelook-debug-aaaaa", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}}
		})
		e := newDebugEnv(t, p)
		got, err := e.add(req, okEvent)
		require.NoError(t, err, reason)
		assert.False(t, got.Reused)
		assert.Len(t, e.updates[0].Spec.EphemeralContainers, 2)
		assert.Equal(t, "kubelook-debug-aaaaa", e.updates[0].Spec.EphemeralContainers[0].Name)
		assert.NotEqual(t, "kubelook-debug-aaaaa", got.Container)
	}
}

func TestDebug_GivesUpAfterSixtySecondsAndNamesTheWaitingReason(t *testing.T) {
	e := newDebugEnv(t, debugPod("web-1", nil))
	_, err := e.add(req, func(w *watch.FakeWatcher, p *corev1.Pod) {
		q := p.DeepCopy()
		q.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{{Name: q.Spec.EphemeralContainers[0].Name, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}
		w.Modify(q)
		e.clock.Advance(59 * time.Second)
		e.clock.Advance(2 * time.Second)
	})
	var api *apperr.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, http.StatusGatewayTimeout, api.Status)
	assert.Equal(t, "The debug container did not start: ImagePullBackOff", api.Message)
	assert.Contains(t, e.out.String(), `"outcome":"failure"`)
}

func TestDebug_RunningAlreadyAfterTheUpdateNeedsNoWatch(t *testing.T) {
	e := newDebugEnv(t, debugPod("web-1", nil))
	e.fakes.Kube.PrependReactor("get", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		if len(e.updates) == 0 {
			return false, nil, nil
		}
		return true, running(e.updates[0], e.updates[0].Spec.EphemeralContainers[0].Name), nil
	})
	got, err := e.add(req, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, got.Container)
}

func TestDebug_Validation(t *testing.T) {
	e := newDebugEnv(t, debugPod("web-1", nil))
	r := req
	r.Profile = "root"
	_, err := e.dbg.AddContainer(t.Context(), sess, "c", r)
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	r = req
	r.Target = "nope"
	_, err = e.dbg.AddContainer(t.Context(), sess, "c", r)
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	r.Target = ""
	_, err = e.dbg.AddContainer(t.Context(), sess, "c", r)
	assert.ErrorIs(t, err, apperr.ErrInvalid, "two containers: the target must be named")
	_, err = e.dbg.AddContainer(t.Context(), sess, "c", execs.PodRequest{Namespace: "ns", Pod: "missing", Target: "app"})
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	e.deps.Debug.Profiles = []config.DebugProfile{{Name: "bad", Profile: "nonsense"}}
	e.rebuild()
	r = req
	r.Profile = "bad"
	_, err = e.dbg.AddContainer(t.Context(), sess, "c", r)
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Empty(t, e.updates)
}

func TestDebug_ASinglecontainerPodNeedsNoTarget(t *testing.T) {
	e := newDebugEnv(t, debugPod("solo", func(p *corev1.Pod) { p.Spec.Containers = p.Spec.Containers[:1] }))
	_, err := e.add(execs.PodRequest{Namespace: "ns", Pod: "solo"}, okEvent)
	require.NoError(t, err)
	assert.Equal(t, "app", e.updates[0].Spec.EphemeralContainers[0].TargetContainerName)
}

func TestDebug_AuditFailureRefusesAndNoUpdateHappens(t *testing.T) {
	e := newDebugEnv(t, debugPod("web-1", nil))
	db := storetest.New(t)
	e.deps.Audit = audit.NewRecorder(db, e.clock, e.out, func() string { return "x" })
	require.NoError(t, db.Close())
	e.rebuild()
	_, err := e.dbg.AddContainer(t.Context(), sess, "c", req)
	assert.ErrorIs(t, err, apperr.ErrNotRecorded)
	assert.Empty(t, e.updates)
}

func TestNodeShell_DisabledIs404(t *testing.T) {
	e := newDebugEnv(t)
	e.deps.NodeShell = config.NodeShell{}
	e.rebuild()
	_, err := e.dbg.CreateNodeShell(t.Context(), sess, "c", "n1")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	assert.ErrorIs(t, e.dbg.DeleteNodeShell(t.Context(), sess, "c", "kubelook-node-shell-aaaaa"), apperr.ErrNotFound)
}

func TestNodeShell_CreatesAPrivilegedHostPodAndWaitsForIt(t *testing.T) {
	e := newDebugEnv(t)
	type res struct {
		r   execs.NodeShell
		err error
	}
	done := make(chan res, 1)
	go func() {
		r, err := e.dbg.CreateNodeShell(context.Background(), sess, "c", "n1")
		done <- res{r, err}
	}()
	w := testutil.Recv(t, e.watcher)
	pods, err := e.fakes.Kube.CoreV1().Pods("default").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, pods.Items, 1)
	pod := pods.Items[0]
	assert.Regexp(t, regexp.MustCompile(`^kubelook-node-shell-[a-z]{5}$`), pod.Name)
	assert.Equal(t, "n1", pod.Spec.NodeName)
	assert.True(t, pod.Spec.HostPID && pod.Spec.HostNetwork && pod.Spec.HostIPC)
	assert.Equal(t, corev1.RestartPolicyNever, pod.Spec.RestartPolicy)
	assert.Equal(t, []corev1.Toleration{{Operator: corev1.TolerationOpExists}}, pod.Spec.Tolerations)
	assert.Equal(t, "kubelook", pod.Labels["app.kubernetes.io/managed-by"])
	c := pod.Spec.Containers[0]
	assert.Equal(t, "nodeshell:1", c.Image)
	assert.Equal(t, []string{"sleep", "infinity"}, c.Command)
	assert.True(t, *c.SecurityContext.Privileged)
	up := pod.DeepCopy()
	up.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "shell", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	w.Modify(up)
	got := testutil.Recv(t, done)
	require.NoError(t, got.err)
	assert.Equal(t, execs.NodeShell{Namespace: "default", Pod: pod.Name, Container: "shell"}, got.r)
	assert.Equal(t, []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "/bin/sh"}, execs.NodeShellCommand)
	assert.Contains(t, e.out.String(), `"node":"n1"`)

	require.NoError(t, e.dbg.DeleteNodeShell(t.Context(), sess, "c", pod.Name))
	_, err = e.fakes.Kube.CoreV1().Pods("default").Get(t.Context(), pod.Name, metav1.GetOptions{})
	assert.Error(t, err)
}

func TestNodeShell_AShellThatNeverStartsIsRemoved(t *testing.T) {
	e := newDebugEnv(t)
	done := make(chan error, 1)
	go func() {
		_, err := e.dbg.CreateNodeShell(context.Background(), sess, "c", "n1")
		done <- err
	}()
	testutil.Recv(t, e.watcher)
	e.clock.Advance(61 * time.Second)
	require.Error(t, testutil.Recv(t, done))
	pods, _ := e.fakes.Kube.CoreV1().Pods("default").List(t.Context(), metav1.ListOptions{})
	assert.Empty(t, pods.Items)
}

func TestNodeShell_ValidationAndDeleteOnlyRemovesOurPods(t *testing.T) {
	foreign := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "kubelook-node-shell-zzzzz", Namespace: "default"}}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default", Labels: map[string]string{"app.kubernetes.io/managed-by": "kubelook"}}}
	e := newDebugEnv(t, foreign, other)
	_, err := e.dbg.CreateNodeShell(t.Context(), sess, "c", "")
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	assert.ErrorIs(t, e.dbg.DeleteNodeShell(t.Context(), sess, "c", "kubelook-node-shell-zzzzz"), apperr.ErrNotFound, "no managed-by label")
	assert.ErrorIs(t, e.dbg.DeleteNodeShell(t.Context(), sess, "c", "web"), apperr.ErrNotFound, "wrong name")
	assert.ErrorIs(t, e.dbg.DeleteNodeShell(t.Context(), sess, "c", "missing"), apperr.ErrNotFound)
}

func TestNodeShell_AuditFailureRefuses(t *testing.T) {
	e := newDebugEnv(t)
	db := storetest.New(t)
	e.deps.Audit = audit.NewRecorder(db, e.clock, e.out, func() string { return "x" })
	require.NoError(t, db.Close())
	e.rebuild()
	_, err := e.dbg.CreateNodeShell(t.Context(), sess, "c", "n1")
	assert.ErrorIs(t, err, apperr.ErrNotRecorded)
	pods, _ := e.fakes.Kube.CoreV1().Pods("default").List(t.Context(), metav1.ListOptions{})
	assert.Empty(t, pods.Items)
}

func TestShell_NodeShellOpensTheNodeCommandOnlyWhenEnabled(t *testing.T) {
	e := newEnv(t)
	e.exec.scripts["nsenter"] = echoUntilEOF(nil)
	raw := []byte(`{"cluster":"c","namespace":"default","pod":"kubelook-node-shell-abcde","container":"shell","nodeShell":true}`)
	_, err := execs.Open(t.Context(), e.deps, e.sink, sess, 1, raw)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	e.deps.NodeShell = true
	ch, err := execs.Open(t.Context(), e.deps, e.sink, sess, 1, raw)
	require.NoError(t, err)
	t.Cleanup(ch.Close)
	assert.Equal(t, "opened", e.next().Type)
	assert.Equal(t, []string{"nsenter"}, e.exec.commands())
}
