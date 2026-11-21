package httpapi_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/bdnix/kubelook/internal/telemetry"
)

func withTracing(t *testing.T, h *harness) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingDeps{Exporter: exp})
	require.NoError(t, err)
	h.deps.Tracing = tr
	h.deps.Log = telemetry.NewLogger(h.logs, slog.LevelInfo)
	return exp
}

func TestTracing_Healthz_OneServerSpanNamedByPatternAndTheAccessLogCarriesItsTraceID(t *testing.T) {
	h := newHarness("https://k.example.com")
	exp := withTracing(t, h)

	rec := h.do(http.MethodGet, "/healthz?token=secret")
	require.Equal(t, http.StatusOK, rec.Code)

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /healthz", spans[0].Name)
	for _, kv := range spans[0].Attributes {
		assert.NotContains(t, kv.Value.String(), "secret")
	}

	var found bool
	dec := json.NewDecoder(h.logs)
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		if m["msg"] == "request" {
			found = true
			assert.Equal(t, spans[0].SpanContext.TraceID().String(), m["trace_id"])
			assert.Equal(t, spans[0].SpanContext.SpanID().String(), m["span_id"])
			assert.Equal(t, "GET /healthz", m["route"])
		}
	}
	assert.True(t, found, "an access log record was written")
}

func TestTracing_RejectedUnauthenticatedRequest_StillTraced(t *testing.T) {
	h, _, _ := withSessions(t)
	exp := withTracing(t, h)
	rec := h.doWithSession(http.MethodGet, "/api/v1/x", "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "unmatched", spans[0].Name)
	assert.Equal(t, "401", spans[0].Attributes[len(spans[0].Attributes)-1].Value.String())
}

func TestTracing_NotWired_NothingChanges(t *testing.T) {
	h := newHarness("https://k.example.com")
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/healthz").Code)
}
