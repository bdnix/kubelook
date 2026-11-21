package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// ParseLevel reads KUBELOOK_LOG_LEVEL: debug, info, warn or error in any letter case; empty is info.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("KUBELOOK_LOG_LEVEL must be debug, info, warn or error, got %q", s)
}

// NewLogger writes JSON records to w. A record logged with the context of a traced request (InfoContext,
// …) carries trace_id and span_id at the top level.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(&traceHandler{root: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})})
}

// traceHandler adds the trace and span IDs of the record's context. The IDs go in before the groups and
// attributes of With/WithGroup, so they stay at the top level of the record. The calls that built the
// handler are kept and replayed only when a record has IDs to add.
type traceHandler struct {
	root slog.Handler
	ops  []func(slog.Handler) slog.Handler
	cur  slog.Handler // root with ops applied
}

func (h *traceHandler) current() slog.Handler {
	if h.cur != nil {
		return h.cur
	}
	return h.root
}

func (h *traceHandler) with(op func(slog.Handler) slog.Handler) *traceHandler {
	return &traceHandler{root: h.root, ops: append(append([]func(slog.Handler) slog.Handler(nil), h.ops...), op), cur: op(h.current())}
}

func (h *traceHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.current().Enabled(ctx, l)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(func(x slog.Handler) slog.Handler { return x.WithAttrs(attrs) })
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return h.with(func(x slog.Handler) slog.Handler { return x.WithGroup(name) })
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return h.current().Handle(ctx, r)
	}
	x := h.root.WithAttrs([]slog.Attr{slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String())})
	for _, op := range h.ops {
		x = op(x)
	}
	return x.Handle(ctx, r)
}
