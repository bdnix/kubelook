package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
)

// failingHandler fails the first time it runs and succeeds afterwards.
type failingHandler struct{ runs int }

func (h *failingHandler) Type() string { return "bulk" }
func (h *failingHandler) Run(context.Context, *jobs.Job, func(jobs.Progress)) error {
	h.runs++
	if h.runs == 1 {
		return errors.New("first run fails")
	}
	return nil
}
func (h *failingHandler) Recover(context.Context, *jobs.Job) (jobs.Status, string, []string) {
	return jobs.Interrupted, "", nil
}

type jobsEnv struct {
	*kubeEnv
	runner *jobs.Runner
}

func newJobsEnv(t *testing.T) *jobsEnv {
	t.Helper()
	e := &jobsEnv{kubeEnv: newKubeEnv(t)}
	sealer, err := crypto.NewSealer(bytes.Repeat([]byte{3}, 32), testutil.Rand("jobs"))
	require.NoError(t, err)
	n := 0
	e.runner = jobs.NewRunner(jobs.Deps{
		Store: store.NewJobs(e.db), Clock: e.fake, Sealer: sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReplicaID: "r1", NewID: func() string { n++; return "job" + string(rune('0'+n)) }, Sessions: store.NewSessions(e.db),
	})
	e.runner.Register(&failingHandler{})
	t.Cleanup(e.runner.Close)
	e.deps.Jobs = e.runner
	return e
}

func (e *jobsEnv) submit(t *testing.T) string {
	t.Helper()
	s, err := e.deps.Sessions.Load(t.Context(), requestWithCookie(e.id))
	require.NoError(t, err)
	id, err := e.runner.Submit(t.Context(), s, "v:prod", "bulk", map[string]string{"name": "n"}, nil)
	require.NoError(t, err)
	e.runner.Wait()
	return id
}

func requestWithCookie(id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "kubelook_session", Value: id})
	return req
}

func (e *jobsEnv) post(path string) *httptest.ResponseRecorder {
	return e.doWithSession(http.MethodPost, path, e.id, e.csrf)
}

func TestJobsAPI_ListAndGet_ShowTheUsersJobs(t *testing.T) {
	e := newJobsEnv(t)
	id := e.submit(t)

	rec := e.get("/api/v1/jobs")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list struct {
		Jobs []struct {
			ID, Cluster, Type, Status, Message string
			Offers                             []string
			Progress                           struct{ Done, Total int }
			Target                             map[string]string
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Jobs, 1)
	j := list.Jobs[0]
	assert.Equal(t, id, j.ID)
	assert.Equal(t, "v:prod", j.Cluster)
	assert.Equal(t, "failed", j.Status)
	assert.Equal(t, "first run fails", j.Message)
	assert.Equal(t, []string{"retry"}, j.Offers)
	assert.Equal(t, "n", j.Target["name"])
	assert.NotContains(t, rec.Body.String(), "sealed", "no sealed material leaves the server")
	assert.NotContains(t, rec.Body.String(), "sessionHash")

	one := e.get("/api/v1/jobs/" + id)
	require.Equal(t, http.StatusOK, one.Code)
	assert.Equal(t, id, jsonField(t, one, "id"))
}

func TestJobsAPI_ActiveFilter(t *testing.T) {
	e := newJobsEnv(t)
	e.submit(t)
	var active, finished struct{ Jobs []any }
	require.NoError(t, json.Unmarshal(e.get("/api/v1/jobs?active=true").Body.Bytes(), &active))
	require.NoError(t, json.Unmarshal(e.get("/api/v1/jobs?active=false").Body.Bytes(), &finished))
	assert.Empty(t, active.Jobs)
	assert.Len(t, finished.Jobs, 1)
	assert.Equal(t, http.StatusBadRequest, e.get("/api/v1/jobs?active=maybe").Code)
}

func TestJobsAPI_Get_UnknownIsNotFound(t *testing.T) {
	e := newJobsEnv(t)
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/jobs/nope").Code)
}

func TestJobsAPI_OtherUsersGetNotFound(t *testing.T) {
	e := newJobsEnv(t)
	id := e.submit(t)
	other, err := store.NewUsers(e.db, e.fake, func() string { return "u2" }).Upsert(t.Context(), "google", "s2", "e2", "n2")
	require.NoError(t, err)
	oid, os, err := e.deps.Sessions.Create(t.Context(), other.ID)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNotFound, e.doWithSession(http.MethodGet, "/api/v1/jobs/"+id, oid, "").Code)
	assert.Equal(t, http.StatusNotFound, e.doWithSession(http.MethodPost, "/api/v1/jobs/"+id+"/retry", oid, os.CSRFToken).Code)
	rec := e.doWithSession(http.MethodGet, "/api/v1/jobs", oid, "")
	assert.JSONEq(t, `{"jobs":[]}`, rec.Body.String())
}

func TestJobsAPI_Retry_StartsANewJob(t *testing.T) {
	e := newJobsEnv(t)
	id := e.submit(t)
	rec := e.post("/api/v1/jobs/" + id + "/retry")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	newID := jsonField(t, rec, "jobId")
	assert.NotEmpty(t, newID)
	assert.NotEqual(t, id, newID)
	e.runner.Wait()
	got := e.get("/api/v1/jobs/" + newID)
	assert.Equal(t, "succeeded", jsonField(t, got, "status"))
}

func TestJobsAPI_Retry_RequiresCSRF_AndASession(t *testing.T) {
	e := newJobsEnv(t)
	id := e.submit(t)
	assert.Equal(t, http.StatusForbidden, e.doWithSession(http.MethodPost, "/api/v1/jobs/"+id+"/retry", e.id, "").Code)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/jobs", "", "").Code)
}

func TestJobsAPI_NotMountedWithoutARunner(t *testing.T) {
	e := newJobsEnv(t)
	e.deps.Jobs = nil
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/jobs").Code)
}

func jsonField(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m), strings.TrimSpace(rec.Body.String()))
	s, _ := m[key].(string)
	return s
}

var _ = httpapi.New
