package httpapi

import (
	"fmt"
	"net/http"

	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/session"
)

type addVaultRequest struct {
	Token string `json:"token"`
}

type restoreRequest struct {
	Items []struct {
		Sealed string `json:"sealed"`
	} `json:"items"`
}

// registerCredentials mounts the credential endpoints. Without Credentials (no vault.address)
// nothing is mounted and the paths answer with the standard API 404.
func (d Deps) registerCredentials(mux *http.ServeMux) {
	if d.Credentials == nil {
		return
	}
	mux.HandleFunc("POST /api/v1/credentials/vault", d.addVaultToken)
	mux.HandleFunc("POST /api/v1/credentials/restore", d.restoreCredentials)
	mux.HandleFunc("POST /api/v1/credentials/vault/renew", d.renewVaultToken)
	mux.HandleFunc("DELETE /api/v1/credentials/vault", d.removeVaultToken)
}

func (d Deps) addVaultToken(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req addVaultRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if req.Token == "" {
		writeError(w, r, d.Log, fmt.Errorf("token is required: %w", ErrInvalid))
		return
	}
	sealed, st, err := d.Credentials.AddVaultToken(r.Context(), s, req.Token)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sealed": sealed, "vault": st})
}

func (d Deps) restoreCredentials(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req restoreRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	sealed := make([]string, len(req.Items))
	for i, it := range req.Items {
		sealed[i] = it.Sealed
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": d.Credentials.Restore(r.Context(), s, sealed)})
}

func (d Deps) renewVaultToken(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	st, err := d.Credentials.RenewVaultToken(r.Context(), s)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]cluster.VaultStatus{"vault": st})
}

func (d Deps) removeVaultToken(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	if err := d.Credentials.RemoveVaultToken(r.Context(), s); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
