package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdnix/kubelook/internal/telemetry"
)

type spanRecorder struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (r *spanRecorder) ExportSpans(_ context.Context, s []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, s...)
	return nil
}
func (r *spanRecorder) Shutdown(context.Context) error { return nil }

func (r *spanRecorder) got() tracetest.SpanStubs {
	r.mu.Lock()
	defer r.mu.Unlock()
	return tracetest.SpanStubsFromReadOnlySpans(r.spans)
}

func (h *harness) traced(t *testing.T) (*telemetry.Tracing, *spanRecorder) {
	t.Helper()
	rec := &spanRecorder{}
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingConfig{Exporter: rec})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Shutdown(context.WithoutCancel(t.Context())) })
	h.deps.Tracer = tr.Tracer()
	return tr, rec
}

func TestTracing_OneServerSpanPerRequest_NamedByTheRoutePattern(t *testing.T) {
	h := newHarness("https://k.example.com")
	tr, rec := h.traced(t)

	h.do(http.MethodGet, "/healthz?token=hvs.QUERY", "Authorization", "Bearer hvs.HEADER")
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := rec.got()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "GET /healthz", s.Name)
	assert.Equal(t, trace.SpanKindServer, s.SpanKind)
	attrs := map[string]string{}
	for _, a := range s.Attributes {
		attrs[string(a.Key)] = a.Value.String()
	}
	assert.Equal(t, "GET /healthz", attrs["http.route"])
	assert.Equal(t, "GET", attrs["http.request.method"])
	assert.Equal(t, "200", attrs["http.response.status_code"])
	for k, v := range attrs {
		assert.NotContains(t, v, "hvs.", k)
		assert.NotContains(t, v, "token", k)
	}
}

func TestTracing_SpanNameIsThePatternNotThePath(t *testing.T) {
	h := newHarness("https://k.example.com")
	tr, rec := h.traced(t)

	h.do(http.MethodGet, "/api/v1/clusters/prod-eu/pods/secret-pod")
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := rec.got()
	require.Len(t, spans, 1)
	assert.Equal(t, "/api/", spans[0].Name)
	assert.False(t, strings.Contains(spans[0].Name, "prod-eu"))
}

func TestTracing_ServerErrorsMarkTheSpan(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.Ready = func(context.Context) error { return assert.AnError }
	tr, rec := h.traced(t)

	h.do(http.MethodGet, "/readyz")
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := rec.got()
	require.Len(t, spans, 1)
	assert.Equal(t, "Error", spans[0].Status.Code.String())
}

func TestTracing_LogRecordsOfARequestCarryTheTraceID(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.Log = telemetry.NewLogger(h.logs, -4)
	tr, rec := h.traced(t)

	h.do(http.MethodGet, "/healthz")
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := rec.got()
	require.Len(t, spans, 1)
	assert.Contains(t, h.logs.String(), `"trace_id":"`+spans[0].SpanContext.TraceID().String()+`"`)
}
