package app_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/app"
	"github.com/bdnix/kubelook/internal/store"
)

var encKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

func envFn(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func runWith(env map[string]string, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = app.Main(args, &out, &errb, envFn(env))
	return code, out.String(), errb.String()
}

func TestMain_AuditCleanup_SQLite_DeletesOldRecordsAndReports(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "k.db")
	db, err := store.Open(t.Context(), store.Options{Driver: "sqlite", SQLitePath: dbPath})
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), db, store.Migrations()))
	for id, ts := range map[string]int64{"old-1": 0, "old-2": 1000} { // 1970: far older than any retention
		_, err = db.Exec(t.Context(), `INSERT INTO audit_events VALUES (?, ?, '', '', '', '', '', '', '', '', '', 'x', '', '', 'success', '{}')`, id, ts)
		require.NoError(t, err)
	}
	require.NoError(t, db.Close())

	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  sqlitePath: "+dbPath+"\naudit:\n  retentionDays: 7\n")
	code, out, errOut := runWith(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey}, "audit-cleanup")
	assert.Equal(t, 0, code, errOut)
	assert.Equal(t, "audit-cleanup: deleted 2 records\n", out)
}

func TestMain_AuditCleanup_RetentionZero_DeletesNothing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "k.db")
	db, err := store.Open(t.Context(), store.Options{Driver: "sqlite", SQLitePath: dbPath})
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), db, store.Migrations()))
	require.NoError(t, db.Close())

	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  sqlitePath: "+dbPath+"\n")
	code, out, _ := runWith(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey}, "audit-cleanup")
	assert.Equal(t, 0, code)
	assert.Equal(t, "audit-cleanup: deleted 0 records\n", out)
}

func TestMain_AuditCleanup_PostgresWithoutCleanupURL_ExitsWithMessage(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  driver: postgres\n")
	code, _, errOut := runWith(map[string]string{
		"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey, "KUBELOOK_DATABASE_URL": "postgres://x",
	}, "audit-cleanup")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "KUBELOOK_AUDIT_CLEANUP_DATABASE_URL")
}

func TestMain_AuditCleanup_MissingConfig_Exit1(t *testing.T) {
	code, _, errOut := runWith(map[string]string{"KUBELOOK_CONFIG": filepath.Join(t.TempDir(), "none.yaml")}, "audit-cleanup")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "reading config")
}

func TestMain_AuditCleanup_UnmigratedDatabase_Exit1(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  sqlitePath: "+filepath.Join(t.TempDir(), "empty.db")+"\naudit:\n  retentionDays: 7\n")
	code, _, errOut := runWith(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey}, "audit-cleanup")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "audit-cleanup:")
}
