package app_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/app"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
)

func loadConfig(t *testing.T, extra string) *config.Config {
	t.Helper()
	body := "server:\n  publicURL: http://localhost\n  listenAddress: 127.0.0.1:0\ndatabase:\n  sqlitePath: " +
		filepath.Join(t.TempDir(), "k.db") + "\n" + extra
	cfg, err := config.Load(writeConfig(t, body), envFn(map[string]string{"KUBELOOK_ENCRYPTION_KEY": encKey}))
	require.NoError(t, err)
	return cfg
}

type running struct {
	addr   string
	result chan error
	ready  app.Ready
	cancel context.CancelFunc
}

// start runs Serve on a loopback listener and returns once it is ready.
func start(t *testing.T, cfg *config.Config, useListener bool) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	r := &running{result: make(chan error, 1), cancel: cancel}
	readyCh := make(chan app.Ready, 1)
	opts := app.ServeOptions{Stdout: io.Discard, OnReady: func(rd app.Ready) { readyCh <- rd }}
	if useListener {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		opts.Listener = ln
	}
	go func() { r.result <- app.Serve(ctx, cfg, opts) }()
	select {
	case r.ready = <-readyCh:
		r.addr = r.ready.Addr
	case err := <-r.result:
		t.Fatalf("Serve returned before it was ready: %v", err)
	}
	return r
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}
	resp, err := client.Get(url) //nolint:noctx // loopback request in a test
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServe_HealthReadyAndGracefulShutdown(t *testing.T) {
	r := start(t, loadConfig(t, ""), true)

	code, body := get(t, "http://"+r.addr+"/healthz")
	assert.Equal(t, 200, code)
	assert.JSONEq(t, `{"status":"ok"}`, body)
	code, _ = get(t, "http://"+r.addr+"/readyz")
	assert.Equal(t, 200, code)

	r.cancel()
	assert.NoError(t, testutil.Recv(t, r.result))
	_, err := net.DialTimeout("tcp", r.addr, time.Second)
	assert.Error(t, err, "the listener is closed")
}

func TestServe_NoListener_ListensOnConfiguredAddress(t *testing.T) {
	r := start(t, loadConfig(t, ""), false)
	code, _ := get(t, "http://"+r.addr+"/healthz")
	assert.Equal(t, 200, code)
	r.cancel()
	assert.NoError(t, testutil.Recv(t, r.result))
}

func TestServe_MigrateOnStartTrue_CreatesTables(t *testing.T) {
	cfg := loadConfig(t, "")
	r := start(t, cfg, true)
	r.cancel()
	require.NoError(t, testutil.Recv(t, r.result))

	db, err := store.Open(t.Context(), store.Options{Driver: "sqlite", SQLitePath: cfg.Database.SQLitePath})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var n int
	require.NoError(t, db.QueryRow(t.Context(), "SELECT COUNT(*) FROM users").Scan(&n))
	assert.Zero(t, n)
}

func TestServe_MigrateOnStartFalse_EmptyDatabase_Refuses(t *testing.T) {
	err := app.Serve(t.Context(), loadConfig(t, "  migrateOnStart: false\n"), app.ServeOptions{Stdout: io.Discard})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `database schema is not up to date; run "kubelook migrate"`)
}

func TestServe_MigrateOnStartFalse_MigratedDatabase_Starts(t *testing.T) {
	cfg := loadConfig(t, "  migrateOnStart: false\n")
	db, err := store.Open(t.Context(), store.Options{Driver: "sqlite", SQLitePath: cfg.Database.SQLitePath})
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), db, store.Migrations()))
	require.NoError(t, db.Close())

	r := start(t, cfg, true)
	r.cancel()
	assert.NoError(t, testutil.Recv(t, r.result))
}

func TestServe_Shutdown_CallsEverySessionRegistryCloser(t *testing.T) {
	r := start(t, loadConfig(t, ""), true)
	closed := make(chan string, 2)
	r.ready.Registry.Register("hash-1", "a", func() error { closed <- "a"; return nil })
	r.ready.Registry.Register("hash-2", "b", func() error { closed <- "b"; return nil })

	r.cancel()
	assert.NoError(t, testutil.Recv(t, r.result))
	got := []string{testutil.Recv(t, closed), testutil.Recv(t, closed)}
	assert.ElementsMatch(t, []string{"a", "b"}, got)
}

func TestServe_UnopenableDatabase_ReturnsError(t *testing.T) {
	cfg := loadConfig(t, "")
	cfg.Database.SQLitePath = "/nonexistent-dir/x/k.db"
	err := app.Serve(t.Context(), cfg, app.ServeOptions{Stdout: io.Discard})
	assert.Error(t, err)
}

func TestServe_UnbindableAddress_ReturnsError(t *testing.T) {
	cfg := loadConfig(t, "")
	cfg.Server.ListenAddress = "256.256.256.256:1"
	err := app.Serve(t.Context(), cfg, app.ServeOptions{Stdout: io.Discard})
	assert.Error(t, err)
}

func TestServe_LogsListeningAndConfigWarnings(t *testing.T) {
	body := "server:\n  publicURL: http://localhost\n  listenAddress: 127.0.0.1:0\ndatabase:\n  sqlitePath: " +
		filepath.Join(t.TempDir(), "k.db") + "\nauth:\n  google:\n    clientId: x\n"
	cfg, err := config.Load(writeConfig(t, body), envFn(map[string]string{
		"KUBELOOK_ENCRYPTION_KEY": encKey, "KUBELOOK_GOOGLE_CLIENT_SECRET": "s",
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out strings.Builder
	require.NoError(t, app.Serve(ctx, cfg, app.ServeOptions{Stdout: &out}))
	assert.Contains(t, out.String(), `"msg":"listening"`)
	assert.Contains(t, out.String(), "auth.google.allowedDomains")
	assert.NotContains(t, out.String(), encKey)
}

func TestMainWithContext_ServeMissingConfig_Exit1(t *testing.T) {
	var out, errb strings.Builder
	code := app.MainWithContext(t.Context(), []string{"serve"}, &out, &errb,
		envFn(map[string]string{"KUBELOOK_CONFIG": filepath.Join(t.TempDir(), "none.yaml")}))
	assert.Equal(t, 1, code)
	assert.Contains(t, errb.String(), "reading config")
}

func TestMainWithContext_ServeCancelledContext_ExitsZero(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\n  listenAddress: 127.0.0.1:0\ndatabase:\n  sqlitePath: "+
		filepath.Join(t.TempDir(), "k.db")+"\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, errb strings.Builder
	code := app.MainWithContext(ctx, []string{"serve"}, &out, &errb,
		envFn(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey}))
	assert.Equal(t, 0, code, errb.String())
	assert.Contains(t, out.String(), "listening")
}

func TestMainWithContext_ServeFailure_Exit1WithMessage(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\n  listenAddress: 127.0.0.1:0\ndatabase:\n  sqlitePath: /nonexistent-dir/x/k.db\n")
	var out, errb strings.Builder
	code := app.MainWithContext(t.Context(), []string{"serve"}, &out, &errb,
		envFn(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey}))
	assert.Equal(t, 1, code)
	assert.Contains(t, errb.String(), "serve:")
}

func TestMainWithContext_BadLogLevel_Exit1(t *testing.T) {
	var out, errb strings.Builder
	code := app.MainWithContext(t.Context(), []string{"serve"}, &out, &errb, envFn(map[string]string{"KUBELOOK_LOG_LEVEL": "loud"}))
	assert.Equal(t, 1, code)
	assert.Contains(t, errb.String(), "KUBELOOK_LOG_LEVEL")
}

func TestMainWithContext_LogLevels_Accepted(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "INFO"} {
		cfg := writeConfig(t, "server:\n  publicURL: http://localhost\n  listenAddress: 127.0.0.1:0\ndatabase:\n  sqlitePath: "+
			filepath.Join(t.TempDir(), "k.db")+"\n")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var out, errb strings.Builder
		code := app.MainWithContext(ctx, []string{"serve"}, &out, &errb,
			envFn(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey, "KUBELOOK_LOG_LEVEL": level}))
		assert.Equal(t, 0, code, level+": "+errb.String())
	}
}

func TestMainWithContext_Migrate_SQLite_PrintsUpToDate(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  sqlitePath: "+filepath.Join(t.TempDir(), "k.db")+"\n")
	env := envFn(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey})
	for i := 0; i < 2; i++ { // the second run changes nothing
		var out, errb strings.Builder
		code := app.MainWithContext(t.Context(), []string{"migrate"}, &out, &errb, env)
		assert.Equal(t, 0, code, errb.String())
		assert.Equal(t, "migrate: schema is up to date\n", out.String())
	}
}

func TestMainWithContext_Migrate_PostgresWithoutMigrateURL_Exit1(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  driver: postgres\n")
	var out, errb strings.Builder
	code := app.MainWithContext(t.Context(), []string{"migrate"}, &out, &errb, envFn(map[string]string{
		"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey, "KUBELOOK_DATABASE_URL": "postgres://x",
	}))
	assert.Equal(t, 1, code)
	assert.Contains(t, errb.String(), "KUBELOOK_MIGRATE_DATABASE_URL")
}

func TestMainWithContext_Migrate_MissingConfig_Exit1(t *testing.T) {
	var out, errb strings.Builder
	code := app.MainWithContext(t.Context(), []string{"migrate"}, &out, &errb,
		envFn(map[string]string{"KUBELOOK_CONFIG": filepath.Join(t.TempDir(), "none.yaml")}))
	assert.Equal(t, 1, code)
	assert.Contains(t, errb.String(), "reading config")
}

func TestMainWithContext_Migrate_UnopenableDatabase_Exit1(t *testing.T) {
	cfg := writeConfig(t, "server:\n  publicURL: http://localhost\ndatabase:\n  sqlitePath: /nonexistent-dir/x/k.db\n")
	var out, errb strings.Builder
	code := app.MainWithContext(t.Context(), []string{"migrate"}, &out, &errb,
		envFn(map[string]string{"KUBELOOK_CONFIG": cfg, "KUBELOOK_ENCRYPTION_KEY": encKey}))
	assert.Equal(t, 1, code)
	assert.Contains(t, errb.String(), "migrate:")
}

func TestSignalContext_StopCancelsTheContext(t *testing.T) {
	ctx, stop := app.SignalContext()
	assert.NoError(t, ctx.Err())
	stop()
	testutil.Recv(t, ctx.Done())
}
