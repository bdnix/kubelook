package httpapi

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"sort"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/certs"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

type secretRequest struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Keys      []string `json:"keys"`
}

// registerSecrets mounts the only endpoints through which Secret values leave the server. Without an audit
// recorder nothing is mounted: no record, no reveal.
func (d Deps) registerSecrets(mux *http.ServeMux, res *kube.Resources) {
	if d.Audit == nil {
		return
	}
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/secrets/reveal", d.secretHandler(res, func(w http.ResponseWriter, r *http.Request, s session.Session, req secretRequest) error {
		return d.reveal(w, r, s, req, res)
	}))
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/secrets/certificates", d.secretHandler(res, func(w http.ResponseWriter, r *http.Request, s session.Session, req secretRequest) error {
		return d.certificates(w, r, s, req, res)
	}))
}

func (d Deps) secretHandler(_ *kube.Resources, fn func(http.ResponseWriter, *http.Request, session.Session, secretRequest) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		var req secretRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		if req.Namespace == "" || req.Name == "" {
			writeError(w, r, d.Log, fmt.Errorf("namespace and name are required: %w", ErrInvalid))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if err := fn(w, r, s, req); err != nil {
			writeError(w, r, d.Log, err)
		}
	}
}

func (d Deps) secretEvent(r *http.Request, verb string, req secretRequest, details map[string]any) audit.Event {
	return audit.Event{Cluster: r.PathValue("cluster"), Namespace: req.Namespace, Verb: verb, ResourceKind: "secrets", ResourceName: req.Name, Details: details}
}

func (d Deps) reveal(w http.ResponseWriter, r *http.Request, s session.Session, req secretRequest, res *kube.Resources) error {
	var keys any = "all"
	if len(req.Keys) > 0 {
		sorted := slices.Clone(req.Keys)
		sort.Strings(sorted)
		keys = sorted
	}
	var out map[string]string
	err := d.audited(r.Context(), d.secretEvent(r, audit.VerbSecretReveal, req, map[string]any{"keys": keys}), func() error {
		data, err := res.SecretData(r.Context(), s, r.PathValue("cluster"), req.Namespace, req.Name)
		if err != nil {
			return err
		}
		out = map[string]string{}
		if len(req.Keys) == 0 {
			for k, v := range data {
				out[k] = base64.StdEncoding.EncodeToString(v)
			}
			return nil
		}
		for _, k := range req.Keys {
			v, ok := data[k]
			if !ok {
				return fmt.Errorf("the Secret has no key %q: %w", k, ErrNotFound)
			}
			out[k] = base64.StdEncoding.EncodeToString(v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

func (d Deps) certificates(w http.ResponseWriter, r *http.Request, s session.Session, req secretRequest, res *kube.Resources) error {
	var found []certs.Entry
	err := d.audited(r.Context(), d.secretEvent(r, audit.VerbCertificateView, req, nil), func() error {
		data, err := res.SecretData(r.Context(), s, r.PathValue("cluster"), req.Namespace, req.Name)
		if err != nil {
			return err
		}
		found = certs.Parse(data, d.Clock.Now())
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"certificates": found})
	return nil
}
