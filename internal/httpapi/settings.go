package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

func (d Deps) registerSettings(mux *http.ServeMux) {
	if d.Users == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/settings", d.getSettings)
	mux.HandleFunc("PUT /api/v1/settings", d.putSettings)
}

// normalized gives every collection a value, so the browser never sees null.
func normalized(s store.Settings) store.Settings {
	if s.FavouriteClusters == nil {
		s.FavouriteClusters = []string{}
	}
	if s.NamespaceFilters == nil {
		s.NamespaceFilters = map[string][]string{}
	}
	if s.ColumnPresets == nil {
		s.ColumnPresets = map[string][]string{}
	}
	if s.FilterSets == nil {
		s.FilterSets = []store.FilterSet{}
	}
	if s.Theme == "" {
		s.Theme = "system"
	}
	return s
}

func (d Deps) getSettings(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	settings, err := d.Users.GetSettings(r.Context(), s.UserID)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, normalized(settings))
}

// putSettings replaces the user's whole settings document. Settings belong to the user, not to the session.
func (d Deps) putSettings(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var in store.Settings
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if err := d.Users.PutSettings(r.Context(), s.UserID, in); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, normalized(in))
}
