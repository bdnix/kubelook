// Package storetest provides migrated databases for tests.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/bdnix/kubelook/internal/store"
)

// New returns a migrated SQLite database in the test's temporary directory.
func New(t testing.TB) *store.DB {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, store.Options{Driver: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "kubelook.db")})
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db, store.Migrations()); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}
	return db
}
