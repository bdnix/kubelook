// Package store is the persistence layer: SQLite or PostgreSQL through database/sql.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	_ "modernc.org/sqlite"             // registers the "sqlite" driver
)

// Dialect identifies the SQL flavour in use.
type Dialect int

// Supported dialects.
const (
	SQLite Dialect = iota
	Postgres
)

// Options selects and locates the database.
type Options struct{ Driver, SQLitePath, URL string }

// DB wraps *sql.DB, rebinding placeholders for the active dialect.
type DB struct {
	*sql.DB
	dialect Dialect
}

// Open opens the database. SQLite is created and checked immediately; PostgreSQL
// connects lazily on first use.
func Open(ctx context.Context, o Options) (*DB, error) {
	switch o.Driver {
	case "sqlite":
		return openSQLite(ctx, o.SQLitePath)
	case "postgres":
		sdb, err := sql.Open("pgx", o.URL)
		if err != nil {
			return nil, fmt.Errorf("opening postgres: %w", err)
		}
		sdb.SetMaxOpenConns(20)
		return &DB{DB: sdb, dialect: Postgres}, nil
	default:
		return nil, fmt.Errorf("unknown database driver %q", o.Driver)
	}
}

func openSQLite(ctx context.Context, path string) (*DB, error) {
	esc := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	dsn := "file:" + esc + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	sdb.SetMaxOpenConns(1) // single writer: avoids SQLITE_BUSY
	if err := sdb.PingContext(ctx); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("opening sqlite %s: %w", path, err)
	}
	return &DB{DB: sdb, dialect: SQLite}, nil
}

// Dialect returns the active dialect.
func (d *DB) Dialect() Dialect { return d.dialect }

// runner is what *sql.DB and *sql.Tx share.
type runner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// bound runs statements on a runner after rebinding placeholders for its dialect.
type bound struct {
	r       runner
	dialect Dialect
}

// Exec rebinds and executes a statement.
func (b bound) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return b.r.ExecContext(ctx, Rebind(b.dialect, query), args...)
}

// Query rebinds and runs a query. Callers must close the rows and check rows.Err.
func (b bound) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return b.r.QueryContext(ctx, Rebind(b.dialect, query), args...)
}

// QueryRow rebinds and runs a query expected to return one row.
func (b bound) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return b.r.QueryRowContext(ctx, Rebind(b.dialect, query), args...)
}

// Exec rebinds and executes a statement.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return bound{d.DB, d.dialect}.Exec(ctx, query, args...)
}

// Query rebinds and runs a query. Callers must close the rows and check rows.Err.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return bound{d.DB, d.dialect}.Query(ctx, query, args...)
}

// QueryRow rebinds and runs a query expected to return one row.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return bound{d.DB, d.dialect}.QueryRow(ctx, query, args...)
}

// Tx is a transaction with the same rebinding helpers as DB.
type Tx struct{ bound }

// InTx runs fn in a transaction: commit when it returns nil, roll back on error or panic.
func (d *DB) InTx(ctx context.Context, fn func(tx *Tx) error) (err error) {
	sqlTx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = sqlTx.Rollback()
			panic(p)
		}
	}()
	if err := fn(&Tx{bound{sqlTx, d.dialect}}); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// Rebind converts ? placeholders to $1, $2, … for PostgreSQL. Question marks inside
// single-quoted strings are left alone. SQLite queries are returned unchanged.
func Rebind(d Dialect, query string) string {
	if d != Postgres {
		return query
	}
	var b strings.Builder
	inQuote := false
	n := 0
	for i := 0; i < len(query); i++ {
		switch c := query[i]; {
		case c == '\'':
			inQuote = !inQuote // a doubled '' toggles twice
			b.WriteByte(c)
		case c == '?' && !inQuote:
			n++
			b.WriteString("$" + strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
