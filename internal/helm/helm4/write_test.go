package helm4_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/cli"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	ri "helm.sh/helm/v4/pkg/release"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"

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

func TestOCIReference_GoesToTheLocatorUnchanged(t *testing.T) {
	r := newRig(t)
	local := helmtest.Save(t, helmtest.Chart(), r.dir)
	var gotRef, gotVersion string
	var gotSettings *cli.EnvSettings
	d := helm4.New(r.Open).WithWorkDir("/work/job1").WithLocator(func(inst *action.Install, ref string, s *cli.EnvSettings) (string, error) {
		gotRef, gotVersion, gotSettings = ref, inst.Version, s
		return local, nil
	})
	_, err := d.Install(context.Background(), helm.InstallOptions{
		Namespace: "n", Name: "web", Chart: helm.ChartSource{OCI: "oci://registry.example.com/charts/web", Version: "1.2.3"},
	})
	require.NoError(t, err)
	assert.Equal(t, "oci://registry.example.com/charts/web", gotRef)
	assert.Equal(t, "1.2.3", gotVersion)
	assert.Equal(t, "/work/job1/registry.json", gotSettings.RegistryConfig, "registry files live in the job's directory")
	assert.Equal(t, "/work/job1/cache", gotSettings.RepositoryCache)
}

func TestOCIReference_LocatorFailureIsInvalid(t *testing.T) {
	r := newRig(t)
	d := helm4.New(r.Open).WithWorkDir(t.TempDir()).WithLocator(func(*action.Install, string, *cli.EnvSettings) (string, error) {
		return "", errors.New("denied")
	})
	_, err := d.Install(context.Background(), helm.InstallOptions{Namespace: "n", Name: "web", Chart: helm.ChartSource{OCI: "oci://x/y"}})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestNoChart_Invalid(t *testing.T) {
	r := newRig(t)
	_, err := r.d.Install(context.Background(), helm.InstallOptions{Namespace: "n", Name: "web"})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

var _ = common.DefaultCapabilities

func TestUnknownReleaseOrRevision_IsNotFound(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	assert.ErrorIs(t, r.d.Rollback(ctx, "n", "nope", 1), apperr.ErrNotFound)
	assert.ErrorIs(t, r.d.Uninstall(ctx, "n", "nope"), apperr.ErrNotFound)
	_, err := r.d.Test(ctx, "n", "nope")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0")})
	require.NoError(t, err)
	assert.ErrorIs(t, r.d.Rollback(ctx, "n", "web", 9), apperr.ErrNotFound, "there is no revision 9")
}

func TestRender_OfAnExistingReleaseIsAnUpgradePreview(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, err := r.d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.0.0"), Values: map[string]any{"replicas": 1}})
	require.NoError(t, err)
	manifest, err := r.d.Render(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: r.chart(t, "1.1.0"), Values: map[string]any{"replicas": 5}})
	require.NoError(t, err)
	assert.Contains(t, manifest, `replicas: "5"`)
	st, err := r.d.Status(ctx, "n", "web")
	require.NoError(t, err)
	assert.Equal(t, 1, st.Revision, "nothing was stored")
	_, err = r.d.Render(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web"})
	assert.ErrorIs(t, err, apperr.ErrInvalid, "no chart")
}

func TestOpenFailure_IsReturned(t *testing.T) {
	boom := errors.New("no storage")
	d := helm4.New(func(string) (*action.Configuration, error) { return nil, boom })
	ctx := context.Background()
	_, err := d.List(ctx, "")
	assert.ErrorIs(t, err, boom)
	_, err = d.Get(ctx, "n", "web", 0)
	assert.ErrorIs(t, err, boom)
	_, err = d.History(ctx, "n", "web")
	assert.ErrorIs(t, err, boom)
	_, err = d.Status(ctx, "n", "web")
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, d.Uninstall(ctx, "n", "web"), boom)
	assert.ErrorIs(t, d.Rollback(ctx, "n", "web", 1), boom)
	_, err = d.Test(ctx, "n", "web")
	assert.ErrorIs(t, err, boom)
	_, err = d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web"})
	assert.ErrorIs(t, err, boom)
}

func TestOCIReference_ThatCannotBeReached_IsInvalid(t *testing.T) {
	r := newRig(t)
	d := helm4.New(r.Open).WithWorkDir(t.TempDir())
	_, err := d.Install(context.Background(), helm.InstallOptions{
		Namespace: "n", Name: "web", Chart: helm.ChartSource{OCI: "oci://127.0.0.1:1/charts/web", Version: "1.0.0"},
	})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

// brokenStorage is a Helm storage driver whose every call fails, as a cluster that cannot be read would.
type brokenStorage struct{ err error }

func (b brokenStorage) Create(string, ri.Releaser) error                   { return b.err }
func (b brokenStorage) Update(string, ri.Releaser) error                   { return b.err }
func (b brokenStorage) Delete(string) (ri.Releaser, error)                 { return nil, b.err }
func (b brokenStorage) Get(string) (ri.Releaser, error)                    { return nil, b.err }
func (b brokenStorage) List(func(ri.Releaser) bool) ([]ri.Releaser, error) { return nil, b.err }
func (b brokenStorage) Query(map[string]string) ([]ri.Releaser, error)     { return nil, b.err }
func (b brokenStorage) Name() string                                       { return "broken" }

func TestStorageFailures_AreReturned_NotMistakenForAMissingRelease(t *testing.T) {
	boom := errors.New("the cluster cannot be read")
	d := helm4.New(func(string) (*action.Configuration, error) {
		cfg := action.NewConfiguration()
		cfg.Releases = storage.Init(brokenStorage{boom})
		cfg.KubeClient = &kubefake.PrintingKubeClient{Out: io.Discard}
		return cfg, nil
	}).WithWorkDir(t.TempDir())
	ctx := context.Background()
	_, err := d.List(ctx, "")
	assert.ErrorIs(t, err, boom)
	_, err = d.Get(ctx, "n", "web", 0)
	assert.ErrorIs(t, err, boom)
	_, err = d.History(ctx, "n", "web")
	assert.ErrorIs(t, err, boom)
	_, err = d.Status(ctx, "n", "web")
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, d.Rollback(ctx, "n", "web", 1), boom)
	assert.ErrorIs(t, d.Uninstall(ctx, "n", "web"), boom)
	_, err = d.Test(ctx, "n", "web")
	assert.ErrorIs(t, err, boom)
	local := helm.ChartSource{Path: helmtest.Save(t, helmtest.Chart(), t.TempDir())}
	_, err = d.Install(ctx, helm.InstallOptions{Namespace: "n", Name: "web", Chart: local})
	assert.ErrorIs(t, err, boom, "a storage failure is not read as 'the name is free'")
	_, err = d.Upgrade(ctx, helm.UpgradeOptions{Namespace: "n", Name: "web", Chart: local})
	assert.ErrorIs(t, err, boom)
}
