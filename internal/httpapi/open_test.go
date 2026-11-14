package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addVault(t *testing.T, e *credEnv) {
	t.Helper()
	require.Equal(t, http.StatusOK, e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"`+apiVaultTok+`"}`).Code)
}

func TestOpen_VaultCluster_ReturnsStateWithoutTheToken(t *testing.T) {
	e := newCredEnv(t)
	addVault(t, e)
	rec := e.post(http.MethodPost, "/api/v1/clusters/v:prod/open", `{"role":"devops-role"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		State, Server, Role string
		CredentialExpiresAt string
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "open", got.State)
	assert.Equal(t, "https://api.prod.example:6443", got.Server)
	assert.Equal(t, "devops-role", got.Role)
	assert.NotEmpty(t, got.CredentialExpiresAt)
	assert.NotContains(t, rec.Body.String(), "sa-token-A")
	assert.NotContains(t, e.logs.String(), "sa-token-A")
}

func TestOpen_EmptyBodyAndEncodedClusterID(t *testing.T) {
	e := newCredEnv(t)
	b, _ := json.Marshal(map[string]string{"label": "work", "content": apiKubeconfig})
	require.Equal(t, http.StatusOK, e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", string(b)).Code)
	rec := e.post(http.MethodPost, "/api/v1/clusters/"+url.PathEscape("k:work/ctx")+"/open", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"state":"open"`)
}

func TestOpen_Errors(t *testing.T) {
	e := newCredEnv(t)
	addVault(t, e)
	rec := e.post(http.MethodPost, "/api/v1/clusters/v:nope/open", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "cluster_unavailable", errorCode(t, rec))

	rec = e.post(http.MethodPost, "/api/v1/clusters/v:prod/open", `{"role":"admin"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = e.post(http.MethodPost, "/api/v1/clusters/v:prod/open", `{"nope":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = e.doWithSession(http.MethodPost, "/api/v1/clusters/v:prod/open", e.id, "")
	assert.Equal(t, "csrf", errorCode(t, rec))
}

func TestOpen_NotWired_404(t *testing.T) {
	e := newCredEnv(t)
	e.deps.Opener = nil
	assert.Equal(t, http.StatusNotFound, e.post(http.MethodPost, "/api/v1/clusters/v:prod/open", "").Code)
}

func TestPendingCredentials_ListAndAck(t *testing.T) {
	e := newCredEnv(t)
	rec := e.doWithSession(http.MethodGet, "/api/v1/credentials/pending", e.id, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"items":[]}`, rec.Body.String())

	rec = e.post(http.MethodPost, "/api/v1/credentials/pending/ack", `{"labels":["work"]}`)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = e.post(http.MethodPost, "/api/v1/credentials/pending/ack", `{"labels":`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	many := make([]string, 101)
	b, _ := json.Marshal(map[string]any{"labels": many})
	assert.Equal(t, http.StatusBadRequest, e.post(http.MethodPost, "/api/v1/credentials/pending/ack", string(b)).Code)
}

func TestPendingCredentials_ReturnsQueuedCopies(t *testing.T) {
	e := newCredEnv(t)
	_, err := e.db.Exec(t.Context(), `INSERT INTO session_credentials (session_hash, kind, ref, sealed, expires_at, updated_at)
SELECT id_hash, 'pending_copy', 'work', 'sealed-copy', 0, 1 FROM sessions`)
	require.NoError(t, err)
	rec := e.doWithSession(http.MethodGet, "/api/v1/credentials/pending", e.id, "")
	assert.JSONEq(t, `{"items":[{"label":"work","sealed":"sealed-copy"}]}`, rec.Body.String())
	e.post(http.MethodPost, "/api/v1/credentials/pending/ack", `{"labels":["work"]}`)
	rec = e.doWithSession(http.MethodGet, "/api/v1/credentials/pending", e.id, "")
	assert.JSONEq(t, `{"items":[]}`, rec.Body.String())
}
