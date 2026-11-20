package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/helm/helm4"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/helmtest"
)

type helmFactory struct{ env *helmtest.Env }

func (f helmFactory) Driver(_ context.Context, _ session.Session, _, _, workDir string) (helm.Driver, error) {
	return helm4.New(f.env.Open).WithWorkDir(workDir), nil
}

type helmEnv struct {
	*writeEnv
	runner *jobs.Runner
	helm   *helmtest.Env
}

// newHelmEnv has a cluster with release web (revisions 1 and 2) in team-a and a chart repository serving web 1.0.0 and 1.1.0.
func newHelmEnv(t *testing.T) *helmEnv {
	t.Helper()
	e := &helmEnv{writeEnv: newWriteEnv(t), helm: helmtest.New(t)}
	e.helm.Install(t, helmtest.Chart(), "team-a", "web", map[string]any{"password": "topsecret"})
	e.helm.Upgrade(t, helmtest.ChartVersion("web", "1.1.0", "2.1"), "team-a", "web", map[string]any{"password": "topsecret", "replicas": 3})

	dir := t.TempDir()
	for _, v := range []string{"1.0.0", "1.1.0"} {
		helmtest.Save(t, helmtest.ChartVersion("web", v, "x"), dir)
	}
	repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			fmt.Fprint(w, "apiVersion: v1\nentries:\n  web:\n    - {version: 1.0.0, urls: [web-1.0.0.tgz]}\n    - {version: 1.1.0, urls: [web-1.1.0.tgz]}\n")
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(r.URL.Path)))
	}))
	t.Cleanup(repo.Close)

	sealer, err := crypto.NewSealer(bytes.Repeat([]byte{4}, 32), testutil.Rand("helm"))
	require.NoError(t, err)
	n := 0
	e.runner = jobs.NewRunner(jobs.Deps{
		Store: store.NewJobs(e.db), Clock: e.fake, Sealer: sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReplicaID: "r1", NewID: func() string { n++; return fmt.Sprintf("hjob%d", n) }, Sessions: store.NewSessions(e.db),
	})
	svc := &helm.Service{
		Factory:  helmFactory{env: e.helm},
		Repos:    &helm.Repos{Org: []config.HelmRepo{{Name: "charts", URL: repo.URL}}, Sessions: store.NewSessions(e.db), Sealer: sealer, Clock: e.fake},
		Fetcher:  &helm.Fetcher{MaxBytes: 1 << 20},
		Audit:    e.deps.Audit,
		TempRoot: t.TempDir(),
	}
	for _, h := range helm.NewJobHandlers(svc.JobDeps()) {
		e.runner.Register(h)
	}
	t.Cleanup(e.runner.Close)
	e.deps.Jobs = e.runner
	e.deps.Helm = svc
	return e
}

func (e *helmEnv) post(path, body string) *httptest.ResponseRecorder {
	return e.sendAs(http.MethodPost, path, "application/json", body)
}
func (e *helmEnv) get(path string) *httptest.ResponseRecorder { return e.sendAs(http.MethodGet, path, "", "") }

const helmBase = "/api/v1/clusters/v:prod/helm"

func TestHelm_ListReleases(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.get(helmBase + "/releases")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Releases []struct {
			Name, Namespace, Status, Chart, ChartVersion, AppVersion string
			Revision                                                 int
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Releases, 1)
	assert.Equal(t, "web", got.Releases[0].Name)
	assert.Equal(t, "team-a", got.Releases[0].Namespace)
	assert.Equal(t, 2, got.Releases[0].Revision)
	assert.Equal(t, "1.1.0", got.Releases[0].ChartVersion)

	assert.Contains(t, e.get(helmBase+"/releases?namespace=other").Body.String(), `"releases":[]`)
	assert.Equal(t, http.StatusBadRequest, e.get(helmBase+"/releases?namespace=Bad%20Name").Code)
}

func TestHelm_Details_HideTheValues(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.get(helmBase + "/releases/team-a/web")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `"valuesHidden":true`)
	assert.NotContains(t, body, "topsecret")
	assert.NotContains(t, body, "userValues")
	assert.Contains(t, body, "kind: ConfigMap")
	assert.NotContains(t, body, "hunter2", "the manifest has Secrets masked")
	assert.Contains(t, body, `"notes":"Thank you for installing web.\n"`)
	assert.Contains(t, body, `"history":[`)

	old := e.get(helmBase + "/releases/team-a/web?revision=1")
	assert.Equal(t, http.StatusOK, old.Code)
	assert.Equal(t, "1", jsonField(t, old, "revision"))
	assert.Equal(t, http.StatusNotFound, e.get(helmBase+"/releases/team-a/nope").Code)
	assert.Equal(t, http.StatusBadRequest, e.get(helmBase+"/releases/team-a/web?revision=x").Code)
}

func TestHelm_RevealValues_IsAudited(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.post(helmBase+"/releases/team-a/web/values/reveal", `{"revision":0}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, jsonField(t, rec, "userValues"), "topsecret")
	assert.Contains(t, jsonField(t, rec, "computedValues"), "message: hello")
	assert.Equal(t, []string{"helm-values-reveal:requested", "helm-values-reveal:success"}, e.verbsAndOutcomes(t))
	assert.NotContains(t, e.auditOut.String(), "topsecret")
}

func TestHelm_RevealValues_ForAnUnknownReleaseIsStillAudited(t *testing.T) {
	e := newHelmEnv(t)
	assert.Equal(t, http.StatusNotFound, e.post(helmBase+"/releases/team-a/nope/values/reveal", `{}`).Code)
}

func TestHelm_Preview_ShowsTheCurrentAndTheNewManifest(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.post(helmBase+"/preview", `{"action":"upgrade","namespace":"team-a","name":"web","chart":{"repo":"charts","name":"web","version":"1.0.0"},"values":{"replicas":9}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, jsonField(t, rec, "currentManifest"), `replicas: "3"`)
	assert.Contains(t, jsonField(t, rec, "newManifest"), `replicas: "9"`)
	assert.NotContains(t, rec.Body.String(), "hunter2")
	assert.Equal(t, "2", jsonField2(t, e.get(helmBase+"/releases/team-a/web"), "revision"), "nothing was stored")
}

func TestHelm_Preview_OfAnInstall_HasNoCurrentManifest(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.post(helmBase+"/preview", `{"action":"install","namespace":"team-b","name":"web","chart":{"repo":"charts","name":"web"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "", jsonField(t, rec, "currentManifest"))
	assert.Contains(t, jsonField(t, rec, "newManifest"), "kind: ConfigMap")
}

func TestHelm_Preview_Validation(t *testing.T) {
	e := newHelmEnv(t)
	for name, body := range map[string]string{
		"unknown action":     `{"action":"explode","namespace":"n","name":"web"}`,
		"rollback":           `{"action":"rollback","namespace":"n","name":"web","revision":1}`,
		"no chart":           `{"action":"install","namespace":"n","name":"web"}`,
		"unknown field":      `{"action":"install","bogus":1}`,
		"not json":           `nonsense`,
		"unknown repository": `{"action":"install","namespace":"n","name":"web","chart":{"repo":"nope","name":"web"}}`,
	} {
		rec := e.post(helmBase+"/preview", body)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound}, rec.Code, name+": "+rec.Body.String())
	}
}

func TestHelm_Jobs_InstallThenUpgradeThenRollbackAndUninstall(t *testing.T) {
	e := newHelmEnv(t)
	start := func(body string) string {
		rec := e.post(helmBase+"/jobs", body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		id := jsonField(t, rec, "jobId")
		e.runner.Wait()
		job := e.get("/api/v1/jobs/" + id)
		require.Equal(t, "succeeded", jsonField(t, job, "status"), job.Body.String())
		return job.Body.String()
	}
	body := start(`{"action":"install","namespace":"team-b","name":"api","chart":{"repo":"charts","name":"web","version":"1.0.0"},"values":{"token":"s3cr3t"}}`)
	assert.Contains(t, body, `"type":"helm-install"`)
	assert.NotContains(t, body, "s3cr3t", "values are not part of the job the API shows")
	start(`{"action":"upgrade","namespace":"team-b","name":"api","chart":{"repo":"charts","name":"web","version":"1.1.0"}}`)
	start(`{"action":"rollback","namespace":"team-b","name":"api","revision":1}`)
	start(`{"action":"test","namespace":"team-b","name":"api"}`)
	start(`{"action":"uninstall","namespace":"team-b","name":"api"}`)
	assert.Equal(t, []string{
		"helm-install:requested", "helm-install:success", "helm-upgrade:requested", "helm-upgrade:success",
		"helm-rollback:requested", "helm-rollback:success", "helm-test:requested", "helm-test:success",
		"helm-uninstall:requested", "helm-uninstall:success",
	}, e.verbsAndOutcomes(t))
	assert.NotContains(t, e.auditOut.String(), "s3cr3t")
}

func TestHelm_Jobs_RecordTheStartingRevisionInTheTarget(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.post(helmBase+"/jobs", `{"action":"upgrade","namespace":"team-a","name":"web","chart":{"repo":"charts","name":"web","version":"1.0.0"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	e.runner.Wait()
	job := e.get("/api/v1/jobs/" + jsonField(t, rec, "jobId"))
	assert.Contains(t, job.Body.String(), `"startRevision":2`)
	assert.Contains(t, job.Body.String(), `"name":"web"`)
}

func TestHelm_Jobs_Validation(t *testing.T) {
	e := newHelmEnv(t)
	for name, c := range map[string]struct {
		body string
		want int
	}{
		"unknown action":          {`{"action":"explode","namespace":"n","name":"web"}`, http.StatusBadRequest},
		"install without a chart": {`{"action":"install","namespace":"n","name":"web"}`, http.StatusBadRequest},
		"bad namespace":           {`{"action":"uninstall","namespace":"Bad Ns","name":"web"}`, http.StatusBadRequest},
		"rollback without rev":    {`{"action":"rollback","namespace":"team-a","name":"web"}`, http.StatusBadRequest},
		"unknown field":           {`{"action":"uninstall","namespace":"n","name":"web","bogus":1}`, http.StatusBadRequest},
		"upgrade of a missing":    {`{"action":"upgrade","namespace":"team-a","name":"nope","chart":{"repo":"charts","name":"web"}}`, http.StatusNotFound},
		"uninstall of a missing":  {`{"action":"uninstall","namespace":"team-a","name":"nope"}`, http.StatusNotFound},
	} {
		assert.Equal(t, c.want, e.post(helmBase+"/jobs", c.body).Code, name)
	}
	assert.Empty(t, e.verbsAndOutcomes(t), "nothing was started")
}

func TestHelm_Jobs_ServerShuttingDown_503(t *testing.T) {
	e := newHelmEnv(t)
	e.runner.Close()
	rec := e.post(helmBase+"/jobs", `{"action":"uninstall","namespace":"team-a","name":"web"}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "server_busy", errorCode(t, rec))
}

func TestHelm_Repositories(t *testing.T) {
	e := newHelmEnv(t)
	rec := e.post(helmBase+"/repositories", `{"name":"mine","url":"https://charts.internal","username":"alice","password":"s3cret"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := e.get(helmBase + "/repositories")
	require.Equal(t, http.StatusOK, list.Code)
	assert.NotContains(t, list.Body.String(), "s3cret")
	assert.Contains(t, list.Body.String(), `"name":"charts"`)
	assert.Contains(t, list.Body.String(), `"scope":"org"`)
	assert.Contains(t, list.Body.String(), `"name":"mine"`)
	assert.Contains(t, list.Body.String(), `"hasAuth":true`)
	assert.Equal(t, http.StatusConflict, e.post(helmBase+"/repositories", `{"name":"charts","url":"https://x"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.post(helmBase+"/repositories", `{"name":"bad name","url":"https://x"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.post(helmBase+"/repositories", `{"name":"ok","url":"ftp://x"}`).Code)

	assert.Equal(t, http.StatusOK, e.sendAs(http.MethodDelete, helmBase+"/repositories/mine", "", "").Code)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodDelete, helmBase+"/repositories/mine", "", "").Code)
	assert.Equal(t, http.StatusForbidden, e.sendAs(http.MethodDelete, helmBase+"/repositories/charts", "", "").Code)
}

func TestHelm_UnknownClusterAndSession(t *testing.T) {
	e := newHelmEnv(t)
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/clusters/v:nope/helm/releases").Code)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, helmBase+"/releases", "", "").Code)
	assert.Equal(t, http.StatusForbidden, e.doWithSession(http.MethodPost, helmBase+"/jobs", e.id, "").Code, "a write needs the CSRF token")
}

func TestHelm_NotMountedWithoutTheService(t *testing.T) {
	e := newHelmEnv(t)
	e.deps.Helm = nil
	assert.Equal(t, http.StatusNotFound, e.get(helmBase+"/releases").Code)
}

func jsonField2(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
	t.Helper()
	return jsonField(t, rec, key)
}
