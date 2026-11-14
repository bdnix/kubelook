package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

func TestMe_NothingWiredYet_401(t *testing.T) {
	rec := newHarness("https://k.example.com").do(http.MethodGet, "/api/v1/me")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthenticated", errorCode(t, rec))
}

func TestMe_WithSessionsButNoCookie_401(t *testing.T) {
	h, _, _ := withSessions(t)
	rec := h.doWithSession(http.MethodGet, "/api/v1/me", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthenticated", errorCode(t, rec))
}

func TestMe_ValidSession_ReturnsTheFullPayload(t *testing.T) {
	h, id, csrf := withSessions(t)
	h.deps.Users = store.NewUsers(h.db, h.fake, func() string { return "unused" })
	no := false
	h.deps.Config = &config.Config{
		Server:      config.Server{PublicURL: "http://localhost:8080"},
		Kubeconfig:  config.Toggle{Enabled: &no},
		FileBrowser: config.FileBrowser{MaxTransferBytes: 1234},
		Helm:        config.Helm{Version: 4},
		Vault:       config.Vault{Address: "https://vault.example"},
		Logs:        config.Logs{HistoryMaxLines: 200000, LiveBufferLines: 10000},
		Maintenance: config.Maintenance{Banner: "back at noon"},
		Modules:     map[string]config.Module{"keda": {Enabled: "auto"}, "trivy": {Enabled: "false"}},
	}
	h.deps.KeyID = "66687aadf862bd77"

	rec := h.doWithSession(http.MethodGet, "/api/v1/me", id, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.JSONEq(t, `{"id":"u1","provider":"google","email":"e","name":"n"}`, string(got["user"]))
	assert.JSONEq(t, `"`+csrf+`"`, string(got["csrfToken"]))
	assert.JSONEq(t, `"back at noon"`, string(got["maintenanceBanner"]))
	assert.JSONEq(t, `{"kubeconfig":false,"fileBrowser":true,"nodeShell":false,"vault":true,"helmVersion":4,
		"modules":{"keda":"auto","trivy":"false"}}`, string(got["features"]))
	assert.JSONEq(t, `{"fileTransferBytes":1234,"logsHistoryMaxLines":200000,"logsLiveBufferLines":10000}`, string(got["limits"]))
	assert.JSONEq(t, `null`, string(got["vault"]))
	assert.JSONEq(t, `"66687aadf862bd77"`, string(got["keyId"]))
	var settings struct {
		WatchIdleSeconds int    `json:"watchIdleSeconds"`
		Theme            string `json:"theme"`
	}
	require.NoError(t, json.Unmarshal(got["settings"], &settings))
	assert.Equal(t, 30, settings.WatchIdleSeconds, "defaults when nothing was saved")
}

type whoamiRoutes struct{}

func (whoamiRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, audit.IdentityFrom(r.Context()))
	})
}

func TestAuditIdentity_RequestWithSession_CarriesTheUser(t *testing.T) {
	h, id, _ := withSessions(t)
	h.deps.Users = store.NewUsers(h.db, h.fake, func() string { return "unused" })
	h.deps.Auth = whoamiRoutes{}

	rec := h.doWithSession(http.MethodGet, "/api/v1/whoami", id, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got audit.Identity
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, audit.Identity{AccountProvider: "google", AccountSubject: "s", AccountEmail: "e"}, got)
}

func TestMe_UserRowGone_404(t *testing.T) {
	h, id, _ := withSessions(t)
	h.deps.Users = store.NewUsers(storetest.New(t), h.fake, func() string { return "x" }) // a different, empty database
	rec := h.doWithSession(http.MethodGet, "/api/v1/me", id, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))
}
