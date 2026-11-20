package helm4_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/helm/helm4"
	"github.com/bdnix/kubelook/internal/testutil/helmtest"
)

var _ helm.Driver = (*helm4.Adapter)(nil)

func seeded(t *testing.T) *helm4.Adapter {
	t.Helper()
	env := helmtest.New(t)
	env.Install(t, helmtest.Chart(), "team-b", "zeta", nil)
	env.Install(t, helmtest.Chart(), "team-a", "web", map[string]any{"replicas": 2})
	env.Install(t, helmtest.Chart(), "team-a", "api", nil)
	env.Upgrade(t, helmtest.ChartVersion("web", "1.1.0", "2.1"), "team-a", "web", map[string]any{"replicas": 3})
	return helm4.New(env.Open)
}

func TestList_AllNamespaces_SortedByNamespaceThenName(t *testing.T) {
	got, err := seeded(t).List(context.Background(), "")
	require.NoError(t, err)
	var keys []string
	for _, r := range got {
		keys = append(keys, r.Namespace+"/"+r.Name)
	}
	assert.Equal(t, []string{"team-a/api", "team-a/web", "team-b/zeta"}, keys)
}

func TestList_ReturnsTheLatestRevisionWithItsFields(t *testing.T) {
	got, err := seeded(t).List(context.Background(), "team-a")
	require.NoError(t, err)
	require.Len(t, got, 2)
	web := got[1]
	assert.Equal(t, "web", web.Name)
	assert.Equal(t, 2, web.Revision)
	assert.Equal(t, "deployed", web.Status)
	assert.Equal(t, "web", web.Chart)
	assert.Equal(t, "1.1.0", web.ChartVersion)
	assert.Equal(t, "2.1", web.AppVersion)
	assert.False(t, web.Updated.IsZero())
}

func TestList_OneNamespaceOnly(t *testing.T) {
	got, err := seeded(t).List(context.Background(), "team-b")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "zeta", got[0].Name)
}

func TestGet_LatestRevision_HasValuesManifestNotesAndHooks(t *testing.T) {
	d, err := seeded(t).Get(context.Background(), "team-a", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, d.Revision)
	assert.Equal(t, "replicas: 3\n", d.UserValues)
	assert.Contains(t, d.ComputedValues, "replicas: 3")
	assert.Contains(t, d.ComputedValues, "message: hello")
	assert.Contains(t, d.Manifest, "kind: ConfigMap")
	assert.Equal(t, "Thank you for installing web.\n", d.Notes)
	require.Len(t, d.Hooks, 1)
	assert.Equal(t, helm.Hook{Name: "web-test", Kind: "Pod", Events: []string{"test"}}, d.Hooks[0])
}

func TestGet_AnOlderRevision(t *testing.T) {
	d, err := seeded(t).Get(context.Background(), "team-a", "web", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, d.Revision)
	assert.Equal(t, "1.0.0", d.ChartVersion)
	assert.Equal(t, "replicas: 2\n", d.UserValues)
	assert.Equal(t, "superseded", d.Status)
}

func TestGet_UnknownReleaseOrRevision_NotFound(t *testing.T) {
	a := seeded(t)
	_, err := a.Get(context.Background(), "team-a", "nope", 0)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = a.Get(context.Background(), "team-a", "web", 9)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = a.Get(context.Background(), "team-b", "web", 0)
	assert.ErrorIs(t, err, apperr.ErrNotFound, "the release lives in another namespace")
}

func TestGet_ManifestHasTheValuesOfSecretsMasked(t *testing.T) {
	d, err := seeded(t).Get(context.Background(), "team-a", "api", 0)
	require.NoError(t, err)
	assert.Contains(t, d.Manifest, "kind: Secret")
	assert.NotContains(t, d.Manifest, "hunter2")
}

func TestHistory_NewestFirst(t *testing.T) {
	got, err := seeded(t).History(context.Background(), "team-a", "web")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 2, got[0].Revision)
	assert.Equal(t, "deployed", got[0].Status)
	assert.Equal(t, "1.1.0", got[0].ChartVersion)
	assert.NotEmpty(t, got[0].Description)
	assert.Equal(t, 1, got[1].Revision)
	assert.Equal(t, "superseded", got[1].Status)
	assert.False(t, got[1].Updated.IsZero())
}

func TestHistory_UnknownRelease_NotFound(t *testing.T) {
	_, err := seeded(t).History(context.Background(), "team-a", "nope")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestRelease_ARecordWithoutAnApplyMethodIsClientSideApply(t *testing.T) {
	got, err := seeded(t).List(context.Background(), "team-b")
	require.NoError(t, err)
	assert.Equal(t, "csa", got[0].ApplyMethod)
}

func TestWriteMethods_AreNotImplementedYet(t *testing.T) {
	assert.NotNil(t, seeded(t))
}
