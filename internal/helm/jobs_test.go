package helm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "helm.sh/helm/v4/pkg/release/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/helm/helm4"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/helmtest"
)

// factory hands out the Helm 4 adapter over one in-memory cluster and remembers the working directories.
type factory struct {
	env  *helmtest.Env
	dirs []string
}

func (f *factory) Driver(_ context.Context, _ session.Session, _, _, workDir string) (helm.Driver, error) {
	f.dirs = append(f.dirs, workDir)
	return helm4.New(f.env.Open).WithWorkDir(workDir), nil
}

type jobRig struct {
	w        *world
	f        *factory
	out      *bytes.Buffer
	tempRoot string
	handlers map[string]jobs.Handler
	repos    *helm.Repos
	audit    *audit.Recorder
}

func newJobRig(t *testing.T) *jobRig {
	t.Helper()
	w := newWorld(t)
	srv := repoServer(t, [2]string{}, nil)
	out := &bytes.Buffer{}
	n := 0
	rec := audit.NewRecorder(w.db, w.clock, out, func() string { n++; return fmt.Sprintf("a%d", n) })
	f := &factory{env: helmtest.New(t)}
	root := t.TempDir()
	repos := w.repos(config.HelmRepo{Name: "charts", URL: srv.URL})
	hs := helm.NewJobHandlers(helm.JobDeps{
		Factory:  f,
		Repos:    repos,
		Fetcher:  &helm.Fetcher{MaxBytes: 1 << 20},
		Audit:    rec,
		TempRoot: root,
	})
	m := map[string]jobs.Handler{}
	for _, h := range hs {
		m[h.Type()] = h
	}
	return &jobRig{w: w, f: f, out: out, tempRoot: root, handlers: m, repos: repos, audit: rec}
}

func (r *jobRig) job(t *testing.T, typ string, req helm.JobRequest, startRevision int) *jobs.Job {
	t.Helper()
	params, err := json.Marshal(req)
	require.NoError(t, err)
	target, err := json.Marshal(helm.JobTarget{Name: req.Name, Namespace: req.Namespace, Action: strings.TrimPrefix(typ, "helm-"), StartRevision: startRevision})
	require.NoError(t, err)
	return &jobs.Job{ID: "j1", UserID: r.w.user, SessionHash: "h1", Cluster: "c", Type: typ, Params: params, Target: target}
}

func (r *jobRig) run(t *testing.T, typ string, req helm.JobRequest, startRevision int) error {
	t.Helper()
	return r.handlers[typ].Run(t.Context(), r.job(t, typ, req, startRevision), func(jobs.Progress) {})
}

func (r *jobRig) audited() []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(r.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m struct {
			Verb, Outcome string
			Resource      struct{ Kind, Name string }
		}
		_ = json.Unmarshal([]byte(l), &m)
		if m.Outcome == "requested" {
			continue // every action is recorded twice: when it is requested and when it ends
		}
		out = append(out, fmt.Sprintf("%s:%s %s", m.Verb, m.Outcome, m.Resource.Name))
	}
	return out
}

func (r *jobRig) tempEmpty(t *testing.T) {
	t.Helper()
	left, err := os.ReadDir(r.tempRoot)
	require.NoError(t, err)
	assert.Empty(t, left, "the job's temporary directory is removed")
}

var install = helm.JobRequest{Namespace: "team-a", Name: "web", Chart: &helm.ChartRef{Repo: "charts", Name: "web", Version: "1.0.0"}, Values: map[string]any{"password": "topsecret"}}

func TestJobTypes_ShareTheHelmLimitGroup(t *testing.T) {
	r := newJobRig(t)
	assert.Len(t, r.handlers, 5)
	for typ := range r.handlers {
		assert.True(t, strings.HasPrefix(typ, "helm"), typ)
	}
}

func TestRun_Install_FromARepository_IsAuditedWithoutValues(t *testing.T) {
	r := newJobRig(t)
	require.NoError(t, r.run(t, "helm-install", install, 0))
	got, err := r.f.env.Open("team-a")
	require.NoError(t, err)
	rel, err := got.Releases.Last("web")
	require.NoError(t, err)
	assert.Equal(t, 1, rel.(*v1.Release).Version)
	assert.Equal(t, []string{"helm-install:success web"}, r.audited())
	assert.Contains(t, r.out.String(), `"1.0.0"`, "the chart version is recorded")
	assert.Contains(t, r.out.String(), `"web"`)
	assert.NotContains(t, r.out.String(), "topsecret", "values are never audited")
	r.tempEmpty(t)
}

func TestRun_UpgradeRollbackTestUninstall_AreAuditedWithTheirVerbs(t *testing.T) {
	r := newJobRig(t)
	require.NoError(t, r.run(t, "helm-install", install, 0))
	up := install
	up.Chart = &helm.ChartRef{Repo: "charts", Name: "web", Version: "1.1.0"}
	require.NoError(t, r.run(t, "helm-upgrade", up, 1))
	require.NoError(t, r.run(t, "helm-rollback", helm.JobRequest{Namespace: "team-a", Name: "web", Revision: 1}, 2))
	require.NoError(t, r.run(t, "helm-test", helm.JobRequest{Namespace: "team-a", Name: "web"}, 3))
	require.NoError(t, r.run(t, "helm-uninstall", helm.JobRequest{Namespace: "team-a", Name: "web"}, 3))
	assert.Equal(t, []string{
		"helm-install:success web", "helm-upgrade:success web", "helm-rollback:success web", "helm-test:success web", "helm-uninstall:success web",
	}, r.audited())
	r.tempEmpty(t)
}

func TestRun_AFailure_IsAuditedAndTheDirectoryIsStillRemoved(t *testing.T) {
	r := newJobRig(t)
	require.NoError(t, r.run(t, "helm-install", install, 0))
	err := r.run(t, "helm-install", install, 0)
	assert.ErrorIs(t, err, apperr.ErrConflict)
	assert.Equal(t, []string{"helm-install:success web", "helm-install:failure web"}, r.audited())
	r.tempEmpty(t)
}

func TestRun_AChartThatIsNotInTheRepository_FailsWithNotFound(t *testing.T) {
	r := newJobRig(t)
	req := install
	req.Chart = &helm.ChartRef{Repo: "charts", Name: "nope"}
	assert.ErrorIs(t, r.run(t, "helm-install", req, 0), apperr.ErrNotFound)
	r.tempEmpty(t)
}

func TestRun_AnUnknownRepository_FailsWithNotFound(t *testing.T) {
	r := newJobRig(t)
	req := install
	req.Chart = &helm.ChartRef{Repo: "elsewhere", Name: "web"}
	assert.ErrorIs(t, r.run(t, "helm-install", req, 0), apperr.ErrNotFound)
}

func TestRun_BadParameters_Invalid(t *testing.T) {
	r := newJobRig(t)
	assert.ErrorIs(t, r.run(t, "helm-install", helm.JobRequest{Namespace: "team-a", Name: "web"}, 0), apperr.ErrInvalid, "no chart")
	assert.ErrorIs(t, r.run(t, "helm-install", helm.JobRequest{Namespace: "Team A", Name: "web", Chart: install.Chart}, 0), apperr.ErrInvalid, "bad namespace")
	assert.ErrorIs(t, r.run(t, "helm-rollback", helm.JobRequest{Namespace: "n", Name: "web"}, 0), apperr.ErrInvalid, "no revision")
	both := install
	both.Chart = &helm.ChartRef{Repo: "charts", OCI: "oci://x/y", Name: "web"}
	assert.ErrorIs(t, r.run(t, "helm-install", both, 0), apperr.ErrInvalid, "a repository and an OCI reference")
	bad := install
	bad.Chart = &helm.ChartRef{OCI: "https://x/y"}
	assert.ErrorIs(t, r.run(t, "helm-install", bad, 0), apperr.ErrInvalid, "an OCI reference must start with oci://")
	j := r.job(t, "helm-install", install, 0)
	j.Params = json.RawMessage(`{"namespace":`)
	assert.ErrorIs(t, r.handlers["helm-install"].Run(t.Context(), j, func(jobs.Progress) {}), apperr.ErrInvalid)
}

// --- Recover ---

func (r *jobRig) recoverJob(t *testing.T, typ string, req helm.JobRequest, start int) (jobs.Status, string, []string) {
	t.Helper()
	return r.handlers[typ].Recover(t.Context(), r.job(t, typ, req, start))
}

func TestRecover_Install(t *testing.T) {
	t.Run("the release exists at the target chart version and values: succeeded", func(t *testing.T) {
		r := newJobRig(t)
		require.NoError(t, r.run(t, "helm-install", install, 0))
		st, _, offers := r.recoverJob(t, "helm-install", install, 0)
		assert.Equal(t, jobs.Succeeded, st)
		assert.Empty(t, offers)
	})
	t.Run("nothing was created: interrupted, retry", func(t *testing.T) {
		r := newJobRig(t)
		st, _, offers := r.recoverJob(t, "helm-install", install, 0)
		assert.Equal(t, jobs.Interrupted, st)
		assert.Equal(t, []string{jobs.OfferRetry}, offers)
	})
	t.Run("pending-install with no earlier revision: interrupted, offers uninstall", func(t *testing.T) {
		r := newJobRig(t)
		require.NoError(t, r.run(t, "helm-install", install, 0))
		r.setStatus(t, "team-a", "web", "pending-install")
		st, _, offers := r.recoverJob(t, "helm-install", install, 0)
		assert.Equal(t, jobs.Interrupted, st)
		assert.Equal(t, []string{jobs.OfferUninstall}, offers)
	})
}

func (r *jobRig) setStatus(t *testing.T, ns, name, status string) {
	t.Helper()
	cfg, err := r.f.env.Open(ns)
	require.NoError(t, err)
	last, err := cfg.Releases.Last(name)
	require.NoError(t, err)
	rel := last.(*v1.Release)
	rel.Info.Status = "pending-install"
	switch status {
	case "pending-upgrade":
		rel.Info.Status = "pending-upgrade"
	case "pending-rollback":
		rel.Info.Status = "pending-rollback"
	}
	require.NoError(t, cfg.Releases.Update(rel))
}

func TestRecover_Upgrade(t *testing.T) {
	up := install
	up.Chart = &helm.ChartRef{Repo: "charts", Name: "web", Version: "1.1.0"}
	t.Run("deployed at the target version and values: succeeded", func(t *testing.T) {
		r := newJobRig(t)
		require.NoError(t, r.run(t, "helm-install", install, 0))
		require.NoError(t, r.run(t, "helm-upgrade", up, 1))
		st, _, _ := r.recoverJob(t, "helm-upgrade", up, 1)
		assert.Equal(t, jobs.Succeeded, st)
	})
	t.Run("unchanged from the starting revision: interrupted, retry", func(t *testing.T) {
		r := newJobRig(t)
		require.NoError(t, r.run(t, "helm-install", install, 0))
		st, _, offers := r.recoverJob(t, "helm-upgrade", up, 1)
		assert.Equal(t, jobs.Interrupted, st)
		assert.Equal(t, []string{jobs.OfferRetry}, offers)
	})
	t.Run("pending-upgrade: interrupted, offers rollback", func(t *testing.T) {
		r := newJobRig(t)
		require.NoError(t, r.run(t, "helm-install", install, 0))
		require.NoError(t, r.run(t, "helm-upgrade", up, 1))
		r.setStatus(t, "team-a", "web", "pending-upgrade")
		st, _, offers := r.recoverJob(t, "helm-upgrade", up, 1)
		assert.Equal(t, jobs.Interrupted, st)
		assert.Equal(t, []string{jobs.OfferRollback}, offers)
	})
	t.Run("deployed at the target version but with other values: not succeeded", func(t *testing.T) {
		r := newJobRig(t)
		require.NoError(t, r.run(t, "helm-install", install, 0))
		require.NoError(t, r.run(t, "helm-upgrade", up, 1))
		other := up
		other.Values = map[string]any{"password": "different"}
		st, _, _ := r.recoverJob(t, "helm-upgrade", other, 1)
		assert.Equal(t, jobs.Interrupted, st)
	})
}

func TestRecover_Rollback(t *testing.T) {
	rb := helm.JobRequest{Namespace: "team-a", Name: "web", Revision: 1}
	r := newJobRig(t)
	require.NoError(t, r.run(t, "helm-install", install, 0))
	up := install
	up.Chart = &helm.ChartRef{Repo: "charts", Name: "web", Version: "1.1.0"}
	require.NoError(t, r.run(t, "helm-upgrade", up, 1))
	st, _, offers := r.recoverJob(t, "helm-rollback", rb, 2)
	assert.Equal(t, jobs.Interrupted, st, "still at the starting revision")
	assert.Equal(t, []string{jobs.OfferRetry}, offers)
	require.NoError(t, r.run(t, "helm-rollback", rb, 2))
	st, _, _ = r.recoverJob(t, "helm-rollback", rb, 2)
	assert.Equal(t, jobs.Succeeded, st)
	r.setStatus(t, "team-a", "web", "pending-rollback")
	st, _, offers = r.recoverJob(t, "helm-rollback", rb, 2)
	assert.Equal(t, jobs.Interrupted, st)
	assert.Equal(t, []string{jobs.OfferRollback}, offers)
}

func TestRecover_Uninstall(t *testing.T) {
	r := newJobRig(t)
	req := helm.JobRequest{Namespace: "team-a", Name: "web"}
	require.NoError(t, r.run(t, "helm-install", install, 0))
	st, _, offers := r.recoverJob(t, "helm-uninstall", req, 1)
	assert.Equal(t, jobs.Interrupted, st)
	assert.Equal(t, []string{jobs.OfferRetry}, offers)
	require.NoError(t, r.run(t, "helm-uninstall", req, 1))
	st, _, _ = r.recoverJob(t, "helm-uninstall", req, 1)
	assert.Equal(t, jobs.Succeeded, st, "the release is gone")
}

func TestRecover_Test_CannotBeChecked(t *testing.T) {
	r := newJobRig(t)
	st, _, offers := r.recoverJob(t, "helm-test", helm.JobRequest{Namespace: "team-a", Name: "web"}, 1)
	assert.Equal(t, jobs.Interrupted, st)
	assert.Equal(t, []string{jobs.OfferRetry}, offers)
}

// Classify is the decision of Recover on what was observed.
func TestClassify(t *testing.T) {
	deployed := func(rev int, version string, values bool) helm.Observed {
		return helm.Observed{Exists: true, Revision: rev, Status: "deployed", ChartVersion: version, ValuesMatch: values}
	}
	tests := []struct {
		name   string
		action string
		start  int
		want   string
		seen   helm.Observed
		status jobs.Status
		offers []string
	}{
		{"install done", "install", 0, "1.0.0", deployed(1, "1.0.0", true), jobs.Succeeded, nil},
		{"install absent", "install", 0, "1.0.0", helm.Observed{}, jobs.Interrupted, []string{jobs.OfferRetry}},
		{"install pending", "install", 0, "1.0.0", helm.Observed{Exists: true, Revision: 1, Status: "pending-install"}, jobs.Interrupted, []string{jobs.OfferUninstall}},
		{"install failed status", "install", 0, "1.0.0", helm.Observed{Exists: true, Revision: 1, Status: "failed"}, jobs.Failed, nil},
		{"upgrade done", "upgrade", 1, "1.1.0", deployed(2, "1.1.0", true), jobs.Succeeded, nil},
		{"upgrade unchanged", "upgrade", 1, "1.1.0", deployed(1, "1.0.0", true), jobs.Interrupted, []string{jobs.OfferRetry}},
		{"upgrade pending", "upgrade", 1, "1.1.0", helm.Observed{Exists: true, Revision: 2, Status: "pending-upgrade"}, jobs.Interrupted, []string{jobs.OfferRollback}},
		{"upgrade pending rollback", "upgrade", 1, "1.1.0", helm.Observed{Exists: true, Revision: 2, Status: "pending-rollback"}, jobs.Interrupted, []string{jobs.OfferRollback}},
		{"upgrade other values", "upgrade", 1, "1.1.0", deployed(2, "1.1.0", false), jobs.Interrupted, []string{jobs.OfferRetry}},
		{"upgrade failed", "upgrade", 1, "1.1.0", helm.Observed{Exists: true, Revision: 2, Status: "failed"}, jobs.Failed, nil},
		{"upgrade release gone", "upgrade", 1, "1.1.0", helm.Observed{}, jobs.Interrupted, []string{jobs.OfferRetry}},
		{"rollback done", "rollback", 2, "", deployed(3, "1.0.0", true), jobs.Succeeded, nil},
		{"rollback unchanged", "rollback", 2, "", deployed(2, "1.1.0", true), jobs.Interrupted, []string{jobs.OfferRetry}},
		{"rollback pending", "rollback", 2, "", helm.Observed{Exists: true, Revision: 3, Status: "pending-rollback"}, jobs.Interrupted, []string{jobs.OfferRollback}},
		{"uninstall gone", "uninstall", 3, "", helm.Observed{}, jobs.Succeeded, nil},
		{"uninstall still there", "uninstall", 3, "", deployed(3, "1.0.0", true), jobs.Interrupted, []string{jobs.OfferRetry}},
		{"test", "test", 3, "", deployed(3, "1.0.0", true), jobs.Interrupted, []string{jobs.OfferRetry}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, msg, offers := helm.Classify(tc.action, tc.start, tc.want, tc.seen)
			assert.Equal(t, tc.status, st)
			assert.Equal(t, tc.offers, offers)
			assert.NotEmpty(t, msg)
		})
	}
}

func TestRecover_WithoutAChartVersion_UsesTheNewestTheRepositoryOffers(t *testing.T) {
	r := newJobRig(t)
	req := install
	req.Chart = &helm.ChartRef{Repo: "charts", Name: "web"}
	require.NoError(t, r.run(t, "helm-install", req, 0))
	st, _, _ := r.recoverJob(t, "helm-install", req, 0)
	assert.Equal(t, jobs.Succeeded, st, "the newest release of the chart is 1.1.0")
	r.tempEmpty(t)

	missing := req
	missing.Chart = &helm.ChartRef{Repo: "nope", Name: "web"}
	st, _, _ = r.recoverJob(t, "helm-install", missing, 0)
	assert.Equal(t, jobs.Interrupted, st)
}

func TestRecover_BadParameters_CannotBeChecked(t *testing.T) {
	r := newJobRig(t)
	j := r.job(t, "helm-install", install, 0)
	j.Params = json.RawMessage(`{`)
	st, _, offers := r.handlers["helm-install"].Recover(t.Context(), j)
	assert.Equal(t, jobs.Interrupted, st)
	assert.Empty(t, offers)
}

func TestSubmit_And_Preview_RefuseWhatIsNotHelmWork(t *testing.T) {
	r := newJobRig(t)
	svc := &helm.Service{Factory: r.f, Repos: r.w.repos(), Fetcher: &helm.Fetcher{MaxBytes: 1}, TempRoot: r.tempRoot}
	_, _, err := svc.Preview(t.Context(), r.w.session("h1"), "c", "rollback", helm.JobRequest{Namespace: "n", Name: "web", Revision: 1})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	_, err = svc.Submit(t.Context(), nil, r.w.session("h1"), "c", "explode", helm.JobRequest{})
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}
