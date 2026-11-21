package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/telemetry"
)

func withTelemetry(h *harness) *telemetry.Metrics {
	m := telemetry.NewMetrics(prometheus.NewRegistry(), telemetry.BuildInfo{Version: "9.9.9", Commit: "deadbee"}, telemetry.Sources{})
	h.deps.Telemetry = m
	return m
}

func TestMetricsEndpoint_NoSession_ServesBuildInfo(t *testing.T) {
	h, _, _ := withSessions(t)
	withTelemetry(h)

	rec := h.doWithSession(http.MethodGet, "/metrics", "", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `kubelook_build_info{commit="deadbee",version="9.9.9"} 1`)
}

func TestMetricsEndpoint_NotWired_FallsThroughToTheSPA(t *testing.T) {
	h := newHarness("https://k.example.com")
	rec := h.do(http.MethodGet, "/metrics")
	assert.Equal(t, "<html>app</html>", rec.Body.String())
}

func TestMetricsEndpoint_RecordsRequestsByRoutePattern(t *testing.T) {
	h, id, _ := withSessions(t)
	withTelemetry(h)

	h.doWithSession(http.MethodGet, "/healthz", "", "")
	h.doWithSession(http.MethodGet, "/api/v1/nowhere/secret-name", id, "") // unknown API path
	h.doWithSession(http.MethodGet, "/some/spa/route", "", "")

	body := h.doWithSession(http.MethodGet, "/metrics", "", "").Body.String()
	assert.Contains(t, body, `kubelook_http_requests_total{method="GET",route="/healthz",status="200"} 1`)
	assert.Contains(t, body, `route="/api/"`)
	assert.Contains(t, body, `kubelook_http_request_duration_seconds_count{route="/healthz"} 1`)
	assert.NotContains(t, body, "secret-name", "the raw path never becomes a label")
	assert.NotContains(t, body, "/some/spa/route")
}

func TestMetricsEndpoint_UnauthenticatedAPIRequest_IsCountedAsUnmatched(t *testing.T) {
	h, _, _ := withSessions(t)
	withTelemetry(h)
	h.doWithSession(http.MethodGet, "/api/v1/x", "", "") // rejected before routing

	body := h.doWithSession(http.MethodGet, "/metrics", "", "").Body.String()
	assert.Contains(t, body, `kubelook_http_requests_total{method="GET",route="unmatched",status="401"} 1`)
}
