package actions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func TestKeda_PauseAndResumeToggleTheAnnotation(t *testing.T) {
	e := newEnv(t)
	e.serveFlux("keda.sh", "v1alpha1", "scaledobjects", "ScaledObject", true)
	e.fluxObject("keda.sh", "v1alpha1", "ScaledObject", "so", nil)
	tg := fluxTarget("keda.sh", "v1alpha1", "scaledobjects", "so")

	require.NoError(t, e.do("kedaPause", tg, nil))
	p := e.lastPatch(t)
	assert.Equal(t, types.MergePatchType, p.PatchType)
	assert.JSONEq(t, `{"metadata":{"annotations":{"autoscaling.keda.sh/paused":"true"}}}`, string(p.Patch))

	require.NoError(t, e.do("kedaResume", tg, nil))
	assert.JSONEq(t, `{"metadata":{"annotations":{"autoscaling.keda.sh/paused":null}}}`, string(e.lastPatch(t).Patch), "removing an annotation with a merge patch needs an explicit null")

	assert.Equal(t, []string{"module-action:requested", "module-action:success", "module-action:requested", "module-action:success"}, e.verbs(t))
}

func TestEso_ForceSyncSetsTheAnnotationFromTheClock(t *testing.T) {
	e := newEnv(t)
	e.serveFlux("external-secrets.io", "v1", "externalsecrets", "ExternalSecret", true)
	e.fluxObject("external-secrets.io", "v1", "ExternalSecret", "db", nil)
	require.NoError(t, e.do("esoForceSync", fluxTarget("external-secrets.io", "v1", "externalsecrets", "db"), nil))
	p := e.lastPatch(t)
	assert.Equal(t, types.MergePatchType, p.PatchType)
	assert.JSONEq(t, `{"metadata":{"annotations":{"force-sync":"1794657600"}}}`, string(p.Patch), "unix seconds of the fake clock, 2026-11-14T12:00:00Z")
	assert.Equal(t, []string{"module-action:requested", "module-action:success"}, e.verbs(t))
}

func TestModuleActions_ApplyToTheirKindsOnly(t *testing.T) {
	e := newEnv(t)
	assert.ErrorIs(t, e.do("kedaPause", depT, nil), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("kedaResume", podT, nil), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("esoForceSync", depT, nil), apperr.ErrInvalid)
	assert.Empty(t, e.verbs(t))
}

func TestTrivyRescan_DeletesTheReport(t *testing.T) {
	e := newEnv(t)
	e.serveFlux("aquasecurity.github.io", "v1alpha1", "vulnerabilityreports", "VulnerabilityReport", true)
	e.fluxObject("aquasecurity.github.io", "v1alpha1", "VulnerabilityReport", "r", nil)
	require.NoError(t, e.do("trivyRescan", fluxTarget("aquasecurity.github.io", "v1alpha1", "vulnerabilityreports", "r"), nil))
	assert.Equal(t, []string{"module-action:requested", "module-action:success"}, e.verbs(t))
	_, err := e.fakes.Dyn.Resource(schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}).Namespace("ns").Get(t.Context(), "r", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "the report is gone")
}

func TestTrivyRescan_OnlyForReportsOfTrivy(t *testing.T) {
	e := newEnv(t)
	assert.ErrorIs(t, e.do("trivyRescan", depT, nil), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("trivyRescan", podT, nil), apperr.ErrInvalid)
}

func TestTrivyRescan_ForAClusterReport(t *testing.T) {
	e := newEnv(t)
	e.serveFlux("aquasecurity.github.io", "v1alpha1", "clustercompliancereports", "ClusterComplianceReport", false)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.Object("aquasecurity.github.io/v1alpha1", "ClusterComplianceReport", "", "nsa", nil)))
	tg := actions.Target{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "clustercompliancereports", Name: "nsa"}
	require.NoError(t, e.do("trivyRescan", tg, nil))
}
