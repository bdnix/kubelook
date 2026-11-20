package helm_test

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/store"
)

// svcRig is a Service over the job rig, with a runner for Submit.
type svcRig struct {
	*jobRig
	svc    *helm.Service
	runner *jobs.Runner
}

func newSvcRig(t *testing.T) *svcRig {
	t.Helper()
	r := newJobRig(t)
	svc := &helm.Service{
		Factory: r.f, Fetcher: &helm.Fetcher{MaxBytes: 1 << 20}, Audit: nil, TempRoot: r.tempRoot,
	}
	// the rig's repositories and audit recorder, through the handlers' dependencies
	n := 0
	runner := jobs.NewRunner(jobs.Deps{
		Store: store.NewJobs(r.w.db), Clock: r.w.clock, Sealer: r.w.sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReplicaID: "r1", NewID: func() string { n++; return fmt.Sprintf("sjob%d", n) }, Sessions: store.NewSessions(r.w.db),
	})
	t.Cleanup(runner.Close)
	svc.Repos = r.repos
	svc.Audit = r.audit
	for _, h := range helm.NewJobHandlers(svc.JobDeps()) {
		runner.Register(h)
	}
	return &svcRig{jobRig: r, svc: svc, runner: runner}
}

func TestService_ListDetailAndReveal(t *testing.T) {
	r := newSvcRig(t)
	require.NoError(t, r.run(t, "helm-install", install, 0))
	sess := r.w.session("h1")

	list, err := r.svc.List(t.Context(), sess, "c", "")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "web", list[0].Name)

	d, hist, err := r.svc.Detail(t.Context(), sess, "c", "team-a", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, d.Revision)
	assert.Len(t, hist, 1)
	_, _, err = r.svc.Detail(t.Context(), sess, "c", "team-a", "nope", 0)
	assert.ErrorIs(t, err, apperr.ErrNotFound)

	user, computed, err := r.svc.Reveal(t.Context(), sess, "c", "team-a", "web", 0)
	require.NoError(t, err)
	assert.Contains(t, user, "topsecret")
	assert.Contains(t, computed, "password: topsecret")
	assert.Equal(t, []string{"helm-install:success web", "helm-values-reveal:success web"}, r.audited())
	_, _, err = r.svc.Reveal(t.Context(), sess, "c", "team-a", "nope", 0)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	assert.Contains(t, r.audited(), "helm-values-reveal:failure nope")
}

func TestService_Preview(t *testing.T) {
	r := newSvcRig(t)
	sess := r.w.session("h1")
	req := helm.JobRequest{Namespace: "team-a", Name: "web", Chart: &helm.ChartRef{Repo: "charts", Name: "web", Version: "1.0.0"}, Values: map[string]any{"replicas": 4}}
	current, next, err := r.svc.Preview(t.Context(), sess, "c", helm.ActionInstall, req)
	require.NoError(t, err)
	assert.Empty(t, current, "nothing is deployed yet")
	assert.Contains(t, next, `replicas: "4"`)

	require.NoError(t, r.run(t, "helm-install", install, 0))
	current, next, err = r.svc.Preview(t.Context(), sess, "c", helm.ActionUpgrade, req)
	require.NoError(t, err)
	assert.Contains(t, current, "kind: ConfigMap")
	assert.Contains(t, next, `replicas: "4"`)

	bad := req
	bad.Chart = &helm.ChartRef{Repo: "nope", Name: "web"}
	_, _, err = r.svc.Preview(t.Context(), sess, "c", helm.ActionInstall, bad)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	bad.Namespace = "Bad Ns"
	_, _, err = r.svc.Preview(t.Context(), sess, "c", helm.ActionInstall, bad)
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestService_Submit_RecordsTheStartingRevision(t *testing.T) {
	r := newSvcRig(t)
	sess := r.w.session("h1")
	require.NoError(t, r.run(t, "helm-install", install, 0))
	up := install
	up.Chart = &helm.ChartRef{Repo: "charts", Name: "web", Version: "1.1.0"}
	id, err := r.svc.Submit(t.Context(), r.runner, sess, "c", helm.ActionUpgrade, up)
	require.NoError(t, err)
	r.runner.Wait()
	job, err := r.runner.Get(t.Context(), r.w.user, id)
	require.NoError(t, err)
	assert.Equal(t, jobs.Succeeded, job.Status)
	assert.Contains(t, string(job.Target), `"startRevision":1`)
	assert.NotContains(t, string(job.Target), "topsecret")

	_, err = r.svc.Submit(t.Context(), r.runner, sess, "c", helm.ActionUpgrade, helm.JobRequest{Namespace: "team-a", Name: "nope", Chart: up.Chart})
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	id, err = r.svc.Submit(t.Context(), r.runner, sess, "c", helm.ActionInstall, helm.JobRequest{Namespace: "team-b", Name: "fresh", Chart: &helm.ChartRef{OCI: "oci://example.com/web", Version: "1"}})
	require.NoError(t, err)
	r.runner.Wait()
	job, err = r.runner.Get(t.Context(), r.w.user, id)
	require.NoError(t, err)
	assert.Contains(t, string(job.Target), `"chart":"oci://example.com/web"`, "an OCI chart is named by its reference")
}
