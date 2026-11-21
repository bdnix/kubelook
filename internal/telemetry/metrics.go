// Package telemetry holds Kubelook's own observability: Prometheus metrics, OpenTelemetry traces,
// structured logs and the memory guard. Label values are fixed vocabularies or route patterns,
// never user, cluster, namespace or object names.
package telemetry

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bdnix/kubelook/internal/clock"
)

// sourceTimeout bounds how long one scrape waits for a source that reads the database.
const sourceTimeout = 2 * time.Second

// BuildInfo identifies the running binary.
type BuildInfo struct{ Version, Commit string }

// JobKey is the label set of kubelook_jobs.
type JobKey struct{ Type, Status string }

// Sources are read at scrape time, so the gauges they feed can never drift from the real state.
// A nil source leaves its gauge out; a source that fails leaves only its own gauge out.
type Sources struct {
	Sessions      func(ctx context.Context) (int, error)
	Watch         func() (watches, subscribers, cachedObjects int)
	WSConnections func() int
	Jobs          func(ctx context.Context) (map[JobKey]int, error)
}

// Metrics owns the collectors and the registry they live in.
type Metrics struct {
	reg *prometheus.Registry

	watchesPerUser prometheus.Histogram
	wsDropped      *prometheus.CounterVec
	logStreams     prometheus.Gauge
	logDropped     prometheus.Counter
	execSessions   prometheus.Gauge
	vaultDuration  *prometheus.HistogramVec
	kubeDuration   *prometheus.HistogramVec
	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	refused        *prometheus.CounterVec
}

var latencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

// NewMetrics registers every collector in reg.
func NewMetrics(reg *prometheus.Registry, b BuildInfo, src Sources) *Metrics {
	m := &Metrics{
		reg: reg,
		watchesPerUser: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "kubelook_watches_per_user", Help: "Upstream watches a user holds, observed whenever a watch is opened.",
			Buckets: []float64{1, 2, 5, 10, 20, 30, 40, 50},
		}),
		wsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubelook_ws_dropped_messages_total", Help: "Droppable WebSocket frames discarded under back-pressure."}, []string{"channel"}),
		logStreams: prometheus.NewGauge(prometheus.GaugeOpts{Name: "kubelook_log_streams_active", Help: "Open log streams."}),
		logDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubelook_log_lines_dropped_total", Help: "Log lines discarded to keep up with the browser."}),
		execSessions: prometheus.NewGauge(prometheus.GaugeOpts{Name: "kubelook_exec_sessions_active", Help: "Open shells."}),
		vaultDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubelook_vault_request_duration_seconds", Help: "Vault request latency.", Buckets: latencyBuckets}, []string{"endpoint", "outcome"}),
		kubeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubelook_kube_request_duration_seconds", Help: "Kubernetes API request latency (time to response headers).", Buckets: latencyBuckets}, []string{"verb", "outcome"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubelook_http_requests_total", Help: "HTTP requests by route pattern."}, []string{"route", "method", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubelook_http_request_duration_seconds", Help: "HTTP request latency by route pattern.", Buckets: latencyBuckets}, []string{"route"}),
		refused: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubelook_refused_requests_total", Help: "New work refused because the server is busy."}, []string{"kind"}),
	}
	build := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubelook_build_info", Help: "Build information; the value is always 1.",
		ConstLabels: prometheus.Labels{"version": b.Version, "commit": b.Commit}})
	build.Set(1)
	reg.MustRegister(m.watchesPerUser, m.wsDropped, m.logStreams, m.logDropped, m.execSessions,
		m.vaultDuration, m.kubeDuration, m.httpRequests, m.httpDuration, m.refused, build, &sourceCollector{src: src})
	return m
}

// Handler serves the registry in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// WatchOpened records how many watches the user holds after opening one.
func (m *Metrics) WatchOpened(userWatches int) { m.watchesPerUser.Observe(float64(userWatches)) }

// WSDropped counts frames dropped on a channel kind ("watch", "logs", …).
func (m *Metrics) WSDropped(channel string, n int) {
	m.wsDropped.WithLabelValues(channel).Add(float64(n))
}

// LogStreamStarted and LogStreamEnded keep kubelook_log_streams_active.
func (m *Metrics) LogStreamStarted() { m.logStreams.Inc() }
func (m *Metrics) LogStreamEnded()   { m.logStreams.Dec() }

// LogLinesDropped counts log lines discarded for a slow browser.
func (m *Metrics) LogLinesDropped(n int) { m.logDropped.Add(float64(n)) }

// ExecStarted and ExecEnded keep kubelook_exec_sessions_active.
func (m *Metrics) ExecStarted() { m.execSessions.Inc() }
func (m *Metrics) ExecEnded()   { m.execSessions.Dec() }

// Refused counts new work the memory guard turned away; kind is "watch", "exec" or "helm".
func (m *Metrics) Refused(kind string) { m.refused.WithLabelValues(kind).Inc() }

// ObserveVault and ObserveKube record one outbound request.
func (m *Metrics) ObserveVault(endpoint, outcome string, d time.Duration) {
	m.vaultDuration.WithLabelValues(endpoint, outcome).Observe(d.Seconds())
}

func (m *Metrics) ObserveKube(verb, outcome string, d time.Duration) {
	m.kubeDuration.WithLabelValues(verb, outcome).Observe(d.Seconds())
}

// HTTPRequest records one served request. route is the mux pattern; an empty one (the request never
// reached the router) is "unmatched". The pattern's method prefix is dropped: method is its own label.
func (m *Metrics) HTTPRequest(route, method string, status int, d time.Duration) {
	if _, after, ok := strings.Cut(route, " "); ok {
		route = after
	}
	if route == "" {
		route = "unmatched"
	}
	m.httpRequests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(route).Observe(d.Seconds())
}

// RoundTripper times every request of base and reports it as label(req) and an outcome:
// "ok" (below 400), "client_error", "server_error" or "error" (no response). Neither the URL,
// the query nor any header reaches record.
func RoundTripper(base http.RoundTripper, clk clock.Clock, label func(*http.Request) string, record func(label, outcome string, d time.Duration)) http.RoundTripper {
	return &timedTransport{base: base, clk: clk, label: label, record: record}
}

type timedTransport struct {
	base   http.RoundTripper
	clk    clock.Clock
	label  func(*http.Request) string
	record func(label, outcome string, d time.Duration)
}

func (t *timedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := t.clk.Now()
	resp, err := t.base.RoundTrip(req)
	t.record(t.label(req), outcomeOf(resp, err), t.clk.Now().Sub(start))
	return resp, err
}

// WrappedRoundTripper lets client-go reach the transport underneath (connection upgrades).
func (t *timedTransport) WrappedRoundTripper() http.RoundTripper { return t.base }

func outcomeOf(resp *http.Response, err error) string {
	switch {
	case err != nil || resp == nil:
		return "error"
	case resp.StatusCode >= 500:
		return "server_error"
	case resp.StatusCode >= 400:
		return "client_error"
	default:
		return "ok"
	}
}

// KubeVerb names a Kubernetes API request by what it does, from the method and the watch parameter.
func KubeVerb(r *http.Request) string {
	switch r.Method {
	case http.MethodGet:
		if w := r.URL.Query().Get("watch"); w == "true" || w == "1" {
			return "watch"
		}
		return "get"
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	default:
		return "other"
	}
}

// sourceCollector turns the scrape-time sources into gauges.
type sourceCollector struct{ src Sources }

var (
	descSessions    = prometheus.NewDesc("kubelook_sessions_active", "Signed-in sessions.", nil, nil)
	descWatches     = prometheus.NewDesc("kubelook_watches_active", "Upstream Kubernetes watches.", nil, nil)
	descSubscribers = prometheus.NewDesc("kubelook_watch_subscribers", "Views attached to upstream watches.", nil, nil)
	descCached      = prometheus.NewDesc("kubelook_watch_cached_objects", "Objects held in the watch caches.", nil, nil)
	descWS          = prometheus.NewDesc("kubelook_ws_connections", "Open WebSocket connections.", nil, nil)
	descJobs        = prometheus.NewDesc("kubelook_jobs", "Durable jobs by type and status.", []string{"type", "status"}, nil)
)

func (c *sourceCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{descSessions, descWatches, descSubscribers, descCached, descWS, descJobs} {
		ch <- d
	}
}

func (c *sourceCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	gauge := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
	}
	if c.src.Sessions != nil {
		if n, err := c.src.Sessions(ctx); err == nil {
			gauge(descSessions, float64(n))
		}
	}
	if c.src.Watch != nil {
		w, s, o := c.src.Watch()
		gauge(descWatches, float64(w))
		gauge(descSubscribers, float64(s))
		gauge(descCached, float64(o))
	}
	if c.src.WSConnections != nil {
		gauge(descWS, float64(c.src.WSConnections()))
	}
	if c.src.Jobs != nil {
		if jobs, err := c.src.Jobs(ctx); err == nil {
			for k, n := range jobs {
				gauge(descJobs, float64(n), k.Type, k.Status)
			}
		}
	}
}
