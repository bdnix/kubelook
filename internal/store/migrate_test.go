package store_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

func count(t *testing.T, db *store.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))
	return n
}

func TestMigrate_AppliesInNameOrderExactlyOnce(t *testing.T) {
	ctx := t.Context()
	db := openSQLite(t)
	fsys := fstest.MapFS{
		"0002_b.sql": {Data: []byte("CREATE TABLE b (id TEXT PRIMARY KEY, a_id TEXT REFERENCES a(id))")},
		"0001_a.sql": {Data: []byte("CREATE TABLE a (id TEXT PRIMARY KEY)\n-- statement-break\nCREATE INDEX a_id ON a(id)")},
		"README.txt": {Data: []byte("ignored")},
	}
	require.NoError(t, store.Migrate(ctx, db, fsys))
	assert.Equal(t, 2, count(t, db, "schema_migrations"))
	assert.Equal(t, 0, count(t, db, "a"))
	assert.Equal(t, 0, count(t, db, "b"))

	require.NoError(t, store.Migrate(ctx, db, fsys))
	assert.Equal(t, 2, count(t, db, "schema_migrations"))

	fsys["0003_c.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE c (id TEXT PRIMARY KEY)")}
	require.NoError(t, store.Migrate(ctx, db, fsys))
	assert.Equal(t, 3, count(t, db, "schema_migrations"))
}

func TestMigrate_InvalidSQL_NamesFileAndRollsBackOnlyThatFile(t *testing.T) {
	ctx := t.Context()
	db := openSQLite(t)
	fsys := fstest.MapFS{
		"0001_a.sql":   {Data: []byte("CREATE TABLE a (id TEXT PRIMARY KEY)")},
		"0002_bad.sql": {Data: []byte("CREATE TABLE half (id TEXT PRIMARY KEY)\n-- statement-break\nTHIS IS NOT SQL")},
	}
	err := store.Migrate(ctx, db, fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0002_bad.sql")

	assert.Equal(t, 1, count(t, db, "schema_migrations"))
	assert.Equal(t, 0, count(t, db, "a"))
	_, err = db.Exec(ctx, "SELECT 1 FROM half")
	assert.Error(t, err, "the half-applied file must be rolled back")
}

func TestMigrate_BadFileName_ReturnsError(t *testing.T) {
	for _, name := range []string{"1_a.sql", "0001_A.sql", "0001-a.sql", "0001_.sql", "00001_a.sql"} {
		err := store.Migrate(t.Context(), openSQLite(t), fstest.MapFS{name: {Data: []byte("SELECT 1")}})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), name)
	}
}

func TestMigrate_CommentOnlyStatementsAreSkipped(t *testing.T) {
	db := openSQLite(t)
	fsys := fstest.MapFS{"0001_x.sql": {Data: []byte("-- nothing yet\n-- statement-break\n\n-- statement-break\n-- still nothing\n")}}
	require.NoError(t, store.Migrate(t.Context(), db, fsys))
	assert.Equal(t, 1, count(t, db, "schema_migrations"))
}

func TestMigrate_DirectoryNamedLikeMigration_IsSkipped(t *testing.T) {
	fsys := fstest.MapFS{"0001_dir.sql/inner": {Data: []byte("x")}} // a directory, not a migration
	require.NoError(t, store.Migrate(t.Context(), openSQLite(t), fsys))
}

func TestMigrations_EmbeddedApplyCleanlyToEmptyDatabase(t *testing.T) {
	db := openSQLite(t)
	require.NoError(t, store.Migrate(t.Context(), db, store.Migrations()))
	assert.GreaterOrEqual(t, count(t, db, "schema_migrations"), 1)
}

func TestStoretestNew_ReturnsMigratedDatabase(t *testing.T) {
	db := storetest.New(t)
	assert.Equal(t, store.SQLite, db.Dialect())
	assert.GreaterOrEqual(t, count(t, db, "schema_migrations"), 1)
}

func TestPending_ListsUnappliedMigrations(t *testing.T) {
	ctx := t.Context()
	db := openSQLite(t)
	fsys := fstest.MapFS{
		"0001_a.sql": {Data: []byte("CREATE TABLE a (id TEXT PRIMARY KEY)")},
		"0002_b.sql": {Data: []byte("CREATE TABLE b (id TEXT PRIMARY KEY)")},
	}

	got, err := store.Pending(ctx, db, fsys)
	require.NoError(t, err)
	assert.Equal(t, []string{"0001_a.sql", "0002_b.sql"}, got, "no schema_migrations table yet: everything is pending")

	require.NoError(t, store.Migrate(ctx, db, fsys))
	got, err = store.Pending(ctx, db, fsys)
	require.NoError(t, err)
	assert.Empty(t, got)

	fsys["0003_c.sql"] = &fstest.MapFile{Data: []byte("SELECT 1")}
	got, err = store.Pending(ctx, db, fsys)
	require.NoError(t, err)
	assert.Equal(t, []string{"0003_c.sql"}, got)
}

func TestPending_DatabaseUnreachable_ReturnsError(t *testing.T) {
	db := openSQLite(t)
	require.NoError(t, db.Close())
	_, err := store.Pending(t.Context(), db, store.Migrations())
	assert.Error(t, err)
}

func TestPending_BadFileName_ReturnsError(t *testing.T) {
	_, err := store.Pending(t.Context(), openSQLite(t), fstest.MapFS{"bad.sql": {Data: []byte("x")}})
	assert.Error(t, err)
}
