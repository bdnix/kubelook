package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/store"
)

func openSQLite(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), store.Options{Driver: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "k.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func pragma(t *testing.T, db *store.DB, name string) string {
	t.Helper()
	var v string
	require.NoError(t, db.QueryRow(t.Context(), "PRAGMA "+name).Scan(&v))
	return v
}

func TestOpen_SQLite_EnablesForeignKeysAndWAL(t *testing.T) {
	db := openSQLite(t)
	assert.Equal(t, store.SQLite, db.Dialect())
	assert.Equal(t, "1", pragma(t, db, "foreign_keys"))
	assert.Equal(t, "wal", pragma(t, db, "journal_mode"))
	assert.Equal(t, "5000", pragma(t, db, "busy_timeout"))
	assert.Equal(t, 1, db.Stats().MaxOpenConnections)
}

func TestOpen_SQLite_UnwritablePath_ReturnsError(t *testing.T) {
	_, err := store.Open(t.Context(), store.Options{Driver: "sqlite", SQLitePath: "/nonexistent-dir/x/k.db"})
	assert.Error(t, err)
}

func TestOpen_Postgres_DoesNotConnect(t *testing.T) {
	db, err := store.Open(t.Context(), store.Options{Driver: "postgres", URL: "postgres://u:p@127.0.0.1:1/db"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assert.Equal(t, store.Postgres, db.Dialect())
	assert.Equal(t, 20, db.Stats().MaxOpenConnections)
}

func TestOpen_UnknownDriver_ReturnsError(t *testing.T) {
	_, err := store.Open(t.Context(), store.Options{Driver: "mysql"})
	assert.ErrorContains(t, err, "mysql")
}

func TestRebind(t *testing.T) {
	tests := []struct {
		name    string
		dialect store.Dialect
		in, out string
	}{
		{"sqlite unchanged", store.SQLite, "a=? AND b=?", "a=? AND b=?"},
		{"postgres numbered", store.Postgres, "a=? AND b=?", "a=$1 AND b=$2"},
		{"quoted question mark untouched", store.Postgres, "a='?' AND b=? AND c='it''s ?' AND d=?", "a='?' AND b=$1 AND c='it''s ?' AND d=$2"},
		{"no placeholders", store.Postgres, "SELECT 1", "SELECT 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.out, store.Rebind(tt.dialect, tt.in))
		})
	}
}

func TestDB_ExecQueryAndQueryRow(t *testing.T) {
	ctx := t.Context()
	db := openSQLite(t)
	_, err := db.Exec(ctx, "CREATE TABLE t (id TEXT PRIMARY KEY, n BIGINT)")
	require.NoError(t, err)
	_, err = db.Exec(ctx, "INSERT INTO t VALUES (?, ?), (?, ?)", "a", 1, "b", 2)
	require.NoError(t, err)

	rows, err := db.Query(ctx, "SELECT id FROM t WHERE n >= ? ORDER BY id", 1)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"a", "b"}, ids)

	var n int
	require.NoError(t, db.QueryRow(ctx, "SELECT n FROM t WHERE id = ?", "b").Scan(&n))
	assert.Equal(t, 2, n)
}

func TestInTx_CommitsOnNilAndRollsBackOnError(t *testing.T) {
	ctx := t.Context()
	db := openSQLite(t)
	_, err := db.Exec(ctx, "CREATE TABLE t (id TEXT PRIMARY KEY)")
	require.NoError(t, err)

	require.NoError(t, db.InTx(ctx, func(tx *store.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO t VALUES (?)", "kept")
		return err
	}))

	boom := errors.New("boom")
	err = db.InTx(ctx, func(tx *store.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO t VALUES (?)", "dropped"); err != nil {
			return err
		}
		var n int
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM t").Scan(&n))
		assert.Equal(t, 2, n)
		rows, err := tx.Query(ctx, "SELECT id FROM t")
		require.NoError(t, err)
		require.NoError(t, rows.Close())
		return boom
	})
	assert.ErrorIs(t, err, boom)

	var n int
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM t").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestInTx_CancelledContext_ReturnsError(t *testing.T) {
	db := openSQLite(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Error(t, db.InTx(ctx, func(*store.Tx) error { return nil }))
}

func TestInTx_Panic_RollsBackAndRepanics(t *testing.T) {
	ctx := t.Context()
	db := openSQLite(t)
	_, err := db.Exec(ctx, "CREATE TABLE t (id TEXT PRIMARY KEY)")
	require.NoError(t, err)
	assert.PanicsWithValue(t, "oops", func() {
		_ = db.InTx(ctx, func(tx *store.Tx) error {
			_, _ = tx.Exec(ctx, "INSERT INTO t VALUES ('x')")
			panic("oops")
		})
	})
	var n int
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM t").Scan(&n))
	assert.Equal(t, 0, n)
}
