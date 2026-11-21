package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdnix/kubelook/internal/telemetry"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"": slog.LevelInfo, "debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError,
		"DEBUG": slog.LevelDebug, "Warn": slog.LevelWarn,
	}
	for in, want := range cases {
		got, err := telemetry.ParseLevel(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"verbose", "trace", "7", "info "} {
		_, err := telemetry.ParseLevel(bad)
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "KUBELOOK_LOG_LEVEL must be debug, info, warn or error")
	}
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(buf)
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		out = append(out, m)
	}
	return out
}

func TestLogger_RecordsInsideATracedRequestCarryTraceAndSpanIDs(t *testing.T) {
	tr, exp := newTracing(t)
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo)

	req, end := tr.StartRequest(httpGet("/healthz"))
	log.InfoContext(req.Context(), "inside", "k", "v")
	log.With("static", 1).InfoContext(req.Context(), "with attrs")
	log.WithGroup("g").InfoContext(req.Context(), "grouped", "a", 1)
	log.Info("no context")
	end("GET /healthz", 200)

	span := exp.GetSpans()[0]
	recs := records(t, &buf)
	require.Len(t, recs, 4)
	for _, r := range recs[:3] {
		assert.Equal(t, span.SpanContext.TraceID().String(), r["trace_id"])
		assert.Equal(t, span.SpanContext.SpanID().String(), r["span_id"])
	}
	assert.Equal(t, "v", recs[0]["k"])
	assert.EqualValues(t, 1, recs[1]["static"])
	assert.Equal(t, map[string]any{"a": float64(1)}, recs[2]["g"])
	assert.NotContains(t, recs[3], "trace_id")
	assert.NotContains(t, recs[3], "span_id")
}

func TestLogger_NoSpan_NoTraceFields(t *testing.T) {
	var buf bytes.Buffer
	telemetry.NewLogger(&buf, slog.LevelInfo).InfoContext(context.Background(), "plain")
	telemetry.NewLogger(&buf, slog.LevelInfo).InfoContext(trace.ContextWithSpanContext(context.Background(), trace.SpanContext{}), "invalid span")
	for _, r := range records(t, &buf) {
		assert.NotContains(t, r, "trace_id")
	}
}

func TestLogger_LevelFiltersRecords(t *testing.T) {
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelWarn)
	log.Info("dropped")
	log.Warn("kept")
	recs := records(t, &buf)
	require.Len(t, recs, 1)
	assert.Equal(t, "kept", recs[0]["msg"])
	assert.True(t, log.Handler().Enabled(context.Background(), slog.LevelError))
	assert.False(t, log.Handler().Enabled(context.Background(), slog.LevelInfo))
}
