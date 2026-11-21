package telemetry_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/telemetry"
)

func newMetrics(t *testing.T, src telemetry.Sources) (*telemetry.Metrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	return telemetry.NewMetrics(reg, telemetry.BuildInfo{Version: "1.2.3", Commit: "abc"}, src), reg
}

func gather(t *testing.T, reg *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	fams, err := reg.Gather()
	require.NoError(t, err)
	out := map[string]*dto.MetricFamily{}
	for _, f := range fams {
		out[f.GetName()] = f
	}
	return out
}

func TestSources_Scrape_ReportGauges(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{
		Sessions:      func(context.Context) (int, error) { return 7, nil },
		Watch:         func() (int, int, int) { return 3, 5, 120 },
		WSConnections: func() int { return 4 },
		Jobs: func(context.Context) (map[telemetry.JobKey]int, error) {
			return map[telemetry.JobKey]int{{Type: "helm.install", Status: "running"}: 2}, nil
		},
	})
	fams := gather(t, reg)
	val := func(name string) float64 {
		require.Contains(t, fams, name)
		return fams[name].GetMetric()[0].GetGauge().GetValue()
	}
	assert.EqualValues(t, 7, val("kubelook_sessions_active"))
	assert.EqualValues(t, 3, val("kubelook_watches_active"))
	assert.EqualValues(t, 5, val("kubelook_watch_subscribers"))
	assert.EqualValues(t, 120, val("kubelook_watch_cached_objects"))
	assert.EqualValues(t, 4, val("kubelook_ws_connections"))
	assert.EqualValues(t, 2, val("kubelook_jobs"))
	labels := fams["kubelook_jobs"].GetMetric()[0].GetLabel()
	assert.Equal(t, "type", labels[0].GetName())
	assert.Equal(t, "helm.install", labels[0].GetValue())
	assert.Equal(t, "status", labels[1].GetName())
	assert.Equal(t, "running", labels[1].GetValue())
}

func TestSources_SourceFails_ThatGaugeIsOmittedAndScrapeSucceeds(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{
		Sessions: func(context.Context) (int, error) { return 0, errors.New("db down") },
		Jobs:     func(context.Context) (map[telemetry.JobKey]int, error) { return nil, errors.New("db down") },
	})
	fams := gather(t, reg)
	assert.NotContains(t, fams, "kubelook_sessions_active")
	assert.NotContains(t, fams, "kubelook_jobs")
	assert.Contains(t, fams, "kubelook_build_info")
}

func TestSources_NoSources_NoSourceGauges(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{})
	fams := gather(t, reg)
	for _, n := range []string{"kubelook_sessions_active", "kubelook_watches_active", "kubelook_ws_connections", "kubelook_jobs"} {
		assert.NotContains(t, fams, n)
	}
}

func TestBuildInfo_HasVersionAndCommit(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{})
	fams := gather(t, reg)
	m := fams["kubelook_build_info"].GetMetric()[0]
	assert.EqualValues(t, 1, m.GetGauge().GetValue())
	got := map[string]string{}
	for _, l := range m.GetLabel() {
		got[l.GetName()] = l.GetValue()
	}
	assert.Equal(t, map[string]string{"version": "1.2.3", "commit": "abc"}, got)
}

func TestWatchOpened_ObservesWatchesPerUser(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.WatchOpened(3)
	m.WatchOpened(10)
	h := gather(t, reg)["kubelook_watches_per_user"].GetMetric()[0].GetHistogram()
	assert.EqualValues(t, 2, h.GetSampleCount())
	assert.EqualValues(t, 13, h.GetSampleSum())
}

func TestWSDropped_CountsPerChannelKind(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.WSDropped("logs", 2)
	m.WSDropped("logs", 1)
	m.WSDropped("watch", 4)
	got := map[string]float64{}
	for _, mm := range gather(t, reg)["kubelook_ws_dropped_messages_total"].GetMetric() {
		assert.Equal(t, "channel", mm.GetLabel()[0].GetName())
		got[mm.GetLabel()[0].GetValue()] = mm.GetCounter().GetValue()
	}
	assert.Equal(t, map[string]float64{"logs": 3, "watch": 4}, got)
}

func TestLogAndExecGauges(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.LogStreamStarted()
	m.LogStreamStarted()
	m.LogStreamEnded()
	m.LogLinesDropped(5)
	m.ExecStarted()
	fams := gather(t, reg)
	assert.EqualValues(t, 1, fams["kubelook_log_streams_active"].GetMetric()[0].GetGauge().GetValue())
	assert.EqualValues(t, 5, fams["kubelook_log_lines_dropped_total"].GetMetric()[0].GetCounter().GetValue())
	assert.EqualValues(t, 1, fams["kubelook_exec_sessions_active"].GetMetric()[0].GetGauge().GetValue())
	m.ExecEnded()
	assert.EqualValues(t, 0, gather(t, reg)["kubelook_exec_sessions_active"].GetMetric()[0].GetGauge().GetValue())
}

func TestRefused_CountsPerKind(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.Refused("watch")
	m.Refused("watch")
	m.Refused("exec")
	f := gather(t, reg)["kubelook_refused_requests_total"]
	got := map[string]float64{}
	for _, mm := range f.GetMetric() {
		got[mm.GetLabel()[0].GetValue()] = mm.GetCounter().GetValue()
	}
	assert.Equal(t, map[string]float64{"watch": 2, "exec": 1}, got)
}

func TestHTTPRequest_RecordsCountAndDurationByRoutePattern(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.HTTPRequest("/api/v1/clusters/{cluster}/pods", "GET", 200, 150*time.Millisecond)
	m.HTTPRequest("/api/v1/clusters/{cluster}/pods", "GET", 200, 50*time.Millisecond)
	m.HTTPRequest("/api/v1/me", "GET", 401, time.Millisecond)
	fams := gather(t, reg)
	var pods float64
	for _, mm := range fams["kubelook_http_requests_total"].GetMetric() {
		l := map[string]string{}
		for _, x := range mm.GetLabel() {
			l[x.GetName()] = x.GetValue()
		}
		if l["route"] == "/api/v1/clusters/{cluster}/pods" {
			assert.Equal(t, "GET", l["method"])
			assert.Equal(t, "200", l["status"])
			pods = mm.GetCounter().GetValue()
		}
	}
	assert.EqualValues(t, 2, pods)
	var count uint64
	for _, mm := range fams["kubelook_http_request_duration_seconds"].GetMetric() {
		if mm.GetLabel()[0].GetValue() == "/api/v1/clusters/{cluster}/pods" {
			count = mm.GetHistogram().GetSampleCount()
			assert.InDelta(t, 0.2, mm.GetHistogram().GetSampleSum(), 1e-9)
		}
	}
	assert.EqualValues(t, 2, count)
}

func TestHTTPRequest_EmptyRoute_IsUnmatched(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.HTTPRequest("", "GET", 404, time.Millisecond)
	f := gather(t, reg)["kubelook_http_requests_total"]
	assert.Equal(t, "unmatched", f.GetMetric()[0].GetLabel()[0].GetValue())
}

type fakeRT struct {
	clk    *clock.Fake
	took   time.Duration
	status int
	err    error
}

func (f *fakeRT) RoundTrip(*http.Request) (*http.Response, error) {
	f.clk.Advance(f.took)
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: http.NoBody}, nil
}

func TestRoundTripper_Outcomes(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	cases := []struct {
		name    string
		status  int
		err     error
		outcome string
	}{
		{"ok", 200, nil, "ok"},
		{"redirect", 302, nil, "ok"},
		{"client error", 404, nil, "client_error"},
		{"server error", 503, nil, "server_error"},
		{"transport error", 0, errors.New("dial"), "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotLabel, gotOutcome string
			var gotD time.Duration
			rt := telemetry.RoundTripper(&fakeRT{clk: clk, took: 250 * time.Millisecond, status: c.status, err: c.err}, clk,
				func(r *http.Request) string { return "label-" + r.Method },
				func(label, outcome string, d time.Duration) { gotLabel, gotOutcome, gotD = label, outcome, d })
			req := httptest.NewRequest(http.MethodGet, "http://example.invalid/secret/path?token=abc", nil)
			resp, err := rt.RoundTrip(req)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, c.status, resp.StatusCode)
			}
			assert.Equal(t, "label-GET", gotLabel)
			assert.Equal(t, c.outcome, gotOutcome)
			assert.Equal(t, 250*time.Millisecond, gotD)
		})
	}
}

func TestRoundTripper_ExposesWrappedTransport(t *testing.T) {
	base := &fakeRT{}
	rt := telemetry.RoundTripper(base, clock.NewFake(time.Unix(0, 0)), func(*http.Request) string { return "" }, func(string, string, time.Duration) {})
	w, ok := rt.(interface{ WrappedRoundTripper() http.RoundTripper })
	require.True(t, ok)
	assert.Same(t, base, w.WrappedRoundTripper())
}

func TestVaultAndKube_ObserversRecordHistograms(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.ObserveVault("creds", "ok", 100*time.Millisecond)
	m.ObserveKube("get", "client_error", 20*time.Millisecond)
	fams := gather(t, reg)
	v := fams["kubelook_vault_request_duration_seconds"].GetMetric()[0]
	assert.Equal(t, "endpoint", v.GetLabel()[0].GetName())
	assert.Equal(t, "creds", v.GetLabel()[0].GetValue())
	assert.Equal(t, "outcome", v.GetLabel()[1].GetName())
	assert.Equal(t, "ok", v.GetLabel()[1].GetValue())
	k := fams["kubelook_kube_request_duration_seconds"].GetMetric()[0]
	assert.Equal(t, "verb", k.GetLabel()[0].GetName())
	assert.Equal(t, "get", k.GetLabel()[0].GetValue())
	assert.Equal(t, "client_error", k.GetLabel()[1].GetValue())
}

func TestKubeVerb_FromRequest(t *testing.T) {
	cases := []struct{ method, url, verb string }{
		{"GET", "https://k/api/v1/pods", "get"},
		{"GET", "https://k/api/v1/pods?watch=true", "watch"},
		{"GET", "https://k/api/v1/pods?watch=1&resourceVersion=3", "watch"},
		{"POST", "https://k/api/v1/pods", "create"},
		{"PUT", "https://k/api/v1/pods/a", "update"},
		{"PATCH", "https://k/api/v1/pods/a", "patch"},
		{"DELETE", "https://k/api/v1/pods/a", "delete"},
		{"HEAD", "https://k/api/v1/pods", "other"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.url, nil)
		assert.Equal(t, c.verb, telemetry.KubeVerb(req), c.method+" "+c.url)
	}
}

func TestMetricLabels_NeverContainUserClusterNamespaceOrObjectNames(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	m, reg := newMetrics(t, telemetry.Sources{})
	rt := telemetry.RoundTripper(&fakeRT{clk: clk, status: 200}, clk, telemetry.KubeVerb, m.ObserveKube)
	req := httptest.NewRequest(http.MethodGet, "https://prod-cluster/api/v1/namespaces/payments/pods/alice-pod", nil)
	_, err := rt.RoundTrip(req)
	require.NoError(t, err)
	m.HTTPRequest("/api/v1/clusters/{cluster}/pods", "GET", 200, 0)

	fams, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range fams {
		for _, mm := range f.GetMetric() {
			for _, l := range mm.GetLabel() {
				v := strings.ToLower(l.GetValue())
				for _, secret := range []string{"alice", "payments", "prod-cluster"} {
					assert.NotContains(t, v, secret, f.GetName())
				}
			}
		}
	}
}
