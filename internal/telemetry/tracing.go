package telemetry

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// The attributes of a server span. They are all there is: no path, query string, header or body.
const (
	AttrRoute  = attribute.Key("http.route")
	AttrMethod = attribute.Key("http.request.method")
	AttrStatus = attribute.Key("http.response.status_code")
)

const tracerName = "github.com/bdnix/kubelook"

// TracingConfig configures NewTracing.
type TracingConfig struct {
	// Endpoint is the OTLP/gRPC collector (host:port). Empty, with no Exporter → tracing is off.
	Endpoint string
	// Exporter replaces the OTLP exporter; tests use it.
	Exporter sdktrace.SpanExporter
}

// Tracing owns the tracer provider.
type Tracing struct {
	tp       trace.TracerProvider
	sdk      *sdktrace.TracerProvider // nil when tracing is off
	exporter sdktrace.SpanExporter
}

// NewTracing builds the provider. With no endpoint and no exporter it is a no-op: no exporter and no goroutines.
func NewTracing(ctx context.Context, c TracingConfig) (*Tracing, error) {
	exp := c.Exporter
	if exp == nil && c.Endpoint != "" {
		var err error
		if exp, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(c.Endpoint)); err != nil {
			return nil, fmt.Errorf("creating the OTLP trace exporter: %w", err)
		}
	}
	if exp == nil {
		return &Tracing{tp: noop.NewTracerProvider()}, nil
	}
	sdk := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
	return &Tracing{tp: sdk, sdk: sdk, exporter: exp}, nil
}

// Tracer returns the tracer of the server.
func (t *Tracing) Tracer() trace.Tracer { return t.tp.Tracer(tracerName) }

// Transport wraps rt so that every outbound request is a client span, a child of the span in the request's
// context. The span is named by the method and carries the method, the host and the status; neither a path,
// a query string, a header nor a body reaches it. (otelhttp's transport records the full URL, query included,
// and has no option to leave it out, so the spans are made here.)
func (t *Tracing) Transport(rt http.RoundTripper) http.RoundTripper {
	return &tracedTransport{next: rt, tracer: t.Tracer()}
}

type tracedTransport struct {
	next   http.RoundTripper
	tracer trace.Tracer
}

func (t *tracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := t.tracer.Start(req.Context(), req.Method, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(AttrMethod.String(req.Method), attribute.String("server.address", req.URL.Hostname())))
	defer span.End()
	resp, err := t.next.RoundTrip(req.WithContext(ctx))
	switch {
	case err != nil:
		span.SetStatus(codes.Error, "request failed")
	default:
		span.SetAttributes(AttrStatus.Int(resp.StatusCode))
		if resp.StatusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
		}
	}
	return resp, err
}

// ForceFlush exports the spans that are waiting.
func (t *Tracing) ForceFlush(ctx context.Context) error {
	if t.sdk == nil {
		return nil
	}
	return t.sdk.ForceFlush(ctx)
}

// Shutdown flushes the pending spans and stops the exporter.
func (t *Tracing) Shutdown(ctx context.Context) error {
	if t.sdk == nil {
		return nil
	}
	return t.sdk.Shutdown(ctx)
}
