package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/bdnix/kubelook/internal/clock"
)

//go:embed migrations/*.sql
var embedded embed.FS

var (
	nameRE         = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)
	statementBreak = "-- statement-break"
)

// Migrations returns the embedded migration files.
func Migrations() fs.FS {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return sub
}

// Migrate applies every unapplied migration in name order, each in its own transaction.
func Migrate(ctx context.Context, db *DB, fsys fs.FS) error {
	if _, err := db.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at BIGINT NOT NULL)"); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}
	names, err := migrationNames(fsys)
	if err != nil {
		return err
	}
	applied, err := appliedSet(ctx, db)
	if err != nil {
		return err
	}
	for _, name := range names {
		if applied[name] {
			continue
		}
		if err := applyOne(ctx, db, fsys, name); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
	}
	return nil
}

func migrationNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if !nameRE.MatchString(e.Name()) {
			return nil, fmt.Errorf("migration file name %q does not match %s", e.Name(), nameRE)
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func appliedSet(ctx context.Context, db *DB) (map[string]bool, error) {
	rows, err := db.Query(ctx, "SELECT name FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	set := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("reading schema_migrations: %w", err)
		}
		set[n] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	return set, nil
}

func applyOne(ctx context.Context, db *DB, fsys fs.FS, name string) error {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	return db.InTx(ctx, func(tx *Tx) error {
		for _, stmt := range splitStatements(string(body)) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)", name, clock.Real().Now().UnixMilli())
		return err
	})
}

// splitStatements splits on lines that contain only the statement-break marker and
// drops statements that hold nothing but comments.
func splitStatements(body string) []string {
	var out []string
	var cur []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(cur, "\n"))
		cur = nil
		for _, l := range strings.Split(text, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "--") {
				out = append(out, text)
				return
			}
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == statementBreak {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}
