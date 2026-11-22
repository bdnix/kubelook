package telemetry_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/telemetry"
)

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
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

func TestLogger_RecordsInsideASpanCarryTraceAndSpanIDs(t *testing.T) {
	tr, _ := newTracing(t)
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo)

	ctx, span := tr.Tracer().Start(t.Context(), "request")
	log.InfoContext(ctx, "inside", "k", "v")
	span.End()
	log.InfoContext(t.Context(), "outside")

	recs := logRecords(t, &buf)
	require.Len(t, recs, 2)
	assert.Equal(t, span.SpanContext().TraceID().String(), recs[0]["trace_id"])
	assert.Equal(t, span.SpanContext().SpanID().String(), recs[0]["span_id"])
	assert.Equal(t, "v", recs[0]["k"])
	assert.NotContains(t, recs[1], "trace_id")
	assert.NotContains(t, recs[1], "span_id")
}

func TestLogger_WithAttrs_KeepsTheTraceIDs(t *testing.T) {
	tr, _ := newTracing(t)
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo).With("component", "x")

	ctx, span := tr.Tracer().Start(t.Context(), "request")
	log.InfoContext(ctx, "inside", "k", "v")
	span.End()

	recs := logRecords(t, &buf)
	require.Len(t, recs, 1)
	assert.Equal(t, "x", recs[0]["component"])
	assert.Equal(t, "v", recs[0]["k"])
	assert.Equal(t, span.SpanContext().TraceID().String(), recs[0]["trace_id"])
}

func TestLogger_LevelFilters(t *testing.T) {
	for _, tc := range []struct {
		level slog.Level
		want  int
	}{{slog.LevelDebug, 4}, {slog.LevelInfo, 3}, {slog.LevelWarn, 2}, {slog.LevelError, 1}} {
		var buf bytes.Buffer
		log := telemetry.NewLogger(&buf, tc.level)
		log.Debug("d")
		log.Info("i")
		log.Warn("w")
		log.Error("e")
		assert.Len(t, logRecords(t, &buf), tc.want, tc.level.String())
	}
}
