package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/telemetry"
)

type observed struct {
	route, method string
	status        int
}

type recordingTelemetry struct {
	mu   sync.Mutex
	seen []observed
}

func (r *recordingTelemetry) Handler() http.Handler { return http.NotFoundHandler() }

func (r *recordingTelemetry) ObserveHTTP(route, method string, status int, _ time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, observed{route, method, status})
}

func (r *recordingTelemetry) last(t *testing.T) observed {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.seen)
	return r.seen[len(r.seen)-1]
}

func TestMetricsEndpoint_ServedWithoutSession_ContainsBuildInfo(t *testing.T) {
	h, _, _ := withSessions(t)
	h.deps.Telemetry = telemetry.NewMetrics(prometheus.NewRegistry(), telemetry.Deps{Version: "9.9.9", Commit: "deadbee"})

	rec := h.doWithSession(http.MethodGet, "/metrics", "", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `kubelook_build_info{commit="deadbee",version="9.9.9"} 1`)
}

func TestMetricsEndpoint_NoTelemetry_IsNotMounted(t *testing.T) {
	h := newHarness("https://k.example.com")
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/metrics").Code, "falls through to the single-page app")
	assert.NotContains(t, h.do(http.MethodGet, "/metrics").Body.String(), "kubelook_")
}

func TestRequests_AreObservedWithTheRoutePatternNotThePath(t *testing.T) {
	h := newHarness("https://k.example.com")
	rec := &recordingTelemetry{}
	h.deps.Telemetry = rec

	h.do(http.MethodGet, "/healthz")
	assert.Equal(t, observed{"GET /healthz", http.MethodGet, http.StatusOK}, rec.last(t))

	h.do(http.MethodGet, "/api/v1/no/such/route/for-alice")
	got := rec.last(t)
	assert.Equal(t, "/api/", got.route, "the catch-all pattern, never the raw path")
	assert.Equal(t, http.StatusNotFound, got.status)

	h.do(http.MethodGet, "/some/spa/page/of/cluster-prod")
	assert.Equal(t, "/", rec.last(t).route)
}

func TestMetricsEndpoint_NeverShowsPathsOrNames(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.Telemetry = telemetry.NewMetrics(prometheus.NewRegistry(), telemetry.Deps{})

	h.do(http.MethodGet, "/api/v1/clusters/prod-eu/namespaces/payments/pods/secret-pod")
	body := httptest.NewRecorder()
	h.deps.Telemetry.Handler().ServeHTTP(body, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	for _, forbidden := range []string{"prod-eu", "payments", "secret-pod"} {
		assert.NotContains(t, body.Body.String(), forbidden)
	}
}
