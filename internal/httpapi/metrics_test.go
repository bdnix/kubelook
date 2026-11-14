package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/metrics"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func withMetrics(t *testing.T, e *kubeEnv, prom config.Prometheus) {
	t.Helper()
	list := func(name, kind string, ns bool) *metav1.APIResourceList {
		return &metav1.APIResourceList{GroupVersion: "metrics.k8s.io/v1beta1", APIResources: []metav1.APIResource{{Name: name, Namespaced: ns, Kind: kind, Verbs: metav1.Verbs{"get", "list"}}}}
	}
	e.fakes.AddResources(list("pods", "PodMetrics", true), list("nodes", "NodeMetrics", false))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.Object("metrics.k8s.io/v1beta1", "PodMetrics", "ns1", "web", map[string]any{
		"containers": []any{map[string]any{"name": "app", "usage": map[string]any{"cpu": "250m", "memory": "128Mi"}}},
	})))
	require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.Object("metrics.k8s.io/v1beta1", "NodeMetrics", "", "n1", map[string]any{
		"usage": map[string]any{"cpu": "1", "memory": "1Gi"},
	})))
	e.deps.Metrics = metrics.New(metrics.Deps{Clients: e.fakes, Clock: e.fake, Prometheus: prom})
}

func TestMetricsAPI_PodsAndNodes(t *testing.T) {
	e := newKubeEnv(t)
	withMetrics(t, e, config.Prometheus{})
	rec := e.get("/api/v1/clusters/v:prod/metrics/pods?namespace=ns1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"available":true,"items":[{"namespace":"ns1","name":"web","cpuMillicores":250,"memoryBytes":134217728,
		"containers":[{"name":"app","cpuMillicores":250,"memoryBytes":134217728}]}]}`, rec.Body.String())
	rec = e.get("/api/v1/clusters/v:prod/metrics/nodes")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"available":true,"items":[{"name":"n1","cpuMillicores":1000,"memoryBytes":1073741824}]}`, rec.Body.String())
}

func TestMetricsAPI_NotServed_IsUnavailableNotAnError(t *testing.T) {
	e := newKubeEnv(t)
	e.deps.Metrics = metrics.New(metrics.Deps{Clients: e.fakes, Clock: e.fake})
	rec := e.get("/api/v1/clusters/v:prod/metrics/pods")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"available":false,"items":[]}`, rec.Body.String())
}

func TestMetricsAPI_History(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1731585000,"2"]]}]}}`))
	}))
	t.Cleanup(prom.Close)
	e := newKubeEnv(t)
	withMetrics(t, e, config.Prometheus{Clusters: map[string]config.PrometheusCluster{"v:prod": {URL: prom.URL}}})
	rec := e.get("/api/v1/clusters/v:prod/metrics/history?kind=pod&namespace=ns1&name=web&minutes=30")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"available":true,"series":[{"metric":"cpu","points":[[1731585000,2]]},{"metric":"memory","points":[[1731585000,2]]}]}`, rec.Body.String())
}

func TestMetricsAPI_HistoryWithoutPrometheus_IsUnavailable(t *testing.T) {
	e := newKubeEnv(t)
	withMetrics(t, e, config.Prometheus{})
	rec := e.get("/api/v1/clusters/v:prod/metrics/history?kind=node&name=n1")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"available":false,"series":[]}`, rec.Body.String())
}

func TestMetricsAPI_Validation(t *testing.T) {
	e := newKubeEnv(t)
	withMetrics(t, e, config.Prometheus{Clusters: map[string]config.PrometheusCluster{"v:prod": {URL: "http://127.0.0.1:1"}}})
	for name, path := range map[string]string{
		"unknown kind":   "/metrics/history?kind=deployment&name=x",
		"no name":        "/metrics/history?kind=pod",
		"zero minutes":   "/metrics/history?kind=pod&name=x&minutes=0",
		"too many":       "/metrics/history?kind=pod&name=x&minutes=1441",
		"minutes a word": "/metrics/history?kind=pod&name=x&minutes=soon",
	} {
		assert.Equal(t, http.StatusBadRequest, e.get("/api/v1/clusters/v:prod"+path).Code, name)
	}
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/clusters/v:nope/metrics/pods").Code)
}

func TestMetricsAPI_NotMountedWithoutTheService(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/clusters/v:prod/metrics/pods").Code)
}
