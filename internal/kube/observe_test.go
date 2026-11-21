package kube_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

type kubeObs struct {
	verb, outcome string
	d             time.Duration
}

// upstream answers every request after 25 ms of fake time with status (or err).
type upstream struct {
	clk    *clock.Fake
	status int
	err    error
}

func (u *upstream) RoundTrip(*http.Request) (*http.Response, error) {
	u.clk.Advance(25 * time.Millisecond)
	if u.err != nil {
		return nil, u.err
	}
	return &http.Response{StatusCode: u.status, Body: http.NoBody}, nil
}

// wrapped builds a client through the factory and returns the transport wrapper it put on the REST configuration.
func wrapped(t *testing.T, clk *clock.Fake, observe func(verb, outcome string, d time.Duration)) *rest.Config {
	t.Helper()
	var got *rest.Config
	clients := kube.NewClients(kube.Deps{
		Config: &source{}, Clock: clk, Observe: observe,
		Build: func(cfg *rest.Config) (kube.Built, error) { got = cfg; return kube.Built{}, nil },
	})
	_, err := clients.Dynamic(context.Background(), session.Session{Hash: "h"}, "c")
	require.NoError(t, err)
	return got
}

func TestObserve_RequestsAreObservedByVerbAndOutcome(t *testing.T) {
	clk := clock.NewFake(t0)
	var got []kubeObs
	cfg := wrapped(t, clk, func(v, o string, d time.Duration) { got = append(got, kubeObs{v, o, d}) })
	require.NotNil(t, cfg.WrapTransport)

	for _, tc := range []struct {
		method, url string
		status      int
		err         error
		want        kubeObs
	}{
		{http.MethodGet, "https://k/api/v1/pods", 200, nil, kubeObs{"get", "ok", 25 * time.Millisecond}},
		{http.MethodGet, "https://k/api/v1/pods?watch=true&resourceVersion=1", 200, nil, kubeObs{"watch", "ok", 25 * time.Millisecond}},
		{http.MethodGet, "https://k/api/v1/pods?watch=1", 200, nil, kubeObs{"watch", "ok", 25 * time.Millisecond}},
		{http.MethodPost, "https://k/api/v1/pods", 201, nil, kubeObs{"create", "ok", 25 * time.Millisecond}},
		{http.MethodPut, "https://k/api/v1/pods/a", 500, nil, kubeObs{"update", "server_error", 25 * time.Millisecond}},
		{http.MethodPatch, "https://k/api/v1/pods/a", 200, nil, kubeObs{"patch", "ok", 25 * time.Millisecond}},
		{http.MethodDelete, "https://k/api/v1/pods/a", 404, nil, kubeObs{"delete", "client_error", 25 * time.Millisecond}},
		{http.MethodGet, "https://k/api/v1/pods", 0, errors.New("connection refused"), kubeObs{"get", "error", 25 * time.Millisecond}},
		{http.MethodHead, "https://k/api", 200, nil, kubeObs{"other", "ok", 25 * time.Millisecond}},
	} {
		got = nil
		rt := cfg.WrapTransport(&upstream{clk: clk, status: tc.status, err: tc.err})
		req, err := http.NewRequestWithContext(context.Background(), tc.method, tc.url, nil)
		require.NoError(t, err)
		resp, err := rt.RoundTrip(req) //nolint:bodyclose // the body is http.NoBody
		assert.Equal(t, tc.err, err)
		if err == nil {
			assert.Equal(t, tc.status, resp.StatusCode, "the answer is passed on untouched")
		}
		assert.Equal(t, []kubeObs{tc.want}, got, tc.method+" "+tc.url)
	}
}

func TestObserve_NoObserver_LeavesTheTransportAlone(t *testing.T) {
	assert.Nil(t, wrapped(t, clock.NewFake(t0), nil).WrapTransport)
}

func TestObserve_KeepsTheTransportWrapperTheSourceSet(t *testing.T) {
	clk := clock.NewFake(t0)
	var seen int
	src := &wrappingSource{wrap: func(rt http.RoundTripper) http.RoundTripper { seen++; return rt }}
	var got *rest.Config
	clients := kube.NewClients(kube.Deps{
		Config: src, Clock: clk, Observe: func(string, string, time.Duration) {},
		Build: func(cfg *rest.Config) (kube.Built, error) { got = cfg; return kube.Built{}, nil },
	})
	_, err := clients.Dynamic(context.Background(), session.Session{Hash: "h"}, "c")
	require.NoError(t, err)

	got.WrapTransport(&upstream{clk: clk, status: 200})
	assert.Equal(t, 1, seen)
}

type wrappingSource struct{ wrap func(http.RoundTripper) http.RoundTripper }

func (s *wrappingSource) RESTConfig(context.Context, session.Session, string) (*rest.Config, error) {
	return &rest.Config{Host: "http://x", WrapTransport: s.wrap}, nil
}
