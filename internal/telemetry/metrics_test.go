package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/telemetry"
)

func newMetrics(t *testing.T, s telemetry.Sources) (*telemetry.Metrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := telemetry.NewMetrics(reg, telemetry.Deps{Version: "1.2.3", Commit: "abc1234", Sources: s})
	return m, reg
}

func family(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()
	fams, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range fams {
		if f.GetName() == name {
			return f
		}
	}
	t.Fatalf("metric %s is not exported", name)
	return nil
}

func labels(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, l := range m.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

// find returns the one sample of the family whose labels include want.
func find(t *testing.T, f *dto.MetricFamily, want map[string]string) *dto.Metric {
	t.Helper()
	for _, m := range f.GetMetric() {
		got := labels(m)
		ok := true
		for k, v := range want {
			if got[k] != v {
				ok = false
			}
		}
		if ok {
			return m
		}
	}
	t.Fatalf("%s has no sample with labels %v", f.GetName(), want)
	return nil
}

func TestMetrics_BuildInfo_CarriesVersionAndCommit(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{})
	m := find(t, family(t, reg, "kubelook_build_info"), map[string]string{"version": "1.2.3", "commit": "abc1234"})
	assert.InDelta(t, 1, m.GetGauge().GetValue(), 0)
}

func TestMetrics_ScrapeTimeGauges_ReadTheirSources(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{
		Sessions: func() int { return 7 },
		Watch: func() telemetry.WatchStats {
			return telemetry.WatchStats{Watches: 5, Subscribers: 9, CachedObjects: 1234, PerUser: []int{1, 3, 12}}
		},
		WSConnections: func() int { return 4 },
		Jobs: func() []telemetry.JobCount {
			return []telemetry.JobCount{{Type: "helm.install", Status: "running", Count: 2}, {Type: "node.drain", Status: "failed", Count: 1}}
		},
	})
	for name, want := range map[string]float64{
		"kubelook_sessions_active":      7,
		"kubelook_watches_active":       5,
		"kubelook_watch_subscribers":    9,
		"kubelook_watch_cached_objects": 1234,
		"kubelook_ws_connections":       4,
	} {
		assert.InDelta(t, want, family(t, reg, name).GetMetric()[0].GetGauge().GetValue(), 0, name)
	}
	jobs := family(t, reg, "kubelook_jobs")
	assert.InDelta(t, 2, find(t, jobs, map[string]string{"type": "helm.install", "status": "running"}).GetGauge().GetValue(), 0)
	assert.InDelta(t, 1, find(t, jobs, map[string]string{"type": "node.drain", "status": "failed"}).GetGauge().GetValue(), 0)
}

func TestMetrics_WatchesPerUser_IsAHistogramOfTheUsersWatchCounts(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{
		Watch: func() telemetry.WatchStats { return telemetry.WatchStats{PerUser: []int{1, 3, 12}} },
	})
	h := family(t, reg, "kubelook_watches_per_user").GetMetric()[0].GetHistogram()
	assert.EqualValues(t, 3, h.GetSampleCount())
	assert.InDelta(t, 16, h.GetSampleSum(), 0)
}

func TestMetrics_MissingSources_ExportZeroes(t *testing.T) {
	_, reg := newMetrics(t, telemetry.Sources{})
	for _, name := range []string{"kubelook_sessions_active", "kubelook_watches_active", "kubelook_ws_connections"} {
		assert.InDelta(t, 0, family(t, reg, name).GetMetric()[0].GetGauge().GetValue(), 0, name)
	}
}

func TestMetrics_Counters_CountByLabel(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.WSDropped("logs", 3)
	m.WSDropped("logs", 2)
	m.LogLinesDropped(10)
	m.Refused("watch")
	m.Refused("watch")
	m.Refused("exec")

	assert.InDelta(t, 5, find(t, family(t, reg, "kubelook_ws_dropped_messages_total"), map[string]string{"channel": "logs"}).GetCounter().GetValue(), 0)
	assert.InDelta(t, 10, family(t, reg, "kubelook_log_lines_dropped_total").GetMetric()[0].GetCounter().GetValue(), 0)
	refused := family(t, reg, "kubelook_refused_requests_total")
	assert.InDelta(t, 2, find(t, refused, map[string]string{"kind": "watch"}).GetCounter().GetValue(), 0)
	assert.InDelta(t, 1, find(t, refused, map[string]string{"kind": "exec"}).GetCounter().GetValue(), 0)
}

func TestMetrics_Durations_AreObservedByLabel(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.ObserveVault("creds", "ok", 120*time.Millisecond)
	m.ObserveKube("list", "ok", 30*time.Millisecond)
	m.ObserveKube("list", "ok", 50*time.Millisecond)
	m.ObserveHTTP("GET /api/v1/me", "GET", 200, 5*time.Millisecond)
	m.ObserveHTTP("GET /api/v1/me", "GET", 401, 5*time.Millisecond)

	v := find(t, family(t, reg, "kubelook_vault_request_duration_seconds"), map[string]string{"endpoint": "creds", "outcome": "ok"})
	assert.EqualValues(t, 1, v.GetHistogram().GetSampleCount())
	assert.InDelta(t, 0.12, v.GetHistogram().GetSampleSum(), 1e-9)

	k := find(t, family(t, reg, "kubelook_kube_request_duration_seconds"), map[string]string{"verb": "list", "outcome": "ok"})
	assert.EqualValues(t, 2, k.GetHistogram().GetSampleCount())

	reqs := family(t, reg, "kubelook_http_requests_total")
	assert.InDelta(t, 1, find(t, reqs, map[string]string{"route": "GET /api/v1/me", "method": "GET", "status": "200"}).GetCounter().GetValue(), 0)
	assert.InDelta(t, 1, find(t, reqs, map[string]string{"route": "GET /api/v1/me", "method": "GET", "status": "401"}).GetCounter().GetValue(), 0)
	d := find(t, family(t, reg, "kubelook_http_request_duration_seconds"), map[string]string{"route": "GET /api/v1/me"})
	assert.EqualValues(t, 2, d.GetHistogram().GetSampleCount())
}

func TestMetrics_Labels_AreBoundedSets(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	// Whatever a caller passes, a label value never becomes a user, cluster, namespace or object name.
	m.WSDropped("cluster-prod-1", 1)
	m.Refused("alice@example.com")
	m.ObserveVault("secret/data/team-a", "boom", time.Millisecond)
	m.ObserveKube("frobnicate", "weird", time.Millisecond)
	m.ObserveHTTP("GET /x", "BREW", 200, time.Millisecond)

	assert.Contains(t, labels(family(t, reg, "kubelook_ws_dropped_messages_total").GetMetric()[0]), "channel")
	assert.Equal(t, "other", labels(family(t, reg, "kubelook_ws_dropped_messages_total").GetMetric()[0])["channel"])
	assert.Equal(t, "other", labels(family(t, reg, "kubelook_refused_requests_total").GetMetric()[0])["kind"])
	vl := labels(family(t, reg, "kubelook_vault_request_duration_seconds").GetMetric()[0])
	assert.Equal(t, "other", vl["endpoint"])
	assert.Equal(t, "other", vl["outcome"])
	kl := labels(family(t, reg, "kubelook_kube_request_duration_seconds").GetMetric()[0])
	assert.Equal(t, "other", kl["verb"])
	assert.Equal(t, "other", kl["outcome"])
	assert.Equal(t, "OTHER", labels(family(t, reg, "kubelook_http_requests_total").GetMetric()[0])["method"])
}

func TestMetrics_Handler_ServesTheExpositionFormat(t *testing.T) {
	m, _ := newMetrics(t, telemetry.Sources{Sessions: func() int { return 1 }})
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "kubelook_build_info{")
	assert.True(t, strings.Contains(body, "kubelook_sessions_active 1"), body)
}

func TestMetrics_LogStreamsAndExecSessions_AreCountedUpAndDown(t *testing.T) {
	m, reg := newMetrics(t, telemetry.Sources{})
	m.LogStreamOpened()
	m.LogStreamOpened()
	m.LogStreamClosed()
	m.ExecOpened()

	assert.InDelta(t, 1, family(t, reg, "kubelook_log_streams_active").GetMetric()[0].GetGauge().GetValue(), 0)
	assert.InDelta(t, 1, family(t, reg, "kubelook_exec_sessions_active").GetMetric()[0].GetGauge().GetValue(), 0)
	m.ExecClosed()
	assert.InDelta(t, 0, family(t, reg, "kubelook_exec_sessions_active").GetMetric()[0].GetGauge().GetValue(), 0)
}
