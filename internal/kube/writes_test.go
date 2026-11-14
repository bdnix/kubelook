package kube_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

var cmGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

func withRV(o *unstructured.Unstructured, rv string) *unstructured.Unstructured {
	o.SetResourceVersion(rv)
	return o
}

func TestCreate_SendsFieldManagerAndDryRunAndRedactsTheResult(t *testing.T) {
	fk := fixture()
	r := kube.NewResources(fk)
	var opts metav1.CreateOptions
	fk.Dyn.PrependReactor("create", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		opts = a.(clienttesting.CreateAction).(clienttesting.CreateActionImpl).CreateOptions
		return false, nil, nil
	})
	got, err := r.Create(t.Context(), sessA, "c", secretsGVR, "ns1", fakekube.Secret("ns1", "new", map[string]any{"k": "dmFs"}), false)
	require.NoError(t, err)
	assert.Equal(t, "kubelook", opts.FieldManager)
	assert.Empty(t, opts.DryRun)
	data, _, _ := unstructured.NestedMap(got.Object, "data")
	assert.Equal(t, "********", data["k"])

	_, err = r.Create(t.Context(), sessA, "c", cmGVR, "ns1", fakekube.ConfigMap("ns1", "x", nil), true)
	require.NoError(t, err)
	_, err = r.Create(t.Context(), sessA, "c", secretsGVR, "ns1", fakekube.Secret("ns1", "n2", map[string]any{"k": "dmFs"}), true)
	require.NoError(t, err)
	assert.Equal(t, []string{"All"}, opts.DryRun)
}

func TestCreate_HelmReleaseRecordCannotBeWritten(t *testing.T) {
	r := kube.NewResources(fixture())
	_, err := r.Create(t.Context(), sessA, "c", secretsGVR, "ns1", fakekube.Secret("ns1", "sh.helm.release.v1.x.v1", nil), false)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestUpdate_RequiresResourceVersionAndSendsFieldManager(t *testing.T) {
	fk := fixture()
	_ = fk.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", "cm", map[string]any{"a": "1"}))
	r := kube.NewResources(fk)
	_, _, err := r.Update(t.Context(), sessA, "c", cmGVR, "ns1", fakekube.ConfigMap("ns1", "cm", nil), false)
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Contains(t, err.Error(), "resourceVersion is required")

	var opts metav1.UpdateOptions
	fk.Dyn.PrependReactor("update", "configmaps", func(a clienttesting.Action) (bool, runtime.Object, error) {
		opts = a.(clienttesting.UpdateActionImpl).UpdateOptions
		return false, nil, nil
	})
	live, res, err := r.Update(t.Context(), sessA, "c", cmGVR, "ns1", withRV(fakekube.ConfigMap("ns1", "cm", map[string]any{"a": "2"}), "7"), false)
	require.NoError(t, err)
	assert.Nil(t, live, "a real save does not read the live object")
	assert.Equal(t, "kubelook", opts.FieldManager)
	assert.Equal(t, "2", res.Object["data"].(map[string]any)["a"])
}

func TestUpdate_DryRunReturnsLiveAndResult(t *testing.T) {
	fk := fixture()
	_ = fk.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", "cm", map[string]any{"a": "1"}))
	r := kube.NewResources(fk)
	var opts metav1.UpdateOptions
	fk.Dyn.PrependReactor("update", "configmaps", func(a clienttesting.Action) (bool, runtime.Object, error) {
		opts = a.(clienttesting.UpdateActionImpl).UpdateOptions
		return false, nil, nil
	})
	live, res, err := r.Update(t.Context(), sessA, "c", cmGVR, "ns1", withRV(fakekube.ConfigMap("ns1", "cm", map[string]any{"a": "2"}), "7"), true)
	require.NoError(t, err)
	assert.Equal(t, []string{"All"}, opts.DryRun)
	assert.Equal(t, "1", live.Object["data"].(map[string]any)["a"])
	assert.Equal(t, "2", res.Object["data"].(map[string]any)["a"])
}

func TestUpdate_SecretMasksAreRestoredFromTheLiveObjectAndTheResultIsRedacted(t *testing.T) {
	fk := fixture()
	r := kube.NewResources(fk)
	var sent *unstructured.Unstructured
	fk.Dyn.PrependReactor("update", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		sent = a.(clienttesting.UpdateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		return false, nil, nil
	})
	edited := withRV(fakekube.Secret("ns1", "db", map[string]any{"password": "********", "extra": "bmV3"}), "5")
	_, res, err := r.Update(t.Context(), sessA, "c", secretsGVR, "ns1", edited, false)
	require.NoError(t, err)
	data, _, _ := unstructured.NestedMap(sent.Object, "data")
	assert.Equal(t, "c2VjcmV0", data["password"], "mask replaced by the live value")
	assert.Equal(t, "bmV3", data["extra"])
	resData, _, _ := unstructured.NestedMap(res.Object, "data")
	assert.Equal(t, "********", resData["password"])
	assert.Equal(t, "********", resData["extra"])
}

func TestUpdate_SecretKeyRemovedMeanwhile_Conflict(t *testing.T) {
	r := kube.NewResources(fixture())
	edited := withRV(fakekube.Secret("ns1", "db", map[string]any{"gone": "********"}), "5")
	_, _, err := r.Update(t.Context(), sessA, "c", secretsGVR, "ns1", edited, false)
	assert.ErrorIs(t, err, apperr.ErrConflict)
}

func TestUpdate_HelmReleaseRecordCannotBeWritten(t *testing.T) {
	r := kube.NewResources(fixture())
	edited := withRV(fakekube.Secret("ns1", "sh.helm.release.v1.web.v1", map[string]any{"release": "eA=="}), "5")
	_, _, err := r.Update(t.Context(), sessA, "c", secretsGVR, "ns1", edited, false)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestWrites_ErrorMapping(t *testing.T) {
	gr := schema.GroupResource{Resource: "configmaps"}
	tests := []struct {
		name    string
		err     error
		dryRun  bool
		is      error
		message string
	}{
		{"conflict", apierrors.NewConflict(gr, "cm", errors.New("stale")), false, apperr.ErrConflict, "The object changed since you loaded it."},
		{"invalid is unprocessable", apierrors.NewInvalid(schema.GroupKind{Kind: "ConfigMap"}, "cm", nil), false, apperr.ErrUnprocessable, "ConfigMap"},
		{"webhook denial", apierrors.NewBadRequest("admission webhook \"x\" denied the request: no"), false, apperr.ErrUnprocessable, "denied the request"},
		{"dry run unsupported", apierrors.NewBadRequest("the server does not support dry run for this kind"), true, apperr.ErrDryRunUnsupported, "does not support dry run"},
		{"forbidden", apierrors.NewForbidden(gr, "cm", errors.New("RBAC says no")), false, apperr.ErrForbidden, "RBAC says no"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := fixture()
			_ = fk.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", "cm", nil))
			fk.Dyn.PrependReactor("update", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
			_, _, err := kube.NewResources(fk).Update(t.Context(), sessA, "c", cmGVR, "ns1", withRV(fakekube.ConfigMap("ns1", "cm", nil), "1"), tc.dryRun)
			require.ErrorIs(t, err, tc.is)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

func TestPatch_SendsTypeBodyAndOptions(t *testing.T) {
	fk := fixture()
	_ = fk.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", "cm", map[string]any{"a": "1"}))
	r := kube.NewResources(fk)
	var got clienttesting.PatchActionImpl
	fk.Dyn.PrependReactor("patch", "configmaps", func(a clienttesting.Action) (bool, runtime.Object, error) {
		got = a.(clienttesting.PatchActionImpl)
		return false, nil, nil
	})
	_, err := r.Patch(t.Context(), sessA, "c", cmGVR, "ns1", "cm", types.MergePatchType, []byte(`{"data":{"a":"2"}}`), true)
	require.NoError(t, err)
	assert.Equal(t, types.MergePatchType, got.PatchType)
	assert.JSONEq(t, `{"data":{"a":"2"}}`, string(got.Patch))
	assert.Equal(t, "kubelook", got.PatchOptions.FieldManager)
	assert.Equal(t, []string{"All"}, got.PatchOptions.DryRun)
	assert.Equal(t, "cm", got.Name)
}

func TestPatch_HelmReleaseAndMaskedValues(t *testing.T) {
	r := kube.NewResources(fixture())
	_, err := r.Patch(t.Context(), sessA, "c", secretsGVR, "ns1", "sh.helm.release.v1.web.v1", types.MergePatchType, []byte(`{}`), false)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.Patch(t.Context(), sessA, "c", secretsGVR, "ns1", "db", types.MergePatchType, []byte(`{"data":{"password":"********"}}`), false)
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	res, err := r.Patch(t.Context(), sessA, "c", secretsGVR, "ns1", "db", types.MergePatchType, []byte(`{"data":{"x":"eQ=="}}`), false)
	require.NoError(t, err)
	d, _, _ := unstructured.NestedMap(res.Object, "data")
	assert.Equal(t, "********", d["x"])
}

func TestDelete_PropagationPolicyAndErrors(t *testing.T) {
	fk := fixture()
	r := kube.NewResources(fk)
	var opts metav1.DeleteOptions
	fk.Dyn.PrependReactor("delete", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		opts = a.(clienttesting.DeleteActionImpl).DeleteOptions
		return false, nil, nil
	})
	require.NoError(t, r.Delete(t.Context(), sessA, "c", podsGVR, "ns1", "a", metav1.DeletePropagationForeground))
	require.NotNil(t, opts.PropagationPolicy)
	assert.Equal(t, metav1.DeletePropagationForeground, *opts.PropagationPolicy)

	require.NoError(t, r.Delete(t.Context(), sessA, "c", podsGVR, "ns1", "b", ""))
	assert.Equal(t, metav1.DeletePropagationBackground, *opts.PropagationPolicy)

	assert.ErrorIs(t, r.Delete(t.Context(), sessA, "c", podsGVR, "ns1", "missing", ""), apperr.ErrNotFound)
	assert.ErrorIs(t, r.Delete(t.Context(), sessA, "c", secretsGVR, "ns1", "sh.helm.release.v1.web.v1", ""), apperr.ErrNotFound)
	assert.ErrorIs(t, r.Delete(t.Context(), sessA, "c", cmGVR, "ns1", "x", "Sideways"), apperr.ErrInvalid)
}

func TestSecretData_DecodesValuesAndHidesHelmRecords(t *testing.T) {
	r := kube.NewResources(fixture())
	got, err := r.SecretData(t.Context(), sessA, "c", "ns1", "db")
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{"password": []byte("secret")}, got)

	_, err = r.SecretData(t.Context(), sessA, "c", "ns1", "sh.helm.release.v1.web.v1")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.SecretData(t.Context(), sessA, "c", "ns1", "nope")
	assert.ErrorIs(t, err, apperr.ErrNotFound)

	fk := fixture()
	_ = fk.Dyn.Tracker().Add(fakekube.Secret("ns1", "broken", map[string]any{"k": "!!!"}))
	_, err = kube.NewResources(fk).SecretData(t.Context(), sessA, "c", "ns1", "broken")
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}
