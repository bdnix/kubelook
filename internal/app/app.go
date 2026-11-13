// Package app dispatches the kubelook commands.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/version"
)

const usage = "usage: kubelook <serve|migrate|audit-cleanup|version>"

// Main is the whole program. args excludes the program name. It returns the exit code.
func Main(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	ctx, stop := signalContext()
	defer stop()
	return mainWithContext(ctx, args, stdout, stderr, getenv)
}

// signalContext is cancelled by SIGINT or SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func mainWithContext(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, version.String())
		return 0
	case "serve":
		return serve(ctx, stdout, stderr, getenv)
	case "migrate":
		return migrate(ctx, stdout, stderr, getenv)
	case "audit-cleanup":
		return auditCleanup(ctx, stdout, stderr, getenv)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}

const defaultConfigPath = "/etc/kubelook/config.yaml"

func loadConfig(getenv func(string) string) (*config.Config, error) {
	path := getenv("KUBELOOK_CONFIG")
	if path == "" {
		path = defaultConfigPath
	}
	return config.Load(path, getenv)
}

// auditCleanup deletes audit records older than audit.retentionDays. On PostgreSQL it
// connects with KUBELOOK_AUDIT_CLEANUP_DATABASE_URL, a role allowed to delete (A14, A15).
func auditCleanup(ctx context.Context, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg, err := loadConfig(getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "audit-cleanup: %v\n", err)
		return 1
	}
	opts := store.Options{Driver: cfg.Database.Driver, SQLitePath: cfg.Database.SQLitePath}
	if cfg.Database.Driver == "postgres" {
		if opts.URL = getenv("KUBELOOK_AUDIT_CLEANUP_DATABASE_URL"); opts.URL == "" {
			_, _ = fmt.Fprintln(stderr, "audit-cleanup: KUBELOOK_AUDIT_CLEANUP_DATABASE_URL is required with database.driver postgres")
			return 1
		}
	}
	db, err := store.Open(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "audit-cleanup: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()
	n, err := audit.Cleanup(ctx, db, clock.Real(), cfg.Audit.RetentionDays)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "audit-cleanup: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "audit-cleanup: deleted %d records\n", n)
	return 0
}

func parseLogLevel(s string) (slog.Level, error) {
	var l slog.Level
	if s == "" {
		return slog.LevelInfo, nil
	}
	if err := l.UnmarshalText([]byte(strings.ToLower(s))); err != nil {
		return 0, fmt.Errorf("KUBELOOK_LOG_LEVEL must be debug, info, warn or error, got %q", s)
	}
	return l, nil
}

func serve(ctx context.Context, stdout, stderr io.Writer, getenv func(string) string) int {
	level, err := parseLogLevel(getenv("KUBELOOK_LOG_LEVEL"))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	cfg, err := loadConfig(getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	if err := Serve(ctx, cfg, ServeOptions{Stdout: stdout, LogLevel: level}); err != nil {
		_, _ = fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	return 0
}

// migrate applies the embedded migrations. On PostgreSQL it connects with
// KUBELOOK_MIGRATE_DATABASE_URL, a role allowed to change the schema (A15).
func migrate(ctx context.Context, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg, err := loadConfig(getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	opts := store.Options{Driver: cfg.Database.Driver, SQLitePath: cfg.Database.SQLitePath}
	if cfg.Database.Driver == "postgres" {
		if opts.URL = getenv("KUBELOOK_MIGRATE_DATABASE_URL"); opts.URL == "" {
			_, _ = fmt.Fprintln(stderr, "migrate: KUBELOOK_MIGRATE_DATABASE_URL is required with database.driver postgres")
			return 1
		}
	}
	db, err := store.Open(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(ctx, db, store.Migrations()); err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "migrate: schema is up to date")
	return 0
}
