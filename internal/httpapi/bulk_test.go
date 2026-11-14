package httpapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
)

type bulkEnv struct {
	*writeEnv
	runner *jobs.Runner
}

func newBulkEnv(t *testing.T) *bulkEnv {
	t.Helper()
	e := &bulkEnv{writeEnv: newWriteEnv(t)}
	sealer, err := crypto.NewSealer(bytes.Repeat([]byte{4}, 32), testutil.Rand("bulk"))
	require.NoError(t, err)
	n := 0
	e.runner = jobs.NewRunner(jobs.Deps{
		Store: store.NewJobs(e.db), Clock: e.fake, Sealer: sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReplicaID: "r1", NewID: func() string { n++; return "job" + string(rune('0'+n)) }, Sessions: store.NewSessions(e.db),
	})
	deps := actions.Deps{Clients: e.fakes, Clock: e.fake, Audit: e.deps.Audit}
	e.runner.Register(actions.NewBulkHandler(deps))
	e.runner.Register(actions.NewDrainHandler(deps))
	t.Cleanup(e.runner.Close)
	e.deps.Jobs = e.runner
	return e
}

func (e *bulkEnv) post(path, body string) *httptest.ResponseRecorder {
	return e.sendAs(http.MethodPost, path, "application/json", body)
}

const bulkPath = "/api/v1/clusters/v:prod/bulk"

func TestBulk_StartsAJobThatDeletesEachTarget(t *testing.T) {
	e := newBulkEnv(t)
	rec := e.post(bulkPath, `{"action":"delete","targets":[
		{"group":"core","version":"v1","resource":"configmaps","namespace":"ns1","name":"cm"},
		{"group":"core","version":"v1","resource":"pods","namespace":"ns1","name":"web-1"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	id := jsonField(t, rec, "jobId")
	require.NotEmpty(t, id)
	e.runner.Wait()

	job := e.sendAs(http.MethodGet, "/api/v1/jobs/"+id, "", "")
	assert.Equal(t, "succeeded", jsonField(t, job, "status"), job.Body.String())
	assert.Equal(t, []string{"delete:requested", "delete:success", "delete:requested", "delete:success"}, e.verbsAndOutcomes(t))
	assert.Contains(t, job.Body.String(), `"type":"bulk"`)
	assert.Contains(t, job.Body.String(), `"cluster":"v:prod"`)
}

func TestBulk_Validation(t *testing.T) {
	e := newBulkEnv(t)
	for name, body := range map[string]string{
		"unknown action": `{"action":"explode","targets":[{"version":"v1","resource":"pods","name":"x"}]}`,
		"no targets":     `{"action":"delete","targets":[]}`,
		"cordon a pod":   `{"action":"cordon","targets":[{"version":"v1","resource":"pods","name":"x"}]}`,
		"unknown field":  `{"action":"delete","targets":[],"bogus":1}`,
		"not json":       `nonsense`,
	} {
		assert.Equal(t, http.StatusBadRequest, e.post(bulkPath, body).Code, name)
	}
	assert.Empty(t, e.verbsAndOutcomes(t), "nothing was started")
}

func TestBulk_UnknownCluster_404(t *testing.T) {
	e := newBulkEnv(t)
	rec := e.post("/api/v1/clusters/v:nope/bulk", `{"action":"delete","targets":[{"version":"v1","resource":"pods","name":"x"}]}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestBulk_RequiresCSRF(t *testing.T) {
	e := newBulkEnv(t)
	assert.Equal(t, http.StatusForbidden, e.doWithSession(http.MethodPost, bulkPath, e.id, "").Code)
}

func TestBulk_NotMountedWithoutJobsOrAudit(t *testing.T) {
	e := newBulkEnv(t)
	body := `{"action":"delete","targets":[{"version":"v1","resource":"pods","name":"x"}]}`
	e.deps.Jobs = nil
	assert.Equal(t, http.StatusNotFound, e.post(bulkPath, body).Code)
	e.deps.Jobs = e.runner
	e.deps.Audit = nil
	assert.Equal(t, http.StatusNotFound, e.post(bulkPath, body).Code)
}

func TestDrain_StartsADrainJobForTheNode(t *testing.T) {
	e := newBulkEnv(t)
	require.NoError(t, e.fakes.Kube.Tracker().Add(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}))
	rec := e.post("/api/v1/clusters/v:prod/nodes/node-1/drain", ``)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	id := jsonField(t, rec, "jobId")
	e.runner.Wait()
	job := e.sendAs(http.MethodGet, "/api/v1/jobs/"+id, "", "")
	assert.Equal(t, "succeeded", jsonField(t, job, "status"), job.Body.String())
	assert.Contains(t, job.Body.String(), `"type":"drain"`)
	assert.Contains(t, job.Body.String(), `"name":"node-1"`)
	assert.Equal(t, []string{"drain:requested", "drain:success"}, e.verbsAndOutcomes(t))
}

func TestDrain_UnknownCluster_404(t *testing.T) {
	e := newBulkEnv(t)
	assert.Equal(t, http.StatusNotFound, e.post("/api/v1/clusters/v:nope/nodes/node-1/drain", ``).Code)
	assert.True(t, strings.HasPrefix(e.post("/api/v1/clusters/v:prod/nodes/%20/drain", ``).Body.String(), `{"error"`))
}
