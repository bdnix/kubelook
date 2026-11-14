package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const downloadPath = "/api/v1/clusters/v:prod/logs/downloads"

func TestLogDownload_IsAuditedAndAnswers204(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, downloadPath, "application/json",
		`{"namespace":"ns1","pod":"web-1","container":"app","format":"ndjson","from":"2026-11-14T12:00:00Z","to":"2026-11-14T12:05:00Z","lines":1234}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, []string{"log-download:success"}, e.verbsAndOutcomes(t))
	lines := e.auditLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, "ns1", lines[0]["namespace"])
	assert.Equal(t, map[string]any{"kind": "Pod", "name": "web-1"}, lines[0]["resource"])
	assert.Equal(t, map[string]any{
		"container": "app", "format": "ndjson", "from": "2026-11-14T12:00:00Z", "to": "2026-11-14T12:05:00Z", "lines": float64(1234),
	}, lines[0]["details"])
}

func TestLogDownload_Validation(t *testing.T) {
	e := newWriteEnv(t)
	for name, body := range map[string]string{
		"no pod":        `{"namespace":"ns1","format":"text"}`,
		"no namespace":  `{"pod":"web-1","format":"text"}`,
		"bad format":    `{"namespace":"ns1","pod":"web-1","format":"pdf"}`,
		"no format":     `{"namespace":"ns1","pod":"web-1"}`,
		"negative":      `{"namespace":"ns1","pod":"web-1","format":"text","lines":-1}`,
		"bad from":      `{"namespace":"ns1","pod":"web-1","format":"text","from":"yesterday"}`,
		"bad to":        `{"namespace":"ns1","pod":"web-1","format":"text","to":"tomorrow"}`,
		"unknown field": `{"namespace":"ns1","pod":"web-1","format":"text","content":"secret"}`,
		"not json":      `nonsense`,
	} {
		assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, downloadPath, "application/json", body).Code, name)
	}
	assert.Empty(t, e.verbsAndOutcomes(t), "nothing was recorded")
}

func TestLogDownload_TheRecordCarriesNoLogText(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, downloadPath, "application/json", `{"namespace":"ns1","pod":"web-1","format":"text","lines":3}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.NotContains(t, e.auditOut.String(), "content")
}

func TestLogDownload_NoRecord_NoDownload(t *testing.T) {
	e := newWriteEnv(t)
	require.NoError(t, e.db.Close())
	rec := e.sendAs(http.MethodPost, downloadPath, "application/json", `{"namespace":"ns1","pod":"web-1","format":"text"}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "the browser saves nothing without a record")
}

func TestLogDownload_UnknownCluster_404_NotMountedWithoutAudit_RequiresCSRF(t *testing.T) {
	e := newWriteEnv(t)
	body := `{"namespace":"ns1","pod":"web-1","format":"text"}`
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, "/api/v1/clusters/v:nope/logs/downloads", "application/json", body).Code)
	assert.Equal(t, http.StatusForbidden, e.doWithSession(http.MethodPost, downloadPath, e.id, "").Code)
	e.deps.Audit = nil
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, downloadPath, "application/json", body).Code)
}
