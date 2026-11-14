package logs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/logs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func resolverPod(name string, phase corev1.PodPhase, labels map[string]string, containers ...string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: labels}, Status: corev1.PodStatus{Phase: phase}}
	for _, c := range containers {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: c})
	}
	return p
}

func resolverEnv(t *testing.T) (logs.Resolver, *fakekube.Clients) {
	t.Helper()
	fk := fakekube.New()
	tr := fk.Kube.Tracker()
	web1 := resolverPod("web-1", corev1.PodRunning, map[string]string{"app": "web"}, "app", "sidecar")
	web1.Spec.InitContainers = []corev1.Container{{Name: "setup"}}
	for _, p := range []*corev1.Pod{
		web1,
		resolverPod("web-2", corev1.PodRunning, map[string]string{"app": "web"}, "app"),
		resolverPod("web-3", corev1.PodPending, map[string]string{"app": "web"}, "app"),
		resolverPod("api-1", corev1.PodRunning, map[string]string{"app": "api"}, "app"),
	} {
		require.NoError(t, tr.Add(p))
	}
	dep := fakekube.Object("apps/v1", "Deployment", "ns", "web", map[string]any{
		"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": "web"}}},
	})
	require.NoError(t, fk.Dyn.Tracker().Add(dep))
	require.NoError(t, fk.Dyn.Tracker().Add(fakekube.Object("apps/v1", "Deployment", "ns", "bare", map[string]any{"spec": map[string]any{}})))
	return logs.NewKubeResolver(fk), fk
}

func TestResolver_AllContainersOfAPod_InitContainersFirst(t *testing.T) {
	r, _ := resolverEnv(t)
	got, err := r.Resolve(t.Context(), session.Session{}, "c", "ns", logs.SourceSpec{Pod: "web-1", AllContainers: true})
	require.NoError(t, err)
	assert.Equal(t, []logs.Target{{Pod: "web-1", Container: "setup"}, {Pod: "web-1", Container: "app"}, {Pod: "web-1", Container: "sidecar"}}, got)
}

func TestResolver_Selector_RunningPodsSortedWithTheirContainers(t *testing.T) {
	r, _ := resolverEnv(t)
	got, err := r.Resolve(t.Context(), session.Session{}, "c", "ns", logs.SourceSpec{Selector: "app=web"})
	require.NoError(t, err)
	assert.Equal(t, []logs.Target{{Pod: "web-1", Container: "app"}, {Pod: "web-1", Container: "sidecar"}, {Pod: "web-2", Container: "app"}}, got,
		"pending pods have no logs yet and init containers are left out")
}

func TestResolver_Workload_UsesItsSelector(t *testing.T) {
	r, _ := resolverEnv(t)
	got, err := r.Resolve(t.Context(), session.Session{}, "c", "ns", logs.SourceSpec{Workload: &logs.WorkloadRef{Group: "apps", Kind: "Deployment", Name: "web"}})
	require.NoError(t, err)
	assert.Len(t, got, 3)
	assert.Equal(t, "web-1", got[0].Pod)
}

func TestResolver_Errors(t *testing.T) {
	r, fk := resolverEnv(t)
	ctx, sess := t.Context(), session.Session{}
	_, err := r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{Pod: "nope", AllContainers: true})
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{Selector: "app in ("})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	_, err = r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{Workload: &logs.WorkloadRef{Group: "apps", Kind: "Deployment", Name: "missing"}})
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{Workload: &logs.WorkloadRef{Group: "apps", Kind: "Deployment", Name: "bare"}})
	assert.ErrorIs(t, err, apperr.ErrInvalid, "a workload without a selector")
	_, err = r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{Workload: &logs.WorkloadRef{Group: "apps", Kind: "Gadget", Name: "x"}})
	assert.ErrorIs(t, err, apperr.ErrNotFound, "a kind the cluster does not serve")
	_, err = r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	fk.Known = map[string]bool{}
	_, err = r.Resolve(ctx, sess, "c", "ns", logs.SourceSpec{Selector: "app=web"})
	assert.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}
