package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/goleak"

	"github.com/bdnix/kubelook/internal/telemetry"
)

// recorder keeps what was exported, and keeps it after Shutdown (the SDK's in-memory exporter forgets it).
type recorder struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
	down  bool
}

func (r *recorder) ExportSpans(_ context.Context, s []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, s...)
	return nil
}

func (r *recorder) Shutdown(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = true
	return nil
}

func (r *recorder) got() tracetest.SpanStubs {
	r.mu.Lock()
	defer r.mu.Unlock()
	return tracetest.SpanStubsFromReadOnlySpans(r.spans)
}

func newTracing(t *testing.T) (*telemetry.Tracing, *recorder) {
	t.Helper()
	rec := &recorder{}
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingConfig{Exporter: rec})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Shutdown(context.WithoutCancel(t.Context())) })
	return tr, rec
}

func attrValues(s tracetest.SpanStub) []string {
	var out []string
	for _, a := range s.Attributes {
		out = append(out, string(a.Key)+"="+a.Value.Emit())
	}
	return out
}

func TestTracing_NoEndpointAndNoExporter_IsANoOpWithoutGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t)
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingConfig{})
	require.NoError(t, err)

	_, span := tr.Tracer().Start(t.Context(), "x")
	assert.False(t, span.IsRecording())
	span.End()
	require.NoError(t, tr.ForceFlush(t.Context()))
	require.NoError(t, tr.Shutdown(t.Context()))
}

func TestTracing_Shutdown_FlushesPendingSpans(t *testing.T) {
	rec := &recorder{}
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingConfig{Exporter: rec})
	require.NoError(t, err)

	_, span := tr.Tracer().Start(t.Context(), "pending")
	span.End()
	require.NoError(t, tr.Shutdown(t.Context()))

	require.Len(t, rec.got(), 1)
	assert.Equal(t, "pending", rec.got()[0].Name)
	assert.True(t, rec.down, "the exporter is shut down too")
}

func TestTracing_Endpoint_BuildsAnOTLPExporterWithoutConnecting(t *testing.T) {
	defer goleak.VerifyNone(t)
	tr, err := telemetry.NewTracing(t.Context(), telemetry.TracingConfig{Endpoint: "127.0.0.1:1"})
	require.NoError(t, err)
	_, span := tr.Tracer().Start(t.Context(), "x")
	assert.True(t, span.IsRecording())
	span.End()
	// Nothing listens there: the flush fails, and Shutdown still returns and stops every goroutine.
	_ = tr.Shutdown(t.Context())
}

func TestTracing_ClientSpan_IsAChildOfTheCurrentSpan_AndCarriesNoSecrets(t *testing.T) {
	tr, rec := newTracing(t)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Vault-Token")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, parent := tr.Tracer().Start(t.Context(), "parent")
	client := &http.Client{Transport: tr.Transport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/auth/token/lookup-self?secret-query=hvs.QUERY", strings.NewReader("body-with-hvs.BODY"))
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", "hvs.HEADER")
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	parent.End()
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := rec.got()
	require.Len(t, spans, 2)
	var child tracetest.SpanStub
	for _, s := range spans {
		if s.Name != "parent" {
			child = s
		}
	}
	assert.Equal(t, trace.SpanKindClient, child.SpanKind)
	assert.Equal(t, parent.SpanContext().SpanID(), child.Parent.SpanID())
	assert.Equal(t, "hvs.HEADER", gotAuth, "the request itself is untouched")
	for _, v := range attrValues(child) {
		for _, secret := range []string{"hvs.HEADER", "hvs.BODY", "hvs.QUERY", "secret-query"} {
			assert.NotContains(t, v, secret)
		}
	}
	assert.NotContains(t, child.Name, "hvs.")
}

func TestTracing_ClientSpan_AFailedRequestIsMarkedAsAnError(t *testing.T) {
	tr, rec := newTracing(t)
	client := &http.Client{Transport: tr.Transport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:1/", nil)
	require.NoError(t, err)
	_, err = client.Do(req) //nolint:bodyclose // the request fails
	require.Error(t, err)
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := rec.got()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestTracing_Attribute_KeysUsedByTheServerSpan(t *testing.T) {
	// The attributes the HTTP layer sets are fixed names; this guards against a rename that would break dashboards.
	assert.Equal(t, attribute.Key("http.route"), telemetry.AttrRoute)
	assert.Equal(t, attribute.Key("http.request.method"), telemetry.AttrMethod)
	assert.Equal(t, attribute.Key("http.response.status_code"), telemetry.AttrStatus)
}
