package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/vault"
)

// status answers GET /api/v1/status: what the server knows about the services it depends on.
func (d Deps) status(w http.ResponseWriter, _ *http.Request) {
	vs := d.vaultStatus()
	writeJSON(w, http.StatusOK, map[string]any{"vault": vs})
}

func (d Deps) vaultStatus() vault.Status {
	if d.VaultMonitor == nil {
		return vault.Status{Reachable: true}
	}
	return d.VaultMonitor.Status()
}
