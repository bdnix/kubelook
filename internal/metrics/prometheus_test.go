package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/metrics"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type promServer struct {
	*httptest.Server
	mu      sync.Mutex
	queries []url.Values
	paths   []string
	answer  func(q string) (int, string)
}

func newProm(t *testing.T) *promServer {
	t.Helper()
	p := &promServer{answer: func(string) (int, string) {
		return 200, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1731585000,"0.25"],[1731585060,"0.5"]]}]}}`
	}}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.queries = append(p.queries, r.URL.Query())
		p.paths = append(p.paths, r.URL.Path)
		status, body := p.answer(r.URL.Query().Get("query"))
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *promServer) last() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queries[len(p.queries)-1]
}

func (p *promServer) service(t *testing.T) *metrics.Service {
	t.Helper()
	return metrics.New(metrics.Deps{
		Clients: fakekube.New(), Clock: clock.NewFake(t0), HTTP: p.Client(),
		Prometheus: config.Prometheus{Clusters: map[string]config.PrometheusCluster{"c": {URL: p.URL}}},
	})
}

func TestHistory_NotConfigured_IsUnavailable(t *testing.T) {
	svc := metrics.New(metrics.Deps{Clients: fakekube.New(), Clock: clock.NewFake(t0)})
	got, err := svc.History(t.Context(), "c", "pod", "ns", "web", 60)
	require.NoError(t, err)
	assert.False(t, got.Available)
	assert.Empty(t, got.Series)
}

func TestHistory_PodQueries_UseTheFixedTemplates(t *testing.T) {
	p := newProm(t)
	got, err := p.service(t).History(t.Context(), "c", "pod", "ns1", "web-1", 60)
	require.NoError(t, err)
	require.True(t, got.Available)
	require.Len(t, got.Series, 2)
	assert.Equal(t, "cpu", got.Series[0].Metric)
	assert.Equal(t, "memory", got.Series[1].Metric)
	assert.Equal(t, [][2]float64{{1731585000, 0.25}, {1731585060, 0.5}}, got.Series[0].Points)

	var queries []string
	for _, q := range p.queries {
		queries = append(queries, q.Get("query"))
	}
	assert.ElementsMatch(t, []string{
		`sum(rate(container_cpu_usage_seconds_total{namespace="ns1",pod="web-1",container!=""}[5m]))`,
		`sum(container_memory_working_set_bytes{namespace="ns1",pod="web-1",container!=""})`,
	}, queries)
	for _, path := range p.paths {
		assert.Equal(t, "/api/v1/query_range", path)
	}
}

func TestHistory_NodeQueries_UseTheFixedTemplates(t *testing.T) {
	p := newProm(t)
	_, err := p.service(t).History(t.Context(), "c", "node", "", "node-1", 30)
	require.NoError(t, err)
	var queries []string
	for _, q := range p.queries {
		queries = append(queries, q.Get("query"))
	}
	assert.ElementsMatch(t, []string{
		`sum(rate(node_cpu_seconds_total{mode!="idle",instance=~"node-1.*"}[5m]))`,
		`node_memory_MemTotal_bytes{instance=~"node-1.*"} - node_memory_MemAvailable_bytes{instance=~"node-1.*"}`,
	}, queries)
}

func TestHistory_LabelValuesAreEscaped(t *testing.T) {
	p := newProm(t)
	_, err := p.service(t).History(t.Context(), "c", "pod", `n"s`, `we"b\x`, 60)
	require.NoError(t, err)
	var queries []string
	for _, q := range p.queries {
		queries = append(queries, q.Get("query"))
	}
	assert.Contains(t, queries, `sum(container_memory_working_set_bytes{namespace="n\"s",pod="we\"b\\x",container!=""})`)
	for _, q := range queries {
		assert.NotContains(t, q, `we"b`, "an unescaped quote would end the label value")
	}
}

func TestHistory_TheRangeEndsNowAndCoversTheMinutes(t *testing.T) {
	p := newProm(t)
	_, err := p.service(t).History(t.Context(), "c", "pod", "ns", "web", 120)
	require.NoError(t, err)
	q := p.last()
	end := t0.Unix()
	assert.Equal(t, formatUnix(end), q.Get("end"))
	assert.Equal(t, formatUnix(end-120*60), q.Get("start"))
	assert.NotEmpty(t, q.Get("step"))
}

func formatUnix(v int64) string { return time.Unix(v, 0).UTC().Format("2006-01-02T15:04:05Z") }

func TestHistory_MinutesAreLimited(t *testing.T) {
	p := newProm(t)
	svc := p.service(t)
	for _, m := range []int{0, -5, 1441} {
		_, err := svc.History(t.Context(), "c", "pod", "ns", "web", m)
		assert.ErrorIs(t, err, apperr.ErrInvalid, m)
	}
	for _, m := range []int{1, 1440} {
		_, err := svc.History(t.Context(), "c", "pod", "ns", "web", m)
		assert.NoError(t, err, m)
	}
	_, err := svc.History(t.Context(), "c", "container", "ns", "web", 60)
	assert.ErrorIs(t, err, apperr.ErrInvalid, "only pods and nodes")
	_, err = svc.History(t.Context(), "c", "pod", "ns", "", 60)
	assert.ErrorIs(t, err, apperr.ErrInvalid, "a name is needed")
}

func TestHistory_PrometheusErrors_MakeItUnavailableWithAMessage(t *testing.T) {
	p := newProm(t)
	svc := p.service(t)
	for name, answer := range map[string]func(string) (int, string){
		"http error":   func(string) (int, string) { return 500, "boom" },
		"api error":    func(string) (int, string) { return 200, `{"status":"error","errorType":"bad_data","error":"parse error"}` },
		"not json":     func(string) (int, string) { return 200, "<html>" },
		"unauthorised": func(string) (int, string) { return 401, "" },
	} {
		p.mu.Lock()
		p.answer = answer
		p.mu.Unlock()
		got, err := svc.History(t.Context(), "c", "pod", "ns", "web", 60)
		require.NoError(t, err, name)
		assert.False(t, got.Available, name)
		assert.NotEmpty(t, got.Message, name)
	}
}

func TestHistory_AnUnreachableServer_IsUnavailable(t *testing.T) {
	p := newProm(t)
	svc := p.service(t)
	p.Close()
	got, err := svc.History(t.Context(), "c", "pod", "ns", "web", 60)
	require.NoError(t, err)
	assert.False(t, got.Available)
	assert.NotEmpty(t, got.Message)
}

func TestHistory_NoDataIsAvailableWithEmptySeries(t *testing.T) {
	p := newProm(t)
	p.answer = func(string) (int, string) { return 200, `{"status":"success","data":{"resultType":"matrix","result":[]}}` }
	got, err := p.service(t).History(t.Context(), "c", "pod", "ns", "web", 60)
	require.NoError(t, err)
	assert.True(t, got.Available)
	require.Len(t, got.Series, 2)
	assert.Empty(t, got.Series[0].Points)
}
