package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const serviceName = "kubelook"

// TracingDeps configures NewTracing.
type TracingDeps struct {
	// Endpoint is the OTLP/gRPC collector (telemetry.otlpEndpoint), host:port. Empty and no Exporter → tracing is off.
	// The usual OTEL_EXPORTER_OTLP_* variables (TLS, insecure, headers) still apply.
	Endpoint string
	// Exporter replaces the OTLP exporter and is written to synchronously; tests pass an in-memory one.
	Exporter sdktrace.SpanExporter
	Version  string
}

// Tracing creates the spans of requests and of the calls the server makes for them. Span names are route
// patterns and fixed words, and attributes never hold a URL, query, header, host or body, so a trace cannot
// leak a token, a cluster address or an object name.
type Tracing struct {
	tracer   trace.Tracer
	prop     propagation.TextMapPropagator
	shutdown func(context.Context) error
}

// NewTracing builds the tracer. Without an endpoint or exporter it is a no-op: nothing is recorded and no
// goroutine is started.
func NewTracing(ctx context.Context, d TracingDeps) (*Tracing, error) {
	t := &Tracing{prop: propagation.TraceContext{}}
	if d.Exporter == nil && d.Endpoint == "" {
		t.tracer = noop.NewTracerProvider().Tracer(serviceName)
		t.shutdown = func(context.Context) error { return nil }
		return t, nil
	}
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource.NewWithAttributes("",
		attribute.String("service.name", serviceName), attribute.String("service.version", d.Version)))}
	if d.Exporter != nil {
		opts = append(opts, sdktrace.WithSyncer(d.Exporter))
	} else {
		exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(d.Endpoint))
		if err != nil {
			return nil, fmt.Errorf("creating the OTLP trace exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	t.tracer = tp.Tracer(serviceName)
	t.shutdown = tp.Shutdown
	return t, nil
}

// Shutdown flushes pending spans and stops the exporter.
func (t *Tracing) Shutdown(ctx context.Context) error { return t.shutdown(ctx) }

// StartRequest starts the server span of r, continuing a trace the caller sent. The returned request carries
// the span in its context; the function ends the span once the router has named the route and the status is known.
func (t *Tracing) StartRequest(r *http.Request) (*http.Request, func(route string, status int)) {
	ctx := t.prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	method := knownMethod(r.Method)
	ctx, span := t.tracer.Start(ctx, "HTTP "+method, trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("http.request.method", method)))
	return r.WithContext(ctx), func(route string, status int) {
		name := "unmatched"
		if route != "" {
			name = route
			span.SetAttributes(attribute.String("http.route", routePath(route)))
		}
		span.SetName(name)
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "")
		}
		span.End()
	}
}

// routePath drops the method from a mux pattern: "GET /x" → "/x".
func routePath(route string) string {
	if _, p, ok := strings.Cut(route, " "); ok {
		return p
	}
	return route
}

// knownMethod keeps an attacker-chosen method string out of span names and attributes.
func knownMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return m
	}
	return "_OTHER"
}

// Transport wraps base so every request is a client span, a child of the span in the request's context, named
// "<peer> <METHOD>" (peer is "vault" or "kube"). The trace context is added to the outgoing headers.
func (t *Tracing) Transport(peer string, base http.RoundTripper) http.RoundTripper {
	return &tracedTransport{t: t, peer: peer, base: base}
}

type tracedTransport struct {
	t    *Tracing
	peer string
	base http.RoundTripper
}

func (tt *tracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	method := knownMethod(req.Method)
	ctx, span := tt.t.tracer.Start(req.Context(), tt.peer+" "+method, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("peer.service", tt.peer), attribute.String("http.request.method", method)))
	defer span.End()
	out := req.Clone(ctx) // a RoundTripper must not change the request it is given
	tt.t.prop.Inject(ctx, propagation.HeaderCarrier(out.Header))
	resp, err := tt.base.RoundTrip(out)
	switch {
	case err != nil:
		span.SetStatus(codes.Error, "") // the error text can carry addresses: keep it out of the trace
	default:
		span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
		if resp.StatusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "")
		}
	}
	return resp, err
}

// WrappedRoundTripper lets client-go reach the transport underneath (connection upgrades).
func (tt *tracedTransport) WrappedRoundTripper() http.RoundTripper { return tt.base }
