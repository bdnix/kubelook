// Package telemetry exports what the server measures about itself: Prometheus metrics, OpenTelemetry
// traces, structured logs and the memory guard. No label value is ever a user, cluster, namespace or
// object name: every label is drawn from a fixed set, and anything else becomes "other".
package telemetry

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const other = "other"

// The allowed values of the labels that callers choose.
var (
	wsChannels    = []string{"watch", "map", "logs", "exec", "file"}
	refusedKinds  = []string{"watch", "exec", "helm"}
	vaultEndpoint = []string{"lookup_self", "renew_self", "mounts", "capabilities", "cluster_config", "creds"}
	vaultOutcomes = []string{"ok", "denied", "error"}
	kubeVerbs     = []string{"get", "watch", "create", "update", "patch", "delete"}
	kubeOutcomes  = []string{"ok", "client_error", "server_error", "error"}
	httpMethods   = []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions,
	}
)

// WatchStats are the numbers the watch gauges and the per-user histogram are made of.
type WatchStats struct {
	Watches, Subscribers, CachedObjects int
	PerUser                             []int // the number of upstream watches of each user that has any
}

// JobCount is the number of jobs of one type in one status.
type JobCount struct {
	Type, Status string
	Count        int
}

// Sources are read when the metrics are scraped. A nil source reads as zero.
type Sources struct {
	Sessions      func() int
	Watch         func() WatchStats
	WSConnections func() int
	Jobs          func() []JobCount
}

// Deps are the dependencies of Metrics.
type Deps struct {
	Version, Commit string
	Sources         Sources
}

// Metrics is the set of metrics of one replica.
type Metrics struct {
	reg *prometheus.Registry

	wsDropped     *prometheus.CounterVec
	logDropped    prometheus.Counter
	logStreams    prometheus.Gauge
	execSessions  prometheus.Gauge
	refused       *prometheus.CounterVec
	vaultDuration *prometheus.HistogramVec
	kubeDuration  *prometheus.HistogramVec
	httpRequests  *prometheus.CounterVec
	httpDuration  *prometheus.HistogramVec
}

// NewMetrics registers every metric in reg.
func NewMetrics(reg *prometheus.Registry, d Deps) *Metrics {
	latency := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}
	m := &Metrics{
		reg: reg,
		wsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubelook_ws_dropped_messages_total", Help: "WebSocket messages dropped for slow clients, by channel kind.",
		}, []string{"channel"}),
		logDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubelook_log_lines_dropped_total", Help: "Log lines dropped because a client could not keep up.",
		}),
		logStreams:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "kubelook_log_streams_active", Help: "Open log streams."}),
		execSessions: prometheus.NewGauge(prometheus.GaugeOpts{Name: "kubelook_exec_sessions_active", Help: "Open shells and debug sessions."}),
		refused: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubelook_refused_requests_total", Help: "Requests refused because the server was busy, by kind.",
		}, []string{"kind"}),
		vaultDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubelook_vault_request_duration_seconds", Help: "Duration of requests to Vault.", Buckets: latency,
		}, []string{"endpoint", "outcome"}),
		kubeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubelook_kube_request_duration_seconds", Help: "Time until the Kubernetes API answered a request.", Buckets: latency,
		}, []string{"verb", "outcome"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubelook_http_requests_total", Help: "HTTP requests, by route pattern, method and status.",
		}, []string{"route", "method", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubelook_http_request_duration_seconds", Help: "Duration of HTTP requests, by route pattern.", Buckets: latency,
		}, []string{"route"}),
	}
	reg.MustRegister(m.wsDropped, m.logDropped, m.logStreams, m.execSessions, m.refused, m.vaultDuration, m.kubeDuration, m.httpRequests, m.httpDuration,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	info := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubelook_build_info", Help: "Build information; the value is always 1.",
		ConstLabels: prometheus.Labels{"version": d.Version, "commit": d.Commit},
	})
	info.Set(1)
	reg.MustRegister(info)

	s := d.Sources
	intFn := func(f func() int) func() float64 {
		return func() float64 {
			if f == nil {
				return 0
			}
			return float64(f())
		}
	}
	watch := func() WatchStats {
		if s.Watch == nil {
			return WatchStats{}
		}
		return s.Watch()
	}
	for _, g := range []struct {
		name, help string
		fn         func() float64
	}{
		{"kubelook_sessions_active", "Signed-in sessions.", intFn(s.Sessions)},
		{"kubelook_watches_active", "Upstream Kubernetes watches held by this replica.", func() float64 { return float64(watch().Watches) }},
		{"kubelook_watch_subscribers", "Views attached to the upstream watches.", func() float64 { return float64(watch().Subscribers) }},
		{"kubelook_watch_cached_objects", "Objects held in the watch caches.", func() float64 { return float64(watch().CachedObjects) }},
		{"kubelook_ws_connections", "Open WebSocket connections.", intFn(s.WSConnections)},
	} {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: g.name, Help: g.help}, g.fn))
	}
	reg.MustRegister(&scrapeCollector{watch: watch, jobs: s.Jobs})
	return m
}

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// WSDropped counts n messages dropped on a channel of the given kind.
func (m *Metrics) WSDropped(channel string, n int) {
	m.wsDropped.WithLabelValues(bound(wsChannels, channel)).Add(float64(n))
}

// LogLinesDropped counts n dropped log lines.
func (m *Metrics) LogLinesDropped(n int) { m.logDropped.Add(float64(n)) }

// LogStreamOpened and LogStreamClosed keep the count of open log streams.
func (m *Metrics) LogStreamOpened() { m.logStreams.Inc() }

// LogStreamClosed see LogStreamOpened.
func (m *Metrics) LogStreamClosed() { m.logStreams.Dec() }

// ExecOpened and ExecClosed keep the count of open shells and debug sessions.
func (m *Metrics) ExecOpened() { m.execSessions.Inc() }

// ExecClosed see ExecOpened.
func (m *Metrics) ExecClosed() { m.execSessions.Dec() }

// Refused counts one request refused because the server was busy.
func (m *Metrics) Refused(kind string) { m.refused.WithLabelValues(bound(refusedKinds, kind)).Inc() }

// ObserveVault records one request to Vault.
func (m *Metrics) ObserveVault(endpoint, outcome string, d time.Duration) {
	m.vaultDuration.WithLabelValues(bound(vaultEndpoint, endpoint), bound(vaultOutcomes, outcome)).Observe(d.Seconds())
}

// ObserveKube records one request to a Kubernetes API.
func (m *Metrics) ObserveKube(verb, outcome string, d time.Duration) {
	m.kubeDuration.WithLabelValues(bound(kubeVerbs, verb), bound(kubeOutcomes, outcome)).Observe(d.Seconds())
}

// ObserveHTTP records one request. route is the pattern the router matched, never the path.
func (m *Metrics) ObserveHTTP(route, method string, status int, d time.Duration) {
	if !slices.Contains(httpMethods, method) {
		method = "OTHER"
	}
	m.httpRequests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(route).Observe(d.Seconds())
}

func bound(allowed []string, v string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return other
}

// scrapeCollector exports what is only known at scrape time and has no fixed set of labels:
// the number of watches of each user as a histogram and the job counts by type and status (both come from the server's own job handlers, never from users).
type scrapeCollector struct {
	watch func() WatchStats
	jobs  func() []JobCount
}

var (
	perUserDesc = prometheus.NewDesc("kubelook_watches_per_user", "Upstream watches held per user that has any.", nil, nil)
	jobsDesc    = prometheus.NewDesc("kubelook_jobs", "Jobs by type and status.", []string{"type", "status"}, nil)
	// watchBuckets surround the default limit of 50 watches per user.
	watchBuckets = []float64{1, 2, 5, 10, 20, 30, 40, 50}
)

func (c *scrapeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- perUserDesc
	ch <- jobsDesc
}

func (c *scrapeCollector) Collect(ch chan<- prometheus.Metric) {
	counts, sum := map[float64]uint64{}, 0.0
	users := c.watch().PerUser
	for _, n := range users {
		sum += float64(n)
		for _, b := range watchBuckets {
			if float64(n) <= b {
				counts[b]++
			}
		}
	}
	ch <- prometheus.MustNewConstHistogram(perUserDesc, uint64(len(users)), sum, counts)
	if c.jobs == nil {
		return
	}
	for _, j := range c.jobs() {
		ch <- prometheus.MustNewConstMetric(jobsDesc, prometheus.GaugeValue, float64(j.Count), j.Type, j.Status)
	}
}
