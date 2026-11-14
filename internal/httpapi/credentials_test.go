package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakevault"
	"github.com/bdnix/kubelook/internal/vault"
)

const apiVaultTok = "hvs.API-TEST-TOKEN"

type credEnv struct {
	*harness
	id, csrf string
	vault    *fakevault.Server
	auditOut *bytes.Buffer
}

func newCredEnv(t *testing.T) *credEnv {
	t.Helper()
	h, id, csrf := withSessions(t)
	h.deps.Users = store.NewUsers(h.db, h.fake, func() string { return "unused" })
	fv := fakevault.New()
	t.Cleanup(fv.Close)
	fv.SetNow(h.fake.Now)
	fv.AddToken(apiVaultTok, fakevault.Token{
		TTLSeconds: 1800, Renewable: true, RenewTTLSeconds: 3600, EntityID: "ent-9", DisplayName: "oidc-grace",
		Caps: map[string][]string{"kubernetes/prod/config": {"read"}, "kubernetes/prod/creds/devops-role": {"update"}},
	})
	fv.AddMount("kubernetes/prod/", "kubernetes")
	sealer, err := crypto.NewSealer(make([]byte, 32), testutil.Rand("seal"))
	require.NoError(t, err)
	e := &credEnv{harness: h, id: id, csrf: csrf, vault: fv, auditOut: &bytes.Buffer{}}
	mgr := h.deps.Sessions
	cfg := &config.Config{Vault: config.Vault{
		Address: fv.URL(), KubernetesMountPrefix: "kubernetes/", CandidateRoles: []string{"devops-role"},
		ExpiryWarning: config.Duration(30 * time.Minute),
	}}
	h.deps.Config = cfg
	cache := cluster.NewDiscoveryCache(h.fake, mgr.Registry())
	vc := vault.NewClient(fv.URL(), fv.Client())
	h.deps.Credentials = cluster.NewCredentials(cluster.Deps{
		Sessions: store.NewSessions(h.db), Sealer: sealer, Vault: vc, Clock: h.fake, Config: cfg,
		Audit: audit.NewRecorder(h.db, h.fake, e.auditOut, func() string { return "audit-id" }), Registry: mgr.Registry(), Ender: mgr, Log: h.deps.Log,
		Cache: cache,
	})
	h.deps.Clusters = cluster.NewRegistry(cluster.RegistryDeps{
		Credentials: h.deps.Credentials, Vault: vc, Sessions: store.NewSessions(h.db), Cache: cache, Config: cfg, Log: h.deps.Log,
	})
	return e
}

func (e *credEnv) post(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "kubelook_session", Value: e.id})
	req.Header.Set("X-CSRF-Token", e.csrf)
	rec := httptest.NewRecorder()
	httpapi.New(e.deps).ServeHTTP(rec, req)
	return rec
}

func TestCredentials_AddVaultToken_ReturnsSealedCopyAndStatusWithoutThePlaintext(t *testing.T) {
	e := newCredEnv(t)
	rec := e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"`+apiVaultTok+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Sealed string
		Vault  struct {
			DisplayName string
			EntityID    string
			TTLSeconds  int
			CanRenew    bool
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotEmpty(t, got.Sealed)
	assert.Equal(t, "oidc-grace", got.Vault.DisplayName)
	assert.Equal(t, "ent-9", got.Vault.EntityID)
	assert.Equal(t, 1800, got.Vault.TTLSeconds)
	assert.True(t, got.Vault.CanRenew)
	assert.NotContains(t, rec.Body.String(), apiVaultTok)
	assert.NotContains(t, e.logs.String(), apiVaultTok)
	assert.NotContains(t, e.auditOut.String(), apiVaultTok)
}

func TestCredentials_AddVaultToken_BadInput(t *testing.T) {
	e := newCredEnv(t)
	for name, body := range map[string]string{
		"empty token": `{"token":""}`, "missing token": `{}`, "unknown field": `{"token":"x","extra":1}`, "not json": `nope`,
	} {
		rec := e.post(http.MethodPost, "/api/v1/credentials/vault", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Equal(t, "bad_request", errorCode(t, rec), name)
	}
	rec := e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"wrong"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Vault rejected this token")
}

func TestCredentials_Restore_ReturnsResultsPerItem(t *testing.T) {
	e := newCredEnv(t)
	add := e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"`+apiVaultTok+`"}`)
	var added struct{ Sealed string }
	require.NoError(t, json.Unmarshal(add.Body.Bytes(), &added))

	rec := e.post(http.MethodPost, "/api/v1/credentials/restore", `{"items":[{"sealed":"`+added.Sealed+`"},{"sealed":"garbage"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Results []struct {
			Status   string
			Kind     string
			Label    string
			Clusters []struct{ Name string }
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Results, 2)
	assert.Equal(t, "ok", got.Results[0].Status)
	assert.Equal(t, "vault_token", got.Results[0].Kind)
	assert.Equal(t, "oidc-grace", got.Results[0].Label)
	require.Len(t, got.Results[0].Clusters, 1)
	assert.Equal(t, "prod", got.Results[0].Clusters[0].Name)
	assert.Equal(t, "invalid", got.Results[1].Status)

	assert.Equal(t, http.StatusBadRequest, e.post(http.MethodPost, "/api/v1/credentials/restore", `{"items":"x"}`).Code)
}

func TestCredentials_Renew_AndDelete(t *testing.T) {
	e := newCredEnv(t)
	assert.Equal(t, http.StatusNotFound, e.post(http.MethodPost, "/api/v1/credentials/vault/renew", "").Code, "no token yet")
	e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"`+apiVaultTok+`"}`)

	rec := e.post(http.MethodPost, "/api/v1/credentials/vault/renew", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ Vault struct{ TTLSeconds int } }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, 3600, got.Vault.TTLSeconds)

	del := e.post(http.MethodDelete, "/api/v1/credentials/vault", "")
	assert.Equal(t, http.StatusNoContent, del.Code)
	assert.Equal(t, http.StatusNotFound, e.post(http.MethodPost, "/api/v1/credentials/vault/renew", "").Code)
}

func TestCredentials_NotConfigured_AllFourEndpoints404(t *testing.T) {
	e := newCredEnv(t)
	e.deps.Credentials = nil
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/credentials/vault"}, {http.MethodPost, "/api/v1/credentials/restore"},
		{http.MethodPost, "/api/v1/credentials/vault/renew"}, {http.MethodDelete, "/api/v1/credentials/vault"},
	} {
		rec := e.post(c.method, c.path, `{}`)
		assert.Equal(t, http.StatusNotFound, rec.Code, c.path)
		assert.Equal(t, "not_found", errorCode(t, rec), c.path)
	}
}

func TestCredentials_NoVaultAddress_VaultEndpoints404ButKubeconfigStillWorks(t *testing.T) {
	e := newCredEnv(t)
	e.deps.Config.Vault.Address = ""
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/credentials/vault"}, {http.MethodPost, "/api/v1/credentials/vault/renew"}, {http.MethodDelete, "/api/v1/credentials/vault"},
	} {
		assert.Equal(t, http.StatusNotFound, e.post(c.method, c.path, `{"token":"x"}`).Code, c.path)
	}
	b, _ := json.Marshal(map[string]string{"label": "work", "content": apiKubeconfig})
	assert.Equal(t, http.StatusOK, e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", string(b)).Code)
}

func TestCredentials_RequireSessionAndCSRF(t *testing.T) {
	e := newCredEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials/vault", strings.NewReader(`{"token":"x"}`))
	rec := httptest.NewRecorder()
	httpapi.New(e.deps).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = e.doWithSession(http.MethodPost, "/api/v1/credentials/vault", e.id, "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "csrf", errorCode(t, rec))
}

func TestAuditIdentity_IncludesTheVaultIdentityWhenATokenIsInTheSession(t *testing.T) {
	e := newCredEnv(t)
	e.deps.Auth = whoamiRoutes{}
	var before audit.Identity
	rec := e.doWithSession(http.MethodGet, "/api/v1/whoami", e.id, "")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &before))
	assert.Empty(t, before.VaultEntityID)

	e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"`+apiVaultTok+`"}`)
	rec = e.doWithSession(http.MethodGet, "/api/v1/whoami", e.id, "")
	var after audit.Identity
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &after))
	assert.Equal(t, "ent-9", after.VaultEntityID)
	assert.Equal(t, "oidc-grace", after.VaultDisplayName)
	assert.Equal(t, "google", after.AccountProvider)
}

const apiKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "https://api.example:6443", certificate-authority-data: Y2E=}
users:
- name: u
  user: {token: api-static-token}
contexts:
- name: ctx
  context: {cluster: c, user: u}
`

func TestCredentials_Kubeconfig_UploadRestoreDelete(t *testing.T) {
	e := newCredEnv(t)
	body, err := json.Marshal(map[string]string{"label": "work", "content": apiKubeconfig})
	require.NoError(t, err)
	rec := e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", string(body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var up struct {
		Sealed   string
		Label    string
		Clusters []struct{ ID, Name, Context, User, Server string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &up))
	assert.Equal(t, "work", up.Label)
	assert.NotEmpty(t, up.Sealed)
	require.Len(t, up.Clusters, 1)
	assert.Equal(t, "k:work/ctx", up.Clusters[0].ID)
	assert.Equal(t, "https://api.example:6443", up.Clusters[0].Server)
	assert.NotContains(t, rec.Body.String(), "api-static-token")
	assert.NotContains(t, e.logs.String(), "api-static-token")

	del := e.post(http.MethodDelete, "/api/v1/credentials/kubeconfig/work", "")
	assert.Equal(t, http.StatusNoContent, del.Code)

	res := e.post(http.MethodPost, "/api/v1/credentials/restore", `{"items":[{"sealed":"`+up.Sealed+`"}]}`)
	require.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), `"status":"ok"`)
	assert.Contains(t, res.Body.String(), `"kind":"kubeconfig"`)
}

func TestCredentials_Kubeconfig_Rejections(t *testing.T) {
	e := newCredEnv(t)
	exec := strings.Replace(apiKubeconfig, "user: {token: api-static-token}", "user: {exec: {apiVersion: client.authentication.k8s.io/v1beta1, command: aws}}", 1)
	b, _ := json.Marshal(map[string]string{"label": "work", "content": exec})
	rec := e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", string(b))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "exec credential plugins are not supported")

	assert.Equal(t, http.StatusBadRequest, e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", `{"label":"","content":"x"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", `nope`).Code)

	no := false
	e.deps.Config.Kubeconfig.Enabled = &no
	b, _ = json.Marshal(map[string]string{"label": "work", "content": apiKubeconfig})
	assert.Equal(t, http.StatusNotFound, e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", string(b)).Code)
}

func TestCredentials_HandlersWithoutASessionInContext_401(t *testing.T) {
	e := newCredEnv(t)
	e.deps.Sessions = nil // no session middleware: handlers must refuse by themselves
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/credentials/vault"}, {http.MethodPost, "/api/v1/credentials/restore"},
		{http.MethodPost, "/api/v1/credentials/vault/renew"}, {http.MethodDelete, "/api/v1/credentials/vault"},
		{http.MethodPost, "/api/v1/credentials/kubeconfig"}, {http.MethodDelete, "/api/v1/credentials/kubeconfig/x"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		httpapi.New(e.deps).ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, c.path)
	}
}
