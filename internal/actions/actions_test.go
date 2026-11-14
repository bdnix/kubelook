package actions_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

var (
	t0   = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	sess = session.Session{Hash: "s", UserID: "u"}

	depT  = actions.Target{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "ns", Name: "web"}
	stsT  = actions.Target{Group: "apps", Version: "v1", Resource: "statefulsets", Namespace: "ns", Name: "db"}
	podT  = actions.Target{Group: "core", Version: "v1", Resource: "pods", Namespace: "ns", Name: "web-1"}
	cjT   = actions.Target{Group: "batch", Version: "v1", Resource: "cronjobs", Namespace: "ns", Name: "nightly"}
	pvcT  = actions.Target{Group: "core", Version: "v1", Resource: "persistentvolumeclaims", Namespace: "ns", Name: "data"}
	rsAPI = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
)

type env struct {
	t     *testing.T
	svc   *actions.Service
	fakes *fakekube.Clients
	out   *bytes.Buffer
	patch []clienttesting.PatchActionImpl
}

func list(gv, name, kind string, ns bool) *metav1.APIResourceList {
	return &metav1.APIResourceList{GroupVersion: gv, APIResources: []metav1.APIResource{{Name: name, Namespaced: ns, Kind: kind, Verbs: metav1.Verbs{"get", "list", "patch", "create"}}}}
}

func obj(apiVersion, kind, name string, fields map[string]any) *unstructured.Unstructured {
	return fakekube.Object(apiVersion, kind, "ns", name, fields)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	fk := fakekube.New()
	fk.AddResources(
		list("apps/v1", "replicasets", "ReplicaSet", true), list("apps/v1", "statefulsets", "StatefulSet", true),
		list("apps/v1", "daemonsets", "DaemonSet", true), list("batch/v1", "cronjobs", "CronJob", true),
		list("batch/v1", "jobs", "Job", true), list("v1", "persistentvolumeclaims", "PersistentVolumeClaim", true),
		list("storage.k8s.io/v1", "storageclasses", "StorageClass", false),
	)
	db := storetest.New(t)
	out := &bytes.Buffer{}
	n := 0
	rec := audit.NewRecorder(db, clock.NewFake(t0), out, func() string { n++; return fmt.Sprintf("a%d", n) })
	e := &env{t: t, fakes: fk, out: out, svc: actions.New(actions.Deps{Clients: fk, Clock: clock.NewFake(t0), Audit: rec})}
	fk.Dyn.PrependReactor("patch", "*", func(a clienttesting.Action) (bool, runtime.Object, error) {
		e.patch = append(e.patch, a.(clienttesting.PatchActionImpl))
		return true, obj("v1", "X", "x", nil), nil
	})
	return e
}

func (e *env) do(action string, target actions.Target, params any) error {
	raw, _ := json.Marshal(params)
	if params == nil {
		raw = nil
	}
	return e.svc.Do(e.t.Context(), sess, "c", actions.Request{Action: action, Target: target, Params: raw})
}

func (e *env) verbs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		out = append(out, m["verb"].(string)+":"+m["outcome"].(string))
	}
	return out
}

func (e *env) lastPatch(t *testing.T) clienttesting.PatchActionImpl {
	t.Helper()
	require.NotEmpty(t, e.patch)
	return e.patch[len(e.patch)-1]
}

func TestScale_PatchesTheScaleSubresourceOfAllThreeKinds(t *testing.T) {
	for _, tg := range []actions.Target{depT, stsT, {Group: "apps", Version: "v1", Resource: "replicasets", Namespace: "ns", Name: "rs"}} {
		e := newEnv(t)
		require.NoError(t, e.do("scale", tg, map[string]any{"replicas": 3}))
		p := e.lastPatch(t)
		assert.Equal(t, "scale", p.Subresource)
		assert.Equal(t, types.MergePatchType, p.PatchType)
		assert.JSONEq(t, `{"spec":{"replicas":3}}`, string(p.Patch))
		assert.Equal(t, []string{"scale:requested", "scale:success"}, e.verbs(t))
	}
}

func TestScale_Validation(t *testing.T) {
	e := newEnv(t)
	assert.ErrorIs(t, e.do("scale", depT, map[string]any{"replicas": -1}), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("scale", depT, nil), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("scale", podT, map[string]any{"replicas": 1}), apperr.ErrInvalid, "wrong kind")
	assert.ErrorIs(t, e.do("nonsense", depT, nil), apperr.ErrInvalid)
	assert.Empty(t, e.verbs(t), "rejected requests are not audited")
}

func TestRestart_SetsTheRestartedAtAnnotationFromTheClock(t *testing.T) {
	for _, tg := range []actions.Target{depT, stsT, {Group: "apps", Version: "v1", Resource: "daemonsets", Namespace: "ns", Name: "agent"}} {
		e := newEnv(t)
		require.NoError(t, e.do("restart", tg, nil))
		p := e.lastPatch(t)
		assert.Equal(t, types.StrategicMergePatchType, p.PatchType)
		assert.JSONEq(t, `{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-11-14T12:00:00Z"}}}}}`, string(p.Patch))
		assert.Equal(t, []string{"restart:requested", "restart:success"}, e.verbs(t))
	}
}

func TestPauseAndResume(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.do("pause", depT, nil))
	assert.JSONEq(t, `{"spec":{"paused":true}}`, string(e.lastPatch(t).Patch))
	require.NoError(t, e.do("resume", depT, nil))
	assert.JSONEq(t, `{"spec":{"paused":false}}`, string(e.lastPatch(t).Patch))
	assert.Equal(t, types.MergePatchType, e.lastPatch(t).PatchType)
	assert.Equal(t, []string{"patch:requested", "patch:success", "patch:requested", "patch:success"}, e.verbs(t))
	assert.ErrorIs(t, e.do("pause", stsT, nil), apperr.ErrInvalid)
}

func rs(name, revision string, owner *unstructured.Unstructured) *unstructured.Unstructured {
	o := obj("apps/v1", "ReplicaSet", name, map[string]any{"spec": map[string]any{"template": map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"app": "web", "pod-template-hash": "abc"}},
		"spec":     map[string]any{"containers": []any{map[string]any{"name": "app", "image": "nginx:" + revision}}},
	}}})
	o.SetAnnotations(map[string]string{"deployment.kubernetes.io/revision": revision})
	if owner != nil {
		o.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: owner.GetName(), UID: owner.GetUID()}})
	}
	return o
}

func TestRollback_PatchesTheDeploymentTemplateOfTheRevisionWithoutThePodTemplateHash(t *testing.T) {
	e := newEnv(t)
	dep := obj("apps/v1", "Deployment", "web", nil)
	other := obj("apps/v1", "Deployment", "other", nil)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(dep))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(rs("web-1", "1", dep)))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(rs("web-2", "2", dep)))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(rs("other-1", "1", other)))

	require.NoError(t, e.do("rollback", depT, map[string]any{"revision": 1}))
	p := e.lastPatch(t)
	assert.Equal(t, types.JSONPatchType, p.PatchType)
	var ops []map[string]any
	require.NoError(t, json.Unmarshal(p.Patch, &ops))
	require.Len(t, ops, 1)
	assert.Equal(t, "replace", ops[0]["op"])
	assert.Equal(t, "/spec/template", ops[0]["path"])
	tpl := ops[0]["value"].(map[string]any)
	assert.Equal(t, map[string]any{"app": "web"}, tpl["metadata"].(map[string]any)["labels"])
	assert.Contains(t, fmt.Sprint(tpl["spec"]), "nginx:1")
	assert.Equal(t, []string{"rollback:requested", "rollback:success"}, e.verbs(t))
	_ = rsAPI
}

func TestRollback_MissingRevision_404(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(obj("apps/v1", "Deployment", "web", nil)))
	assert.ErrorIs(t, e.do("rollback", depT, map[string]any{"revision": 9}), apperr.ErrNotFound)
	assert.Equal(t, []string{"rollback:requested", "rollback:failure"}, e.verbs(t))
}

func workload(kind, name string) *unstructured.Unstructured {
	return obj("apps/v1", kind, name, map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
		"containers":     []any{map[string]any{"name": "app", "image": "nginx:1"}},
		"initContainers": []any{map[string]any{"name": "setup", "image": "busybox"}},
	}}}})
}

func TestSetImage_PatchesContainersOrInitContainers(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(workload("Deployment", "web")))
	require.NoError(t, e.do("setImage", depT, map[string]any{"container": "app", "image": "nginx:2"}))
	p := e.lastPatch(t)
	assert.Equal(t, types.StrategicMergePatchType, p.PatchType)
	assert.JSONEq(t, `{"spec":{"template":{"spec":{"containers":[{"name":"app","image":"nginx:2"}]}}}}`, string(p.Patch))

	require.NoError(t, e.do("setImage", depT, map[string]any{"container": "setup", "image": "busybox:2", "init": true}))
	assert.JSONEq(t, `{"spec":{"template":{"spec":{"initContainers":[{"name":"setup","image":"busybox:2"}]}}}}`, string(e.lastPatch(t).Patch))
	assert.Equal(t, []string{"patch:requested", "patch:success", "patch:requested", "patch:success"}, e.verbs(t))
}

func TestSetImage_Validation(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(workload("Deployment", "web")))
	assert.ErrorIs(t, e.do("setImage", depT, map[string]any{"container": "nope", "image": "x:1"}), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("setImage", depT, map[string]any{"container": "app", "image": ""}), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("setImage", depT, map[string]any{"container": "app", "image": "a b"}), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("setImage", depT, map[string]any{"container": "", "image": "a"}), apperr.ErrInvalid)
	assert.Empty(t, e.patch)
}

func TestCronJobTrigger_CreatesAJobFromTheTemplateOwnedByTheCronJob(t *testing.T) {
	e := newEnv(t)
	cj := obj("batch/v1", "CronJob", "nightly", map[string]any{"spec": map[string]any{"jobTemplate": map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"team": "a"}},
		"spec":     map[string]any{"template": map[string]any{"spec": map[string]any{"restartPolicy": "Never"}}},
	}}})
	require.NoError(t, e.fakes.Dyn.Tracker().Add(cj))
	require.NoError(t, e.do("cronjobTrigger", cjT, nil))
	jobs, err := e.fakes.Dyn.Resource(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}).Namespace("ns").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)
	j := jobs.Items[0]
	assert.Equal(t, fmt.Sprintf("nightly-manual-%d", t0.Unix()), j.GetName())
	assert.Equal(t, "manual", j.GetAnnotations()["cronjob.kubernetes.io/instantiate"])
	assert.Equal(t, map[string]string{"team": "a"}, j.GetLabels())
	refs := j.GetOwnerReferences()
	require.Len(t, refs, 1)
	assert.Equal(t, "CronJob", refs[0].Kind)
	assert.Equal(t, cj.GetUID(), refs[0].UID)
	spec, _, _ := unstructured.NestedMap(j.Object, "spec", "template", "spec")
	assert.Equal(t, "Never", spec["restartPolicy"])
	assert.Equal(t, []string{"create:requested", "create:success"}, e.verbs(t))
}

func TestCronJobSuspendAndResume(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.do("cronjobSuspend", cjT, nil))
	assert.JSONEq(t, `{"spec":{"suspend":true}}`, string(e.lastPatch(t).Patch))
	require.NoError(t, e.do("cronjobResume", cjT, nil))
	assert.JSONEq(t, `{"spec":{"suspend":false}}`, string(e.lastPatch(t).Patch))
	assert.ErrorIs(t, e.do("cronjobSuspend", depT, nil), apperr.ErrInvalid)
}

func TestEvict_UsesTheEvictionSubresourceAndMapsPDBRefusals(t *testing.T) {
	e := newEnv(t)
	_, err := e.fakes.Kube.CoreV1().Pods("ns").Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "ns"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, e.do("evict", podT, nil))
	var got string
	for _, a := range e.fakes.Kube.Actions() {
		if a.GetSubresource() == "eviction" {
			got = a.GetNamespace()
		}
	}
	assert.Equal(t, "ns", got)
	assert.Equal(t, []string{"evict:requested", "evict:success"}, e.verbs(t))

	e.fakes.Kube.PrependReactor("create", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		return true, nil, apierrors.NewTooManyRequests("Cannot evict pod as it would violate the pod's disruption budget.", 10)
	})
	err = e.do("evict", podT, nil)
	require.ErrorIs(t, err, apperr.ErrConflict)
	assert.Contains(t, err.Error(), "Cannot evict: a PodDisruptionBudget would be violated.")
}

func pvc(storageClass, size string) *unstructured.Unstructured {
	return obj("v1", "PersistentVolumeClaim", "data", map[string]any{"spec": map[string]any{
		"storageClassName": storageClass, "resources": map[string]any{"requests": map[string]any{"storage": size}},
	}})
}

func sc(expand bool) *unstructured.Unstructured {
	return fakekube.Object("storage.k8s.io/v1", "StorageClass", "", "fast", map[string]any{"allowVolumeExpansion": expand})
}

func TestExpandPVC(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(pvc("fast", "10Gi")))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(sc(true)))
	require.NoError(t, e.do("expandPVC", pvcT, map[string]any{"storage": "20Gi"}))
	assert.JSONEq(t, `{"spec":{"resources":{"requests":{"storage":"20Gi"}}}}`, string(e.lastPatch(t).Patch))
	assert.Equal(t, []string{"patch:requested", "patch:success"}, e.verbs(t))
}

func TestExpandPVC_Refusals(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(pvc("fast", "10Gi")))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(sc(false)))
	assert.ErrorIs(t, e.do("expandPVC", pvcT, map[string]any{"storage": "20Gi"}), apperr.ErrUnprocessable, "class does not allow expansion")

	e2 := newEnv(t)
	require.NoError(t, e2.fakes.Dyn.Tracker().Add(pvc("fast", "10Gi")))
	require.NoError(t, e2.fakes.Dyn.Tracker().Add(sc(true)))
	assert.ErrorIs(t, e2.do("expandPVC", pvcT, map[string]any{"storage": "10Gi"}), apperr.ErrUnprocessable, "not larger")
	assert.ErrorIs(t, e2.do("expandPVC", pvcT, map[string]any{"storage": "5Gi"}), apperr.ErrUnprocessable)
	assert.ErrorIs(t, e2.do("expandPVC", pvcT, map[string]any{"storage": "lots"}), apperr.ErrInvalid)
	assert.Empty(t, e2.patch)
}

func TestAudit_FailureRefusesTheActionAndAPIErrorsAreRecorded(t *testing.T) {
	e := newEnv(t)
	db := storetest.New(t)
	dead := audit.NewRecorder(db, clock.NewFake(t0), &bytes.Buffer{}, func() string { return "x" })
	require.NoError(t, db.Close())
	svc := actions.New(actions.Deps{Clients: e.fakes, Clock: clock.NewFake(t0), Audit: dead})
	raw, _ := json.Marshal(map[string]any{"replicas": 1})
	err := svc.Do(t.Context(), sess, "c", actions.Request{Action: "scale", Target: depT, Params: raw})
	assert.ErrorIs(t, err, apperr.ErrNotRecorded)
	assert.Empty(t, e.patch)

	e.fakes.Dyn.PrependReactor("patch", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "web", errors.New("RBAC says no"))
	})
	err = e.do("scale", depT, map[string]any{"replicas": 2})
	require.ErrorIs(t, err, apperr.ErrForbidden)
	assert.Contains(t, err.Error(), "RBAC says no")
	assert.Equal(t, []string{"scale:requested", "scale:failure"}, e.verbs(t))
}

func TestUnknownClusterAndUnservedResource(t *testing.T) {
	e := newEnv(t)
	e.fakes.Known = map[string]bool{}
	assert.ErrorIs(t, e.do("pause", depT, nil), apperr.ErrClusterUnavailable)
	e.fakes.Known = nil
	assert.ErrorIs(t, e.do("pause", actions.Target{Group: "apps", Version: "v1", Resource: "gadgets", Namespace: "ns", Name: "x"}, nil), apperr.ErrNotFound)
}
