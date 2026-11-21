package telemetry_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdnix/kubelook/internal/telemetry"
)

func newTracing(t *testing.T) (*telemetry.Tracing, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingDeps{Exporter: exp, Version: "1.2.3"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Shutdown(context.Background()) })
	return tr, exp
}

// attrs flattens a span's attributes to strings.
func attrs(s tracetest.SpanStub) map[string]string {
	out := map[string]string{}
	for _, kv := range s.Attributes {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func spanByKind(t *testing.T, spans tracetest.SpanStubs, kind trace.SpanKind) tracetest.SpanStub {
	t.Helper()
	for _, s := range spans {
		if s.SpanKind == kind {
			return s
		}
	}
	t.Fatalf("no span of kind %v in %v", kind, spans)
	return tracetest.SpanStub{}
}

type stubRT struct {
	status int
	err    error
	seen   http.Header
}

func (s *stubRT) RoundTrip(r *http.Request) (*http.Response, error) {
	s.seen = r.Header.Clone()
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{StatusCode: s.status, Body: http.NoBody}, nil
}

func TestTracing_Request_OneServerSpanNamedByTheRoutePattern(t *testing.T) {
	tr, exp := newTracing(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz?token=secret-token", nil)
	req.Header.Set("Authorization", "Bearer secret-bearer")
	_, end := tr.StartRequest(req)
	end("GET /healthz", http.StatusOK)

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "GET /healthz", s.Name)
	assert.Equal(t, trace.SpanKindServer, s.SpanKind)
	a := attrs(s)
	assert.Equal(t, "GET", a["http.request.method"])
	assert.Equal(t, "/healthz", a["http.route"])
	assert.Equal(t, "200", a["http.response.status_code"])
	for k, v := range a {
		assert.NotContains(t, v, "secret", k)
		assert.NotContains(t, k, "url", k)
	}
}

func TestTracing_Request_UnmatchedRouteAndServerErrorStatus(t *testing.T) {
	tr, exp := newTracing(t)
	_, end := tr.StartRequest(httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	end("", http.StatusInternalServerError)
	_, end = tr.StartRequest(httptest.NewRequest(http.MethodGet, "/api/v1/y", nil))
	end("", http.StatusNotFound)

	spans := exp.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, "unmatched", spans[0].Name)
	assert.Equal(t, codes.Error, spans[0].Status.Code, "5xx marks the server span as failed")
	assert.Equal(t, codes.Unset, spans[1].Status.Code, "4xx is the client's error, not the server's")
}

func TestTracing_Request_ContinuesAnIncomingTrace(t *testing.T) {
	tr, exp := newTracing(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	_, end := tr.StartRequest(req)
	end("GET /healthz", 200)
	s := exp.GetSpans()[0]
	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", s.SpanContext.TraceID().String())
	assert.Equal(t, "b7ad6b7169203331", s.Parent.SpanID().String())
	assert.True(t, s.Parent.IsRemote())
}

func TestTracing_Transport_ChildClientSpanOfTheRequestSpan(t *testing.T) {
	tr, exp := newTracing(t)
	req, end := tr.StartRequest(httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil))

	stub := &stubRT{status: 200}
	out, err := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://vault.internal.example:8200/v1/auth/token/lookup-self?wrap=secret-ttl", nil)
	require.NoError(t, err)
	out.Header.Set("X-Vault-Token", "hvs.secret-token")
	resp, err := tr.Transport("vault", stub).RoundTrip(out) //nolint:bodyclose // the stub response has no body
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	end("GET /api/v1/clusters", 200)

	spans := exp.GetSpans()
	require.Len(t, spans, 2)
	server, client := spanByKind(t, spans, trace.SpanKindServer), spanByKind(t, spans, trace.SpanKindClient)
	assert.Equal(t, server.SpanContext.TraceID(), client.SpanContext.TraceID())
	assert.Equal(t, server.SpanContext.SpanID(), client.Parent.SpanID())
	assert.Equal(t, "vault POST", client.Name)
	a := attrs(client)
	assert.Equal(t, "vault", a["peer.service"])
	assert.Equal(t, "POST", a["http.request.method"])
	assert.Equal(t, "200", a["http.response.status_code"])
	for k, v := range a {
		assert.NotContains(t, v, "secret", k)
		assert.NotContains(t, v, "internal.example", k, "no host, URL or query")
	}
	assert.NotEmpty(t, stub.seen.Get("Traceparent"), "the trace continues on the outgoing request")
	assert.Contains(t, stub.seen.Get("Traceparent"), client.SpanContext.TraceID().String())
}

func TestTracing_Transport_FailuresMarkTheSpan(t *testing.T) {
	tr, exp := newTracing(t)
	cases := []struct {
		name   string
		status int
		err    error
		code   codes.Code
	}{
		{"ok", 200, nil, codes.Unset},
		{"client error", 404, nil, codes.Unset},
		{"server error", 502, nil, codes.Error},
		{"transport error", 0, errors.New("connection refused"), codes.Error},
	}
	for _, c := range cases {
		exp.Reset()
		req := httptest.NewRequest(http.MethodGet, "https://k.example/api", nil)
		resp, err := tr.Transport("kube", &stubRT{status: c.status, err: c.err}).RoundTrip(req) //nolint:bodyclose // the stub response has no body
		if c.err != nil {
			require.ErrorIs(t, err, c.err)
		} else {
			require.NoError(t, err)
			_ = resp
		}
		spans := exp.GetSpans()
		require.Len(t, spans, 1, c.name)
		assert.Equal(t, c.code, spans[0].Status.Code, c.name)
		assert.NotContains(t, fmt.Sprint(spans[0].Status.Description), "connection refused", "error text may carry addresses")
	}
}

func TestTracing_Transport_ExposesWrappedTransport(t *testing.T) {
	tr, _ := newTracing(t)
	base := &stubRT{}
	w, ok := tr.Transport("kube", base).(interface{ WrappedRoundTripper() http.RoundTripper })
	require.True(t, ok)
	assert.Same(t, base, w.WrappedRoundTripper())
}

func TestTracing_NoEndpoint_NoopProviderAndNothingRecorded(t *testing.T) {
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingDeps{})
	require.NoError(t, err)
	req, end := tr.StartRequest(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.False(t, trace.SpanFromContext(req.Context()).IsRecording())
	end("GET /healthz", 200)
	stub := &stubRT{status: 200}
	_, err = tr.Transport("vault", stub).RoundTrip(req.Clone(req.Context())) //nolint:bodyclose // the stub response has no body
	require.NoError(t, err)
	assert.NoError(t, tr.Shutdown(t.Context()))
	// goleak (TestMain) proves that no exporter goroutine was started.
}

func TestTracing_Endpoint_BuildsAnExporterWithoutConnecting(t *testing.T) {
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingDeps{Endpoint: "127.0.0.1:1"})
	require.NoError(t, err)
	assert.NoError(t, tr.Shutdown(t.Context()))
}

type countingExporter struct {
	sdktrace.SpanExporter
	shutdowns atomic.Int32
}

func (c *countingExporter) Shutdown(ctx context.Context) error {
	c.shutdowns.Add(1)
	return c.SpanExporter.Shutdown(ctx)
}

func TestTracing_Shutdown_FlushesAndStopsTheExporterOnce(t *testing.T) {
	exp := &countingExporter{SpanExporter: tracetest.NewInMemoryExporter()}
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingDeps{Exporter: exp})
	require.NoError(t, err)
	require.NoError(t, tr.Shutdown(t.Context()))
	assert.EqualValues(t, 1, exp.shutdowns.Load())
}

func TestTracing_ResourceCarriesTheServiceNameAndVersion(t *testing.T) {
	tr, exp := newTracing(t)
	_, end := tr.StartRequest(httptest.NewRequest(http.MethodGet, "/", nil))
	end("/", 200)
	got := map[attribute.Key]string{}
	for _, kv := range exp.GetSpans()[0].Resource.Attributes() {
		got[kv.Key] = kv.Value.Emit()
	}
	assert.Equal(t, "kubelook", got["service.name"])
	assert.Equal(t, "1.2.3", got["service.version"])
}

func httpGet(target string) *http.Request { return httptest.NewRequest(http.MethodGet, target, nil) }
