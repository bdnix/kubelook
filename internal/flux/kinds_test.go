package flux_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/flux"
)

func TestKinds_AreTheSpecsTable(t *testing.T) {
	var got []string
	for _, k := range flux.Kinds {
		got = append(got, k.Group+"/"+k.Kind)
	}
	assert.ElementsMatch(t, []string{
		"fluxcd.controlplane.io/FluxInstance", "fluxcd.controlplane.io/FluxReport", "fluxcd.controlplane.io/ResourceSet", "fluxcd.controlplane.io/ResourceSetInputProvider",
		"source.toolkit.fluxcd.io/GitRepository", "source.toolkit.fluxcd.io/OCIRepository", "source.toolkit.fluxcd.io/HelmRepository",
		"source.toolkit.fluxcd.io/HelmChart", "source.toolkit.fluxcd.io/Bucket", "source.toolkit.fluxcd.io/ExternalArtifact",
		"source.extensions.fluxcd.io/ArtifactGenerator", "kustomize.toolkit.fluxcd.io/Kustomization", "helm.toolkit.fluxcd.io/HelmRelease",
		"image.toolkit.fluxcd.io/ImageRepository", "image.toolkit.fluxcd.io/ImagePolicy", "image.toolkit.fluxcd.io/ImageUpdateAutomation",
		"notification.toolkit.fluxcd.io/Provider", "notification.toolkit.fluxcd.io/Alert", "notification.toolkit.fluxcd.io/Receiver",
	}, got)
	_, ok := flux.Find("keda.sh", "ScaledObject")
	assert.False(t, ok)
}

func TestSuspendPatch_PerFamily(t *testing.T) {
	tests := []struct {
		group, kind string
		suspend     string
		resume      string
	}{
		{"kustomize.toolkit.fluxcd.io", "Kustomization", `{"spec":{"suspend":true}}`, `{"spec":{"suspend":false}}`},
		{"source.toolkit.fluxcd.io", "ExternalArtifact", `{"spec":{"suspend":true}}`, `{"spec":{"suspend":false}}`},
		{"notification.toolkit.fluxcd.io", "Alert", `{"spec":{"suspend":true}}`, `{"spec":{"suspend":false}}`},
		{"source.extensions.fluxcd.io", "ArtifactGenerator", `{"metadata":{"annotations":{"source.extensions.fluxcd.io/reconcile":"disabled"}}}`, `{"metadata":{"annotations":{"source.extensions.fluxcd.io/reconcile":"enabled"}}}`},
		{"fluxcd.controlplane.io", "FluxInstance", `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"disabled"}}}`, `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"enabled"}}}`},
		{"fluxcd.controlplane.io", "ResourceSet", `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"disabled"}}}`, `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"enabled"}}}`},
		{"fluxcd.controlplane.io", "ResourceSetInputProvider", `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"disabled"}}}`, `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"enabled"}}}`},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			k, ok := flux.Find(tc.group, tc.kind)
			require.True(t, ok)
			on, err := k.SuspendPatch(true)
			require.NoError(t, err)
			assert.JSONEq(t, tc.suspend, string(on))
			off, err := k.SuspendPatch(false)
			require.NoError(t, err)
			assert.JSONEq(t, tc.resume, string(off))
		})
	}
}

func TestSuspendPatch_FluxReportCannotBeSuspended(t *testing.T) {
	k, ok := flux.Find("fluxcd.controlplane.io", "FluxReport")
	require.True(t, ok)
	_, err := k.SuspendPatch(true)
	assert.ErrorIs(t, err, flux.ErrCannotSuspend)
	_, err = k.SuspendPatch(false)
	assert.ErrorIs(t, err, flux.ErrCannotSuspend)
}

func TestReconcilePatch_SetsRequestedAtFromTheGivenTime(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 123, time.UTC)
	assert.JSONEq(t, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"2026-11-14T12:00:00.000000123Z"}}}`, string(flux.ReconcilePatch(now)))
}

func TestIsSuspended(t *testing.T) {
	spec, _ := flux.Find("kustomize.toolkit.fluxcd.io", "Kustomization")
	assert.True(t, spec.IsSuspended(map[string]any{"spec": map[string]any{"suspend": true}}))
	assert.False(t, spec.IsSuspended(map[string]any{"spec": map[string]any{"suspend": false}}))
	assert.False(t, spec.IsSuspended(map[string]any{}))

	op, _ := flux.Find("fluxcd.controlplane.io", "ResourceSet")
	annotated := func(v string) map[string]any {
		return map[string]any{"metadata": map[string]any{"annotations": map[string]any{"fluxcd.controlplane.io/reconcile": v}}}
	}
	assert.True(t, op.IsSuspended(annotated("disabled")))
	assert.True(t, op.IsSuspended(annotated("Disabled")), "the controllers compare without case")
	assert.False(t, op.IsSuspended(annotated("enabled")))
	assert.False(t, op.IsSuspended(map[string]any{}))

	report, _ := flux.Find("fluxcd.controlplane.io", "FluxReport")
	assert.False(t, report.IsSuspended(map[string]any{"spec": map[string]any{"suspend": true}}))
}
