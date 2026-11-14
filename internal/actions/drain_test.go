package actions_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/testutil"
)

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

func typedPod(ns, name, nodeName string, mut ...func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, UID: "uid-" + ns + "-" + name},
		Spec:       corev1.PodSpec{NodeName: nodeName},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, m := range mut {
		m(p)
	}
	return p
}

func ownedByDaemonSet(p *corev1.Pod) {
	yes := true
	p.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "agent", UID: "ds", Controller: &yes}}
}

func mirror(p *corev1.Pod) { p.Annotations = map[string]string{"kubernetes.io/config.mirror": "abc"} }

func phase(ph corev1.PodPhase) func(*corev1.Pod) { return func(p *corev1.Pod) { p.Status.Phase = ph } }

func emptyDir(p *corev1.Pod) {
	p.Spec.Volumes = []corev1.Volume{{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
}

// drainEnv is a cluster with a node, pods on it, and an eviction API that deletes the evicted pod.
type drainEnv struct {
	*env
	mu        sync.Mutex
	order     []string // "cordon", "evict ns/name" in the order the API saw them
	evictions chan string // every successful eviction request
	tries     chan string // every eviction request, refused or not
	watches   chan struct{}
	evictErr  func(name string, n int) error
	attempts  map[string]int
	autoGone  bool
}

func newDrainEnv(t *testing.T, pods ...*corev1.Pod) *drainEnv {
	t.Helper()
	d := &drainEnv{env: newEnv(t), evictions: make(chan string, 64), tries: make(chan string, 64), watches: make(chan struct{}, 64), attempts: map[string]int{}, autoGone: true}
	tr := d.fakes.Kube.Tracker()
	for _, n := range []string{"node-1", "node-2"} {
		require.NoError(t, tr.Add(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: n}}))
	}
	for _, p := range pods {
		require.NoError(t, tr.Add(p))
	}
	d.fakes.Kube.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		d.watches <- struct{}{}
		return false, nil, nil
	})
	d.fakes.Kube.PrependReactor("patch", "nodes", func(a clienttesting.Action) (bool, runtime.Object, error) {
		d.mu.Lock()
		d.order = append(d.order, "cordon "+a.(clienttesting.PatchAction).GetName())
		d.mu.Unlock()
		return false, nil, nil
	})
	d.fakes.Kube.PrependReactor("create", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		ev := a.(clienttesting.CreateAction).GetObject().(*policyv1.Eviction)
		key := ev.Namespace + "/" + ev.Name
		d.mu.Lock()
		d.order = append(d.order, "evict "+key)
		d.attempts[key]++
		n := d.attempts[key]
		d.mu.Unlock()
		d.tries <- key
		if d.evictErr != nil {
			if err := d.evictErr(ev.Name, n); err != nil {
				return true, nil, err
			}
		}
		d.evictions <- key
		if d.autoGone {
			_ = tr.Delete(podsGVR, ev.Namespace, ev.Name)
		}
		return true, ev, nil
	})
	return d
}

func (d *drainEnv) log() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.order...)
}

func (d *drainEnv) drainer() *actions.Drainer {
	return actions.NewDrainer(actions.Deps{Clients: d.fakes, Clock: d.clock, Audit: d.audit})
}

func TestPodFilter_Table(t *testing.T) {
	for name, tc := range map[string]struct {
		pod    *corev1.Pod
		evict  bool
		reason string
	}{
		"normal":            {typedPod("ns", "p", "n"), true, ""},
		"emptyDir":          {typedPod("ns", "p", "n", emptyDir), true, ""},
		"daemonset":         {typedPod("ns", "p", "n", ownedByDaemonSet), false, "DaemonSet"},
		"mirror":            {typedPod("ns", "p", "n", mirror), false, "mirror"},
		"succeeded":         {typedPod("ns", "p", "n", phase(corev1.PodSucceeded)), false, "finished"},
		"failed":            {typedPod("ns", "p", "n", phase(corev1.PodFailed)), false, "finished"},
		"pending":           {typedPod("ns", "p", "n", phase(corev1.PodPending)), true, ""},
		"owned by a rs":     {typedPod("ns", "p", "n", func(p *corev1.Pod) { p.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "r"}} }), true, ""},
		"daemonset by name": {typedPod("ns", "p", "n", func(p *corev1.Pod) { p.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "d"}} }), false, "DaemonSet"},
	} {
		evict, reason := actions.PodFilter(tc.pod)
		assert.Equal(t, tc.evict, evict, name)
		if tc.reason != "" {
			assert.Contains(t, reason, tc.reason, name)
		}
	}
}

func TestDrain_CordonsFirst_ThenEvictsOnlyWhatPodFilterAllows(t *testing.T) {
	d := newDrainEnv(t,
		typedPod("ns", "b-web", "node-1"),
		typedPod("ns", "a-web", "node-1", emptyDir),
		typedPod("kube-system", "agent", "node-1", ownedByDaemonSet),
		typedPod("kube-system", "etcd", "node-1", mirror),
		typedPod("ns", "done", "node-1", phase(corev1.PodSucceeded)),
		typedPod("ns", "crashed", "node-1", phase(corev1.PodFailed)),
		typedPod("ns", "elsewhere", "node-2"),
	)
	var seenSelector atomic.Value
	d.fakes.Kube.PrependReactor("list", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		seenSelector.Store(a.(clienttesting.ListAction).GetListRestrictions().Fields.String())
		return false, nil, nil
	})
	var reports []jobs.Progress
	require.NoError(t, d.drainer().Drain(t.Context(), sess, "c", "node-1", func(p jobs.Progress) { reports = append(reports, p) }))

	log := d.log()
	require.Len(t, log, 3)
	assert.Equal(t, "cordon node-1", log[0], "the node is cordoned before any pod is evicted")
	assert.ElementsMatch(t, []string{"evict ns/a-web", "evict ns/b-web"}, log[1:])
	assert.Equal(t, "spec.nodeName=node-1", seenSelector.Load())
	require.NotEmpty(t, reports)
	assert.Equal(t, jobs.Progress{Done: 2, Total: 2, Text: "Evicted 2 of 2 pods"}, reports[len(reports)-1])
	assert.Equal(t, []string{"drain:requested", "drain:success"}, d.verbs(t))
	left, err := d.fakes.Kube.CoreV1().Pods("ns").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, left.Items, 3, "the filtered pods stay: done, crashed, elsewhere")
}

func TestDrain_ANodeWithNothingToEvict_IsDrainedAtOnce(t *testing.T) {
	d := newDrainEnv(t, typedPod("kube-system", "agent", "node-1", ownedByDaemonSet))
	var last jobs.Progress
	require.NoError(t, d.drainer().Drain(t.Context(), sess, "c", "node-1", func(p jobs.Progress) { last = p }))
	assert.Equal(t, []string{"cordon node-1"}, d.log())
	assert.Equal(t, 0, last.Total)
	assert.Equal(t, "No pods to evict", last.Text)
}

func TestDrain_ARefusedEviction_IsRetriedEveryFiveSecondsUntilItGoesThrough(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "web", "node-1"))
	d.evictErr = func(_ string, n int) error {
		if n <= 2 {
			return apierrors.NewTooManyRequests("Cannot evict pod as it would violate the pod's disruption budget.", 0)
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- d.drainer().Drain(t.Context(), sess, "c", "node-1", func(jobs.Progress) {}) }()

	testutil.Recv(t, d.tries) // refused
	d.clock.BlockUntil(1)
	d.clock.Advance(4 * time.Second)
	testutil.NoRecv(t, d.tries)
	d.clock.Advance(time.Second)
	testutil.Recv(t, d.tries) // refused again
	d.clock.BlockUntil(1)
	d.clock.Advance(5 * time.Second)
	testutil.Recv(t, d.tries) // goes through
	require.NoError(t, testutil.Recv(t, done))
}

func TestDrain_ARefusedEviction_IsRetriedWithoutEnd_UntilTheContextEnds(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "web", "node-1"))
	d.evictErr = func(string, int) error { return apierrors.NewTooManyRequests("budget", 0) }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- d.drainer().Drain(ctx, sess, "c", "node-1", func(jobs.Progress) {}) }()
	for range 3 {
		testutil.Recv(t, d.tries)
		d.clock.BlockUntil(1)
		d.clock.Advance(5 * time.Second)
	}
	testutil.Recv(t, d.tries)
	d.clock.BlockUntil(1)
	cancel()
	assert.ErrorIs(t, testutil.Recv(t, done), context.Canceled)
	assert.Equal(t, []string{"drain:requested", "drain:failure"}, d.verbs(t))
}

func TestDrain_WaitsForEachPodToDisappearThroughAWatch(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "web", "node-1"))
	d.autoGone = false
	done := make(chan error, 1)
	go func() { done <- d.drainer().Drain(t.Context(), sess, "c", "node-1", func(jobs.Progress) {}) }()

	testutil.Recv(t, d.evictions)
	testutil.NoRecv(t, done) // evicted, but still there
	testutil.Recv(t, d.watches) // the drain watches the pod instead of polling it
	require.NoError(t, d.fakes.Kube.Tracker().Delete(podsGVR, "ns", "web"))
	require.NoError(t, testutil.Recv(t, done))
}

func TestDrain_APodThatIsReplacedUnderTheSameName_CountsAsGone(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "web-0", "node-1"))
	d.autoGone = false
	done := make(chan error, 1)
	go func() { done <- d.drainer().Drain(t.Context(), sess, "c", "node-1", func(jobs.Progress) {}) }()
	testutil.Recv(t, d.evictions)
	tr := d.fakes.Kube.Tracker()
	require.NoError(t, tr.Delete(podsGVR, "ns", "web-0"))
	require.NoError(t, tr.Add(typedPod("ns", "web-0", "node-2", func(p *corev1.Pod) { p.UID = "new-uid" })))
	require.NoError(t, testutil.Recv(t, done))
}

func TestDrain_AFailedEviction_FailsTheDrainAndIsAudited(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "web", "node-1"))
	d.evictErr = func(string, int) error { return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "web", nil) }
	err := d.drainer().Drain(t.Context(), sess, "c", "node-1", func(jobs.Progress) {})
	assert.Error(t, err)
	assert.Equal(t, []string{"drain:requested", "drain:failure"}, d.verbs(t))
}

func TestDrain_UnknownNode_IsNotFoundAndNothingIsAudited(t *testing.T) {
	d := newDrainEnv(t)
	err := d.drainer().Drain(t.Context(), sess, "c", "node-9", func(jobs.Progress) {})
	assert.Error(t, err)
	assert.Empty(t, d.log())
}

func drainJob(node string) *jobs.Job {
	raw, _ := json.Marshal(map[string]string{"node": node})
	return &jobs.Job{ID: "j1", UserID: sess.UserID, SessionHash: sess.Hash, Cluster: "c", Type: "drain", Params: raw}
}

func TestDrainHandler_RunDrainsTheNodeOfItsParameters(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "web", "node-1"))
	h := actions.NewDrainHandler(actions.Deps{Clients: d.fakes, Clock: d.clock, Audit: d.audit})
	assert.Equal(t, "drain", h.Type())
	require.NoError(t, h.Run(t.Context(), drainJob("node-1"), func(jobs.Progress) {}))
	assert.Equal(t, []string{"cordon node-1", "evict ns/web"}, d.log())
}

func TestDrainHandler_Run_BadParameters_Fail(t *testing.T) {
	d := newDrainEnv(t)
	h := actions.NewDrainHandler(actions.Deps{Clients: d.fakes, Clock: d.clock, Audit: d.audit})
	j := drainJob("")
	assert.Error(t, h.Run(t.Context(), j, func(jobs.Progress) {}))
	j.Params = json.RawMessage(`nonsense`)
	assert.Error(t, h.Run(t.Context(), j, func(jobs.Progress) {}))
}

func TestDrainHandler_Recover(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "a", "node-1"), typedPod("ns", "b", "node-1"), typedPod("kube-system", "agent", "node-1", ownedByDaemonSet))
	h := actions.NewDrainHandler(actions.Deps{Clients: d.fakes, Clock: d.clock, Audit: d.audit})

	status, msg, offers := h.Recover(t.Context(), drainJob("node-1"))
	assert.Equal(t, jobs.Interrupted, status)
	assert.Equal(t, "Drain stopped part-way: 2 pods remain on the node.", msg)
	assert.Equal(t, []string{"retry"}, offers)

	require.NoError(t, d.fakes.Kube.Tracker().Delete(podsGVR, "ns", "a"))
	_, msg, _ = h.Recover(t.Context(), drainJob("node-1"))
	assert.Equal(t, "Drain stopped part-way: 1 pod remains on the node.", msg)

	require.NoError(t, d.fakes.Kube.Tracker().Delete(podsGVR, "ns", "b"))
	status, msg, offers = h.Recover(t.Context(), drainJob("node-1"))
	assert.Equal(t, jobs.Succeeded, status)
	assert.NotEmpty(t, msg)
	assert.Empty(t, offers)
}

func TestDrainHandler_Recover_UnreadableOrUnreachable_StaysInterrupted(t *testing.T) {
	d := newDrainEnv(t)
	d.fakes.Known = map[string]bool{"other": true} // the cluster cannot be reached
	h := actions.NewDrainHandler(actions.Deps{Clients: d.fakes, Clock: d.clock, Audit: d.audit})
	status, msg, offers := h.Recover(t.Context(), drainJob("node-1"))
	assert.Equal(t, jobs.Interrupted, status)
	assert.True(t, strings.Contains(msg, "could not") || strings.Contains(msg, "Could not"), msg)
	assert.Equal(t, []string{"retry"}, offers)
	j := drainJob("node-1")
	j.Params = json.RawMessage(`nonsense`)
	status, _, _ = h.Recover(t.Context(), j)
	assert.Equal(t, jobs.Interrupted, status)
}
