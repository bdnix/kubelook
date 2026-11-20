package helm4_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	v1 "helm.sh/helm/v4/pkg/release/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/helm/helm4"
	"github.com/bdnix/kubelook/internal/testutil/helmtest"
)

type rig struct {
	*helmtest.Env
	d   *helm4.Adapter
	dir string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	env := helmtest.New(t)
	return &rig{Env: env, d: helm4.New(env.Open).WithWorkDir(t.TempDir()), dir: t.TempDir()}
}

func (r *rig) chart(t *testing.T, version string) helm.ChartSource {
	t.Helper()
	return helm.ChartSource{Path: helmtest.Save(t, helmtest.ChartVersion("web", version, "2.0"), r.dir)}
}

func TestInstall_CreatesRevision1Deployed_WithItsValues(t *testing.T) {
	r := newRig(t)
	rel, err := r.d.Install(context.Background(), helm.InstallOptions{Namespace: "team-a", Name: "web", Chart: r.chart(t, "1.0.0"), Values: map[string]any{"replicas": 2}})
	require.NoError(t, err)
	assert.Equal(t, 1, rel.Revision)
	assert.Equal(t, "deployed", rel.Status)
	assert.Equal(t, "team-a", rel.Namespace)
	d, err := r.d.Get(context.Background(), "team-a", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, "replicas: 2\n", d.UserValues)
	assert.Equal(t, "ssa", d.ApplyMethod, "a first install uses the SDK's default")
}

func TestInstall_ExistingName_Conflict(t *testing.T) {
	r := newRig(t)
	o := helm.InstallOptions{Namespace: "team-a", Name: "web", Chart: r.chart(t, "1.0.0")}
	_, err := r.d.Install(context.Background(), o)
	require.NoError(t, err)
	_, err = r.d.Install(context.Background(), o)
	assert.ErrorIs(t, err, apperr.ErrConflict)
}

func TestInstall_MissingChart_Invalid(t *testing.T) {
	r := newRig(t)
	_, err := r.d.Install(context.Background(), helm.InstallOptions{Namespace: "n", Name: "web", Chart: helm.ChartSource{Path: "/nope.tgz"}})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestUpgrade_AddsARevisionWithTheNewValues(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0"), Values: map[string]any{"replicas": 1}})
	require.NoError(t, err)
	rel, err := r.d.Upgrade(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.1.0"), Values: map[string]any{"replicas": 4}})
	require.NoError(t, err)
	assert.Equal(t, 2, rel.Revision)
	d, err := r.d.Get(ctx, "n", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, "replicas: 4\n", d.UserValues)
	assert.Equal(t, "1.1.0", d.ChartVersion)
}

func TestUpgrade_UnknownRelease_NotFound(t *testing.T) {
	r := newRig(t)
	_, err := r.d.Upgrade(context.Background(), helm.UpgradeOptions{Namespace: "n", Name: "nope", Chart: r.chart(t, "1.0.0")})
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestRollback_MakesANewRevisionWithTheOldValues(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0"), Values: map[string]any{"replicas": 1}})
	require.NoError(t, err)
	_, err = r.d.Upgrade(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.1.0"), Values: map[string]any{"replicas": 4}})
	require.NoError(t, err)
	require.NoError(t, r.d.Rollback(ctx, "n", "web", 1))
	d, err := r.d.Get(ctx, "n", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, 3, d.Revision)
	assert.Equal(t, "replicas: 1\n", d.UserValues)
	assert.Equal(t, "1.0.0", d.ChartVersion)
}

func TestUninstall_RemovesTheRelease(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0")})
	require.NoError(t, err)
	require.NoError(t, r.d.Uninstall(ctx, "n", "web"))
	_, err = r.d.Status(ctx, "n", "web")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestStatus_IsTheLatestRelease(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0")})
	require.NoError(t, err)
	st, err := r.d.Status(ctx, "n", "web")
	require.NoError(t, err)
	assert.Equal(t, "deployed", st.Status)
	assert.Equal(t, 1, st.Revision)
}

func TestTest_RunsTheTestHooks(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0")})
	require.NoError(t, err)
	res, err := r.d.Test(ctx, "n", "web")
	require.NoError(t, err)
	require.Len(t, res.Hooks, 1)
	assert.Equal(t, "web-test", res.Hooks[0].Name)
	assert.NotEmpty(t, res.Hooks[0].Phase)
}

func TestRender_ReturnsTheManifestWithoutStoringARelease(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	manifest, err := r.d.Render(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0"), Values: map[string]any{"replicas": 7}})
	require.NoError(t, err)
	assert.Contains(t, manifest, `replicas: "7"`)
	assert.NotContains(t, manifest, "hunter2", "Secrets are masked")
	_, err = r.d.Status(ctx, "n", "web")
	assert.ErrorIs(t, err, apperr.ErrNotFound, "nothing was stored")
}

// helm3Record stores a revision the way Helm 3 wrote it: no apply method.
func (r *rig) helm3Record(t *testing.T, method string) {
	t.Helper()
	rel := v1.Mock(&v1.MockReleaseOptions{Name: "web", Namespace: "n", Version: 1})
	rel.Chart = helmtest.Chart()
	rel.Chart.Metadata.Version = "1.0.0"
	rel.ApplyMethod = method
	rel.Config = map[string]any{"replicas": 1}
	rel.Manifest = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web-config\n"
	rel.Hooks = nil
	r.Releases().Driver.(interface{ SetNamespace(string) }).SetNamespace("n")
	require.NoError(t, r.Releases().Create(rel))
}

func TestApplyMethod_UpgradeAndRollbackKeepClientSideApply(t *testing.T) {
	for _, method := range []string{"", "csa"} {
		t.Run("recorded:"+method, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			r.helm3Record(t, method)
			_, err := r.d.Upgrade(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.1.0")})
			require.NoError(t, err)
			d, err := r.d.Get(ctx, "n", "web", 0)
			require.NoError(t, err)
			assert.Equal(t, "csa", d.ApplyMethod, "the adapter never forces server-side apply onto a client-side release")
			require.NoError(t, r.d.Rollback(ctx, "n", "web", 1))
			d, err = r.d.Get(ctx, "n", "web", 0)
			require.NoError(t, err)
			assert.Equal(t, "csa", d.ApplyMethod)
		})
	}
}

func TestApplyMethod_AServerSideReleaseStaysServerSide(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0")})
	require.NoError(t, err)
	_, err = r.d.Upgrade(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.1.0")})
	require.NoError(t, err)
	d, err := r.d.Get(ctx, "n", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, "ssa", d.ApplyMethod)
}

var _ = common.DefaultCapabilities
