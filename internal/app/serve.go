package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/auth"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/egress"
	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/files"
	"github.com/bdnix/kubelook/internal/flux"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/helm/helm4"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/logs"
	"github.com/bdnix/kubelook/internal/metrics"
	"github.com/bdnix/kubelook/internal/resourcemap"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/telemetry"
	"github.com/bdnix/kubelook/internal/vault"
	"github.com/bdnix/kubelook/internal/version"
	"github.com/bdnix/kubelook/internal/watch"
	"github.com/bdnix/kubelook/internal/webui"
	"github.com/bdnix/kubelook/internal/ws"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
	// cgroupMemoryMax holds the container's memory limit (cgroup v2).
	cgroupMemoryMax  = "/sys/fs/cgroup/memory.max"
	authHTTPTimeout  = 15 * time.Second
	vaultHTTPTimeout = 15 * time.Second
)

// ServeOptions injects the parts of Serve that tests replace.
type ServeOptions struct {
	Listener net.Listener // nil → listen on cfg.Server.ListenAddress
	Stdout   io.Writer    // audit lines and logs
	Clock    clock.Clock  // nil → clock.Real()
	Rand     io.Reader    // nil → crypto/rand.Reader
	LogLevel slog.Level
	OnReady  func(Ready) // called once the server is accepting connections
	// MemoryUsage and MemoryLimit replace the readers of the memory guard (tests); nil → the Go runtime's
	// memory in use and the container's memory limit.
	MemoryUsage, MemoryLimit func() uint64
}

// Ready describes a running server to OnReady.
type Ready struct {
	Addr     string
	Registry *session.Registry
}

// components are the constructed parts of the server; later steps wire more of them here.
type components struct {
	log          *slog.Logger
	db           *store.DB
	sealer       *crypto.Sealer
	users        *store.Users
	sessions     *session.Manager
	audit        *audit.Recorder
	opener       *cluster.Opener
	ws           *ws.Mux
	vaultMonitor *vault.Monitor
	metrics      *telemetry.Metrics
	tracing      *telemetry.Tracing
	egressProxy  *egress.Proxy // the guarded tunnel of exec; nil when the guard is off
	hub          *watch.Hub
	jobs         *jobs.Runner
	handler      http.Handler
}

func (o *ServeOptions) defaults() {
	if o.Clock == nil {
		o.Clock = clock.Real()
	}
	if o.Rand == nil {
		o.Rand = rand.Reader
	}
	if o.Stdout == nil {
		o.Stdout = io.Discard
	}
	if o.MemoryUsage == nil {
		o.MemoryUsage = telemetry.GoMemoryUsage
	}
	if o.MemoryLimit == nil {
		o.MemoryLimit = telemetry.CgroupLimit(cgroupMemoryMax, func() int64 { return debug.SetMemoryLimit(-1) })
	}
}

// build opens the database, brings the schema up to date (or checks it) and wires the components.
func build(ctx context.Context, cfg *config.Config, o ServeOptions) (*components, error) {
	log := telemetry.NewLogger(o.Stdout, o.LogLevel)
	tracing, err := telemetry.NewTracing(ctx, telemetry.TracingConfig{Endpoint: cfg.Telemetry.OTLPEndpoint})
	if err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, store.Options{Driver: cfg.Database.Driver, SQLitePath: cfg.Database.SQLitePath, URL: cfg.DatabaseURL})
	if err != nil {
		return nil, err
	}
	var tunnel *egress.Proxy // started below when the egress guard is on
	fail := func(err error) (*components, error) {
		_ = db.Close()
		_ = tracing.Shutdown(context.WithoutCancel(ctx))
		if tunnel != nil {
			_ = tunnel.Close()
		}
		return nil, err
	}
	block := cfg.Egress.Blocks()
	if block {
		if tunnel, err = egress.NewProxy(true); err != nil {
			return fail(err)
		}
	}

	if cfg.Database.MigrateOnStart() {
		if err := store.Migrate(ctx, db, store.Migrations()); err != nil {
			return fail(err)
		}
	} else {
		pending, err := store.Pending(ctx, db, store.Migrations())
		if err != nil {
			return fail(err)
		}
		if len(pending) > 0 {
			return fail(errors.New(`database schema is not up to date; run "kubelook migrate"`))
		}
	}

	sealer, err := crypto.NewSealer(cfg.EncryptionKey, o.Rand)
	if err != nil {
		return fail(err)
	}
	mgr := session.NewManager(session.Deps{
		Store: store.NewSessions(db), Clock: o.Clock, Rand: o.Rand, Log: log,
		IdleTimeout: cfg.Session.IdleTimeout.D(), MaxLifetime: cfg.Session.MaxLifetime.D(), CookieSecure: cfg.CookieSecure(),
	})
	c := &components{
		log: log, db: db, sealer: sealer, sessions: mgr, egressProxy: tunnel,
		users: store.NewUsers(db, o.Clock, store.NewID),
		audit: audit.NewRecorder(db, o.Clock, o.Stdout, store.NewID),
	}
	c.tracing = tracing
	guard := telemetry.NewMemGuard(telemetry.MemGuardDeps{
		Clock: o.Clock, Usage: o.MemoryUsage, Limit: o.MemoryLimit, Percent: cfg.Memory.RefuseNewWorkPercent,
	})
	c.metrics = telemetry.NewMetrics(prometheus.NewRegistry(), telemetry.Deps{
		Version: version.Version, Commit: version.Commit, Sources: c.metricSources(),
	})
	authHandlers, err := newAuth(ctx, cfg, o, log, c, sealer)
	if err != nil {
		return fail(err)
	}
	rawVault := vault.NewClient(cfg.Vault.Address, &http.Client{Timeout: vaultHTTPTimeout, Transport: tracing.Transport(http.DefaultTransport)})
	c.vaultMonitor = vault.NewMonitor(rawVault, o.Clock, cfg.Vault.Address != "")
	vaultClient := vault.Instrument(c.vaultMonitor.Wrap(rawVault), o.Clock, c.metrics.ObserveVault)
	cache := cluster.NewDiscoveryCache(o.Clock, mgr.Registry())
	creds := cluster.NewCredentials(cluster.Deps{
		Sessions: store.NewSessions(db), Sealer: sealer, Vault: vaultClient,
		Clock: o.Clock, Config: cfg, Audit: c.audit, Registry: mgr.Registry(), Ender: mgr, Log: log, Cache: cache,
	})
	clusters := cluster.NewRegistry(cluster.RegistryDeps{
		Credentials: creds, Vault: vaultClient, Sessions: store.NewSessions(db), Cache: cache, Config: cfg, Log: log,
	})
	opener := cluster.NewOpener(cluster.OpenerDeps{
		Registry: clusters, Credentials: creds, Sessions: store.NewSessions(db), Sealer: sealer, Vault: vaultClient,
		Clock: o.Clock, Config: cfg, Audit: c.audit, Closers: mgr.Registry(), Log: log,
		BlockLocal: block,
	})
	clusters.SetStates(opener.StateOf)
	c.opener = opener
	kubeClients := kube.NewClients(kube.Deps{Config: opener, Clock: o.Clock, Closers: mgr.Registry(), Observe: c.metrics.ObserveKube, WrapTransport: tracing.Transport})
	publicURL, err := url.Parse(cfg.Server.PublicURL)
	if err != nil {
		return fail(fmt.Errorf("parsing server.publicURL: %w", err))
	}
	c.hub = watch.NewHub(watch.Deps{
		Clients: kubeClients, Clock: o.Clock, Log: log, Closers: mgr.Registry(), MaxPerUser: cfg.Watch.MaxPerUser,
		Guard: guard, Refusals: c.metrics,
		IdleSeconds: func(ctx context.Context, userID string) int {
			st, err := c.users.GetSettings(ctx, userID)
			if err != nil {
				return 0
			}
			return st.WatchIdleSeconds
		},
	})
	c.jobs = jobs.NewRunner(jobs.Deps{
		Store: store.NewJobs(db), Clock: o.Clock, Sealer: sealer, Log: log, ReplicaID: store.NewID(), NewID: store.NewID,
		Limits:   jobs.Limits{Helm: cfg.Helm.MaxConcurrentJobs},
		Sessions: store.NewSessions(db),
		// Jobs run without a request: give them the identity the audit records of a request would carry.
		Context: func(ctx context.Context, j *jobs.Job) context.Context {
			u, err := c.users.Get(ctx, j.UserID)
			if err != nil {
				return ctx
			}
			id := audit.UserIdentity(u)
			id.VaultEntityID, id.VaultDisplayName = creds.VaultIdentity(ctx, j.Session())
			return audit.WithIdentity(ctx, id)
		},
	})
	actionDeps := actions.Deps{Clients: kubeClients, Clock: o.Clock, Audit: c.audit}
	c.jobs.Register(actions.NewBulkHandler(actionDeps))
	c.jobs.Register(actions.NewDrainHandler(actionDeps))
	var guardedClient *http.Client // for what users address: session Helm repositories and OCI registries
	if block {
		guardedClient = &http.Client{Transport: egress.Transport(http.DefaultTransport.(*http.Transport), true)}
	}
	helmService := &helm.Service{
		Factory: &helm4.Factory{Clients: kubeClients, RegistryHTTP: guardedClient},
		Repos:   &helm.Repos{Org: cfg.Helm.Repositories, Sessions: store.NewSessions(db), Sealer: sealer, Clock: o.Clock, BlockLocal: block},
		Fetcher: &helm.Fetcher{MaxBytes: cfg.Helm.MaxChartBytes, GuardedHTTP: guardedClient},
		Audit:   c.audit,
		Guard:   guard, Refusals: c.metrics,
	}
	for _, h := range helm.NewJobHandlers(helmService.JobDeps()) {
		c.jobs.Register(h)
	}
	c.ws = ws.NewMux(ws.Deps{Clock: o.Clock, Log: log, Sessions: mgr, PublicHost: publicURL.Host, Fail: httpapi.ErrorWriter(log), Metrics: c.metrics})
	c.ws.Register("watch.open", c.hub.Factory())
	c.ws.Register("map.open", resourcemap.Factory(resourcemap.Deps{Source: c.hub, Clients: kubeClients, Access: kube.NewAccess(kubeClients, o.Clock, mgr.Registry()), Clock: o.Clock}))
	c.ws.Register("logs.open", logs.NewFactory(logs.Deps{
		Source: logs.NewKubeSource(kubeClients), Resolver: logs.NewKubeResolver(kubeClients), Clock: o.Clock, Audit: c.audit,
		MaxLinesPerSecond: cfg.Logs.MaxLinesPerSecond, HistoryMaxLines: cfg.Logs.HistoryMaxLines,
		KeepAlive: mgr.KeepAlive, Metrics: c.metrics,
	}))
	c.ws.Register("exec.open", execs.NewFactory(execs.Deps{
		Executor: newExecutor(kubeClients, tunnel), Clock: o.Clock, Audit: c.audit,
		IdleTimeout: time.Duration(cfg.Shell.IdleTimeout), KeepAlive: mgr.KeepAlive, NodeShell: cfg.NodeShell.IsEnabled(), Log: log, Metrics: c.metrics, Guard: guard, Refusals: c.metrics,
	}))
	c.ws.Register("file.open", files.NewFactory(files.Deps{
		Executor: newExecutor(kubeClients, tunnel), Clock: o.Clock, Audit: c.audit, Enabled: cfg.FileBrowser.IsEnabled(),
		MaxTransferBytes: cfg.FileBrowser.MaxTransferBytes, KeepAlive: mgr.KeepAlive,
	}))
	c.handler = httpapi.New(httpapi.Deps{
		Config: cfg, Clock: o.Clock, Log: log, WebFS: webui.FS(), Sessions: mgr, Users: c.users,
		Auth: authHandlers, Credentials: creds, Clusters: clusters, Opener: opener, Kube: kubeClients, Audit: c.audit, Jobs: c.jobs, Helm: helmService, Flux: &flux.Inventory{Clients: kubeClients, Helm: helmService}, WS: c.ws,
		Metrics:  metrics.New(metrics.Deps{Clients: kubeClients, Clock: o.Clock, Prometheus: cfg.Prometheus}),
		Debugger: execs.NewDebugger(execs.DebuggerDeps{Clients: kubeClients, Clock: o.Clock, Audit: c.audit, Rand: o.Rand, Debug: cfg.Debug, NodeShell: cfg.NodeShell}), VaultMonitor: c.vaultMonitor, KeyID: sealer.KeyID(),
		Ready:     func(ctx context.Context) error { return db.PingContext(ctx) },
		Telemetry: c.metrics, Tracer: tracing.Tracer(),
	})
	return c, nil
}

// newExecutor is the exec executor: through the guarded tunnel when the egress guard is on.
func newExecutor(c kube.Clients, tunnel *egress.Proxy) execs.Executor {
	if tunnel == nil {
		return execs.NewKubeExecutor(c)
	}
	return execs.NewGuardedKubeExecutor(c, tunnel)
}

// metricSourceTimeout bounds the database reads of one scrape.
const metricSourceTimeout = 2 * time.Second

// metricSources are the numbers read when /metrics is scraped. They read the components built later,
// which exist by the time anything scrapes.
func (c *components) metricSources() telemetry.Sources {
	return telemetry.Sources{
		Sessions: func() int {
			ctx, cancel := context.WithTimeout(context.Background(), metricSourceTimeout)
			defer cancel()
			n, err := store.NewSessions(c.db).Count(ctx)
			if err != nil {
				c.log.Warn("counting sessions for the metrics", "error", err.Error())
			}
			return n
		},
		Watch: func() telemetry.WatchStats {
			st := c.hub.Stats()
			return telemetry.WatchStats{Watches: st.Watches, Subscribers: st.Subscribers, CachedObjects: st.CachedObjects, PerUser: c.hub.WatchesPerUser()}
		},
		WSConnections: func() int { return c.ws.Connections() },
		Jobs: func() []telemetry.JobCount {
			ctx, cancel := context.WithTimeout(context.Background(), metricSourceTimeout)
			defer cancel()
			counts, err := store.NewJobs(c.db).CountByTypeAndStatus(ctx)
			if err != nil {
				c.log.Warn("counting jobs for the metrics", "error", err.Error())
			}
			out := make([]telemetry.JobCount, len(counts))
			for i, n := range counts {
				out[i] = telemetry.JobCount{Type: n.Type, Status: n.Status, Count: n.Count}
			}
			return out
		},
	}
}

// newAuth builds the sign-in handlers. A provider exists only when its client ID is configured.
func newAuth(ctx context.Context, cfg *config.Config, o ServeOptions, log *slog.Logger, c *components, sealer *crypto.Sealer) (*auth.Handlers, error) {
	hc := &http.Client{Timeout: authHTTPTimeout}
	var providers []auth.Provider
	if g := cfg.Auth.Google; g.ClientID != "" {
		p, err := auth.NewGoogle(ctx, auth.GoogleConfig{
			ClientID: g.ClientID, ClientSecret: cfg.GoogleSecret, IssuerURL: g.IssuerURL,
			AllowedDomains: g.AllowedDomains, Restrict: cfg.Auth.Restrict(),
		}, o.Clock, hc)
		if err != nil {
			return nil, fmt.Errorf("configuring Google sign-in: %w", err)
		}
		providers = append(providers, p)
	}
	if g := cfg.Auth.GitHub; g.ClientID != "" {
		providers = append(providers, auth.NewGitHub(auth.GitHubConfig{
			ClientID: g.ClientID, ClientSecret: cfg.GitHubSecret, BaseURL: g.BaseURL, APIBaseURL: g.APIBaseURL,
			AllowedOrgs: g.AllowedOrgs, Restrict: cfg.Auth.Restrict(),
		}, hc))
	}
	return auth.NewHandlers(auth.Deps{
		Providers: providers, PublicURL: cfg.Server.PublicURL, Sealer: sealer, Clock: o.Clock, Rand: o.Rand,
		CookieSecure: cfg.CookieSecure(), Users: c.users, Sessions: c.sessions, Audit: c.audit, Log: log,
		HTTPClient: hc, Fail: httpapi.ErrorWriter(log),
	}), nil
}

// Serve runs the server until ctx is done, then shuts it down: open streams are closed
// at once (no drain period), in-flight requests get a short bound, the database is closed.
func Serve(ctx context.Context, cfg *config.Config, o ServeOptions) error {
	o.defaults()
	c, err := build(ctx, cfg, o)
	if err != nil {
		if ctx.Err() != nil {
			return nil // asked to stop while starting: a clean exit
		}
		return err
	}
	defer func() { _ = c.db.Close() }()
	if c.egressProxy != nil {
		defer func() { _ = c.egressProxy.Close() }()
	}
	defer func() { // after the server has stopped, so the last spans are sent
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := c.tracing.Shutdown(flushCtx); err != nil {
			c.log.Warn("flushing traces", "error", err.Error())
		}
	}()

	ln := o.Listener
	if ln == nil {
		if ln, err = (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Server.ListenAddress); err != nil {
			return fmt.Errorf("listening on %s: %w", cfg.Server.ListenAddress, err)
		}
	}
	for _, w := range cfg.Warnings() {
		c.log.Warn(w)
	}
	c.log.Info("listening", "addr", ln.Addr().String(), "config", cfg)

	var wg sync.WaitGroup
	for _, loop := range []func(context.Context){c.sessions.RunWatchdog, c.sessions.RunSweep, c.opener.RunRefresher, c.vaultMonitor.Run, c.jobs.Run} {
		wg.Add(1)
		go func() { defer wg.Done(); loop(ctx) }()
	}
	defer wg.Wait()

	srv := &http.Server{Handler: c.handler, ReadHeaderTimeout: readHeaderTimeout}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	if o.OnReady != nil {
		o.OnReady(Ready{Addr: ln.Addr().String(), Registry: c.sessions.Registry()})
	}

	select {
	case err := <-serveErr:
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}

	c.ws.Shutdown() // open sockets close with 1001
	c.hub.Close()
	c.jobs.Close() // running jobs are cancelled; their rows stay "running" until recovery on a live replica classifies them
	for _, h := range c.sessions.Registry().Hashes() {
		if err := c.sessions.Registry().Close(h); err != nil {
			c.log.Warn("closing session resources", "error", err.Error())
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving: %w", err)
	}
	return nil
}
