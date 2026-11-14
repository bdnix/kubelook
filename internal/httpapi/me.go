package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

type meUser struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

type meFeatures struct {
	Kubeconfig  bool              `json:"kubeconfig"`
	FileBrowser bool              `json:"fileBrowser"`
	NodeShell   bool              `json:"nodeShell"`
	Vault       bool              `json:"vault"`
	HelmVersion int               `json:"helmVersion"`
	Modules     map[string]string `json:"modules"` // module name → "auto" | "false"
}

type meLimits struct {
	FileTransferBytes   int64 `json:"fileTransferBytes"`
	LogsHistoryMaxLines int   `json:"logsHistoryMaxLines"`
	LogsLiveBufferLines int   `json:"logsLiveBufferLines"`
}

type meResponse struct {
	User              meUser         `json:"user"`
	CSRFToken         string         `json:"csrfToken"`
	Settings          store.Settings `json:"settings"`
	MaintenanceBanner string         `json:"maintenanceBanner"`
	Features          meFeatures     `json:"features"`
	Limits            meLimits       `json:"limits"`
	Vault             any            `json:"vault"` // null until a Vault identity is attached (Step 2.7)
	KeyID             string         `json:"keyId"`
}

// me describes the session: who is signed in, the CSRF token, the user's settings and what the server offers.
func (d Deps) me(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok || d.Users == nil {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	u, err := d.Users.Get(r.Context(), s.UserID)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	settings, err := d.Users.GetSettings(r.Context(), u.ID)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	c := d.Config
	modules := make(map[string]string, len(c.Modules))
	for name, m := range c.Modules {
		modules[name] = m.Enabled
	}
	writeJSON(w, http.StatusOK, meResponse{
		User:              meUser{ID: u.ID, Provider: u.Provider, Email: u.Email, Name: u.Name},
		CSRFToken:         s.CSRFToken,
		Settings:          settings,
		MaintenanceBanner: c.Maintenance.Banner,
		Features: meFeatures{
			Kubeconfig: c.Kubeconfig.IsEnabled(), FileBrowser: c.FileBrowser.IsEnabled(), NodeShell: c.NodeShell.IsEnabled(),
			Vault: c.Vault.Address != "", HelmVersion: c.Helm.Version, Modules: modules,
		},
		Limits: meLimits{
			FileTransferBytes: c.FileBrowser.MaxTransferBytes, LogsHistoryMaxLines: c.Logs.HistoryMaxLines,
			LogsLiveBufferLines: c.Logs.LiveBufferLines,
		},
		KeyID: d.KeyID,
	})
}
