package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/bdnix/kubelook/internal/session"
)

const defaultHistoryMinutes = 60

func (d Deps) registerMetrics(mux *http.ServeMux) {
	if d.Metrics == nil {
		return
	}
	const base = "GET /api/v1/clusters/{cluster}/metrics/"
	mux.HandleFunc(base+"pods", func(w http.ResponseWriter, r *http.Request) {
		s, ok := d.sessionOrFail(w, r)
		if !ok {
			return
		}
		u, err := d.Metrics.Pods(r.Context(), s, r.PathValue("cluster"), r.URL.Query().Get("namespace"))
		d.writeResult(w, r, u, err)
	})
	mux.HandleFunc(base+"nodes", func(w http.ResponseWriter, r *http.Request) {
		s, ok := d.sessionOrFail(w, r)
		if !ok {
			return
		}
		u, err := d.Metrics.Nodes(r.Context(), s, r.PathValue("cluster"))
		d.writeResult(w, r, u, err)
	})
	mux.HandleFunc(base+"history", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := d.sessionOrFail(w, r); !ok {
			return
		}
		q := r.URL.Query()
		minutes := defaultHistoryMinutes
		if v := q.Get("minutes"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				writeError(w, r, d.Log, fmt.Errorf("minutes must be a number: %w", ErrInvalid))
				return
			}
			minutes = n
		}
		h, err := d.Metrics.History(r.Context(), r.PathValue("cluster"), q.Get("kind"), q.Get("namespace"), q.Get("name"), minutes)
		d.writeResult(w, r, h, err)
	})
}

func (d Deps) sessionOrFail(w http.ResponseWriter, r *http.Request) (session.Session, bool) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
	}
	return s, ok
}

func (d Deps) writeResult(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
