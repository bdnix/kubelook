// Package app dispatches the kubelook commands.
package app

import (
	"context"
	"fmt"
	"io"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/version"
)

const usage = "usage: kubelook <serve|migrate|audit-cleanup|version>"

// Main is the whole program. args excludes the program name. It returns the exit code.
func Main(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, version.String())
		return 0
	case "audit-cleanup":
		return auditCleanup(context.Background(), stdout, stderr, getenv)
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
