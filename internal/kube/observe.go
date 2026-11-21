package kube

import (
	"net/http"
	"time"

	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/clock"
)

// Observer receives one observation per request to a cluster: the verb ("get", "watch", "create", "update",
// "patch", "delete" or "other"), the outcome ("ok", "client_error", "server_error" or "error") and the time until
// the cluster answered. Never a cluster, namespace or object name.
type Observer func(verb, outcome string, d time.Duration)

// observeConfig returns a copy of cfg whose transport reports every request to observe.
func observeConfig(cfg *rest.Config, c clock.Clock, observe Observer) *rest.Config {
	cfg = rest.CopyConfig(cfg)
	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &observedTransport{next: rt, clock: c, observe: observe}
	})
	return cfg
}

type observedTransport struct {
	next    http.RoundTripper
	clock   clock.Clock
	observe Observer
}

func (t *observedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := t.clock.Now()
	resp, err := t.next.RoundTrip(req)
	outcome := "ok"
	switch {
	case err != nil:
		outcome = "error"
	case resp.StatusCode >= 500:
		outcome = "server_error"
	case resp.StatusCode >= 400:
		outcome = "client_error"
	}
	t.observe(verbOf(req), outcome, t.clock.Now().Sub(start))
	return resp, err
}

func verbOf(req *http.Request) string {
	switch req.Method {
	case http.MethodGet:
		if w := req.URL.Query().Get("watch"); w == "true" || w == "1" {
			return "watch"
		}
		return "get"
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	}
	return "other"
}
