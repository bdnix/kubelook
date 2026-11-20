package actions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/apperr"
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
