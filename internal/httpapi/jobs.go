package httpapi

import (
	"fmt"
	"net/http"

	"github.com/bdnix/kubelook/internal/session"
)

// registerJobs mounts the job endpoints. Without a runner nothing is mounted.
func (d Deps) registerJobs(mux *http.ServeMux) {
	if d.Jobs == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/jobs", d.listJobs)
	mux.HandleFunc("GET /api/v1/jobs/{id}", d.getJob)
	mux.HandleFunc("POST /api/v1/jobs/{id}/retry", d.retryJob)
}

func (d Deps) listJobs(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var active *bool
	switch v := r.URL.Query().Get("active"); v {
	case "":
	case "true", "false":
		b := v == "true"
		active = &b
	default:
		writeError(w, r, d.Log, fmt.Errorf("active must be true or false: %w", ErrInvalid))
		return
	}
	list, err := d.Jobs.List(r.Context(), s.UserID, active)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": list})
}

func (d Deps) getJob(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	j, err := d.Jobs.Get(r.Context(), s.UserID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (d Deps) retryJob(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	id, err := d.Jobs.Retry(r.Context(), s, r.PathValue("id"))
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"jobId": id})
}
