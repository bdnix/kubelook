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
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/webui"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// ServeOptions injects the parts of Serve that tests replace.
type ServeOptions struct {
	Listener net.Listener // nil → listen on cfg.Server.ListenAddress
	Stdout   io.Writer    // audit lines and logs
	Clock    clock.Clock  // nil → clock.Real()
	Rand     io.Reader    // nil → crypto/rand.Reader
	LogLevel slog.Level
	OnReady  func(Ready) // called once the server is accepting connections
}

// Ready describes a running server to OnReady.
type Ready struct {
	Addr     string
	Registry *session.Registry
}

// components are the constructed parts of the server; later steps wire more of them here.
type components struct {
	log      *slog.Logger
	db       *store.DB
	sealer   *crypto.Sealer
	users    *store.Users
	sessions *session.Manager
	audit    *audit.Recorder
	handler  http.Handler
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
}

// build opens the database, brings the schema up to date (or checks it) and wires the components.
func build(ctx context.Context, cfg *config.Config, o ServeOptions) (*components, error) {
	log := slog.New(slog.NewJSONHandler(o.Stdout, &slog.HandlerOptions{Level: o.LogLevel}))
	db, err := store.Open(ctx, store.Options{Driver: cfg.Database.Driver, SQLitePath: cfg.Database.SQLitePath, URL: cfg.DatabaseURL})
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*components, error) { _ = db.Close(); return nil, err }

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
		log: log, db: db, sealer: sealer, sessions: mgr,
		users: store.NewUsers(db, o.Clock, store.NewID),
		audit: audit.NewRecorder(db, o.Clock, o.Stdout, store.NewID),
	}
	c.handler = httpapi.New(httpapi.Deps{
		Config: cfg, Clock: o.Clock, Log: log, WebFS: webui.FS(), Sessions: mgr, Users: c.users,
		Ready: func(ctx context.Context) error { return db.PingContext(ctx) },
	})
	return c, nil
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
	for _, loop := range []func(context.Context){c.sessions.RunWatchdog, c.sessions.RunSweep} {
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
