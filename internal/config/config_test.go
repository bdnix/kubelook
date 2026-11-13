package config_test

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/config"
)

var testKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

func env(extra map[string]string) func(string) string {
	m := map[string]string{"KUBELOOK_ENCRYPTION_KEY": testKey}
	for k, v := range extra {
		if v == "" {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return func(k string) string { return m[k] }
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

const base = "server:\n  publicURL: https://k.example.com\n"

func TestLoad_Minimal_AppliesEveryDefault(t *testing.T) {
	c, err := config.Load("testdata/minimal.yaml", env(nil))
	require.NoError(t, err)

	assert.Equal(t, ":8080", c.Server.ListenAddress)
	assert.Equal(t, "https://kubelook.example.com", c.Server.PublicURL)
	assert.Equal(t, "sqlite", c.Database.Driver)
	assert.Equal(t, "/data/kubelook.db", c.Database.SQLitePath)
	assert.True(t, c.Database.MigrateOnStart())
	assert.Equal(t, 30*time.Minute, c.Session.IdleTimeout.D())
	assert.Equal(t, 12*time.Hour, c.Session.MaxLifetime.D())
	assert.True(t, c.Auth.Restrict())
	assert.Equal(t, "https://accounts.google.com", c.Auth.Google.IssuerURL)
	assert.Equal(t, "https://github.com", c.Auth.GitHub.BaseURL)
	assert.Equal(t, "https://api.github.com", c.Auth.GitHub.APIBaseURL)
	assert.Equal(t, "https://vault.example.com", c.Vault.Address)
	assert.Equal(t, "kubernetes/", c.Vault.KubernetesMountPrefix)
	assert.Equal(t, "itops", c.Vault.CredsNamespace)
	assert.True(t, c.Vault.ClusterRoleBinding())
	assert.Nil(t, c.Vault.CandidateRoles)
	assert.Equal(t, 30*time.Minute, c.Vault.TokenTTL.D())
	assert.Equal(t, 30*time.Minute, c.Vault.ExpiryWarning.D())
	assert.Equal(t, 5*time.Minute, c.Vault.RemintBefore.D())
	assert.True(t, c.Kubeconfig.IsEnabled())
	assert.True(t, c.FileBrowser.IsEnabled())
	assert.Equal(t, int64(26214400), c.FileBrowser.MaxTransferBytes)
	assert.Equal(t, 200000, c.Logs.HistoryMaxLines)
	assert.Equal(t, 10000, c.Logs.LiveBufferLines)
	assert.Equal(t, 2000, c.Logs.MaxLinesPerSecond)
	assert.Equal(t, 15*time.Minute, c.Shell.IdleTimeout.D())
	assert.False(t, c.NodeShell.IsEnabled())
	assert.Equal(t, "ghcr.io/bdnix/kubelook-netshoot:v0.16", c.Debug.Image)
	assert.Equal(t, c.Debug.Image, c.NodeShell.Image)
	assert.Equal(t, "/bin/bash", c.Debug.Shell)
	assert.Nil(t, c.Debug.Profiles)
	assert.Equal(t, 4, c.Helm.Version)
	assert.Equal(t, 5, c.Helm.MaxConcurrentJobs)
	assert.Equal(t, int64(10485760), c.Helm.MaxChartBytes)
	assert.Equal(t, 50, c.Watch.MaxPerUser)
	assert.Equal(t, 80, c.Memory.RefuseNewWorkPercent)
	assert.Equal(t, 0, c.Audit.RetentionDays)
	assert.Equal(t, "", c.Maintenance.Banner)
	for _, name := range []string{"karpenter", "keda", "externalSecrets", "vaultSecrets", "certManager", "gatewayApi", "trivy", "permissions"} {
		assert.Equal(t, "auto", c.Modules[name].Enabled, name)
	}
	assert.Empty(t, c.Prometheus.Clusters)
	assert.Equal(t, "", c.Telemetry.OTLPEndpoint)
	assert.Equal(t, make([]byte, 32), c.EncryptionKey)
}

func TestLoad_Full_EveryFieldEqualsFileValue(t *testing.T) {
	c, err := config.Load("testdata/full.yaml", env(map[string]string{
		"KUBELOOK_DATABASE_URL":         "postgres://u:p@h/db",
		"KUBELOOK_GOOGLE_CLIENT_SECRET": "gs",
		"KUBELOOK_GITHUB_CLIENT_SECRET": "ghs",
	}))
	require.NoError(t, err)

	assert.Equal(t, ":9090", c.Server.ListenAddress)
	assert.Equal(t, "postgres", c.Database.Driver)
	assert.Equal(t, "/var/lib/kubelook/k.db", c.Database.SQLitePath)
	assert.True(t, c.Database.MigrateOnStart())
	assert.Equal(t, 10*time.Minute, c.Session.IdleTimeout.D())
	assert.Equal(t, 2*time.Hour, c.Session.MaxLifetime.D())
	assert.False(t, c.Auth.Restrict())
	assert.Equal(t, "google-id", c.Auth.Google.ClientID)
	assert.Equal(t, []string{"example.org", "example.com"}, c.Auth.Google.AllowedDomains)
	assert.Equal(t, "https://idp.example.org", c.Auth.Google.IssuerURL)
	assert.Equal(t, "github-id", c.Auth.GitHub.ClientID)
	assert.Equal(t, []string{"acme", "widgets"}, c.Auth.GitHub.AllowedOrgs)
	assert.Equal(t, "https://ghe.example.org", c.Auth.GitHub.BaseURL)
	assert.Equal(t, "https://ghe.example.org/api/v3", c.Auth.GitHub.APIBaseURL)
	assert.Equal(t, "k8s/", c.Vault.KubernetesMountPrefix)
	assert.Equal(t, "ops", c.Vault.CredsNamespace)
	assert.False(t, c.Vault.ClusterRoleBinding())
	assert.Equal(t, []string{"viewer", "editor"}, c.Vault.CandidateRoles)
	assert.Equal(t, 45*time.Minute, c.Vault.TokenTTL.D())
	assert.Equal(t, 10*time.Minute, c.Vault.ExpiryWarning.D())
	assert.Equal(t, 2*time.Minute, c.Vault.RemintBefore.D())
	assert.False(t, c.Kubeconfig.IsEnabled())
	assert.False(t, c.FileBrowser.IsEnabled())
	assert.Equal(t, int64(1024), c.FileBrowser.MaxTransferBytes)
	assert.Equal(t, 100, c.Logs.HistoryMaxLines)
	assert.Equal(t, 200, c.Logs.LiveBufferLines)
	assert.Equal(t, 300, c.Logs.MaxLinesPerSecond)
	assert.Equal(t, 5*time.Minute, c.Shell.IdleTimeout.D())
	assert.True(t, c.NodeShell.IsEnabled())
	assert.Equal(t, "example.org/node-shell:1", c.NodeShell.Image)
	assert.Equal(t, "example.org/debug:2", c.Debug.Image)
	assert.Equal(t, "/bin/sh", c.Debug.Shell)
	assert.Equal(t, []config.DebugProfile{{Name: "net", Profile: "netadmin", ShareProcessNamespace: true}}, c.Debug.Profiles)
	assert.Equal(t, 3, c.Helm.Version)
	assert.Equal(t, 2, c.Helm.MaxConcurrentJobs)
	assert.Equal(t, int64(2048), c.Helm.MaxChartBytes)
	assert.Equal(t, []config.HelmRepo{{Name: "bitnami", URL: "https://charts.example.org"}}, c.Helm.Repositories)
	assert.Equal(t, 7, c.Watch.MaxPerUser)
	assert.Equal(t, 60, c.Memory.RefuseNewWorkPercent)
	assert.Equal(t, 90, c.Audit.RetentionDays)
	assert.Equal(t, "Down at noon", c.Maintenance.Banner)
	assert.Equal(t, "false", c.Modules["keda"].Enabled)
	assert.Equal(t, "auto", c.Modules["trivy"].Enabled)
	assert.Equal(t, "https://prom.example.org", c.Prometheus.Clusters["v:prod"].URL)
	assert.Equal(t, "otel.example.org:4317", c.Telemetry.OTLPEndpoint)
	assert.Equal(t, "postgres://u:p@h/db", c.DatabaseURL)
	assert.Equal(t, "gs", c.GoogleSecret)
	assert.Equal(t, "ghs", c.GitHubSecret)
}

func TestLoad_Errors(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	tests := []struct {
		name string
		body string
		env  map[string]string
		want string
	}{
		{"unknown key", base + "vault:\n  adress: x\n", nil, "unknown field"}, //nolint:misspell // the typo is the test input
		{"key missing", base, map[string]string{"KUBELOOK_ENCRYPTION_KEY": ""}, "KUBELOOK_ENCRYPTION_KEY is required"},
		{"key wrong length", base, map[string]string{"KUBELOOK_ENCRYPTION_KEY": short}, "must decode to 32 bytes"},
		{"key not base64", base, map[string]string{"KUBELOOK_ENCRYPTION_KEY": "!!!"}, "base64"},
		{"publicURL missing", "vault:\n  address: x\n", nil, "server.publicURL"},
		{"publicURL not http", "server:\n  publicURL: ftp://x\n", nil, "server.publicURL"},
		{"postgres without url", base + "database:\n  driver: postgres\n", nil, "KUBELOOK_DATABASE_URL"},
		{"bad driver", base + "database:\n  driver: mysql\n", nil, "database.driver"},
		{"helm version", base + "helm:\n  version: 5\n", nil, "helm.version"},
		{"google id without secret", base + "auth:\n  google:\n    clientId: x\n", nil, "KUBELOOK_GOOGLE_CLIENT_SECRET"},
		{"github id without secret", base + "auth:\n  github:\n    clientId: x\n", nil, "KUBELOOK_GITHUB_CLIENT_SECRET"},
		{"bad duration", base + "vault:\n  tokenTTL: abc\n", nil, "invalid duration"},
		{"unknown module", base + "modules:\n  nope:\n    enabled: auto\n", nil, "nope"},
		{"bad module value", base + "modules:\n  keda:\n    enabled: true\n", nil, "modules.keda.enabled"},
		{"bad module value string", base + "modules:\n  keda:\n    enabled: sometimes\n", nil, "modules.keda.enabled"},
		{"unknown module field", base + "modules:\n  keda:\n    enable: false\n", nil, "unknown field"},
		{"module not a mapping", base + "modules:\n  keda: 3\n", nil, "cannot unmarshal"},
		{"module enabled wrong type", base + "modules:\n  keda:\n    enabled: 3\n", nil, "modules.keda.enabled"},
		{"duration wrong type", base + "vault:\n  tokenTTL: 5\n", nil, "duration must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(write(t, tt.body), env(tt.env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoad_MissingFile_ReportsReadingConfig(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml"), env(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config")
}

func TestLoad_ReportsAllProblemsAtOnce(t *testing.T) {
	_, err := config.Load(write(t, "database:\n  driver: mysql\nhelm:\n  version: 9\n"), env(nil))
	require.Error(t, err)
	for _, want := range []string{"server.publicURL", "database.driver", "helm.version"} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestLoad_RestrictSignInWithoutLists_LoadsWithWarnings(t *testing.T) {
	body := base + "auth:\n  google:\n    clientId: x\n  github:\n    clientId: y\n"
	c, err := config.Load(write(t, body), env(map[string]string{
		"KUBELOOK_GOOGLE_CLIENT_SECRET": "s", "KUBELOOK_GITHUB_CLIENT_SECRET": "s",
	}))
	require.NoError(t, err)
	w := c.Warnings()
	require.Len(t, w, 2)
	assert.Contains(t, w[0], "auth.google.allowedDomains")
	assert.Contains(t, w[1], "auth.github.allowedOrgs")

	c, err = config.Load(write(t, body+"  restrictSignIn: false\n"), env(map[string]string{
		"KUBELOOK_GOOGLE_CLIENT_SECRET": "s", "KUBELOOK_GITHUB_CLIENT_SECRET": "s",
	}))
	require.NoError(t, err)
	assert.Empty(t, c.Warnings())
}

func TestLoad_Durations(t *testing.T) {
	c, err := config.Load(write(t, base+"vault:\n  tokenTTL: 45m\n"), env(nil))
	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, c.Vault.TokenTTL.D())
}

func TestLoad_MountPrefixIsNormalised(t *testing.T) {
	c, err := config.Load(write(t, base+"vault:\n  kubernetesMountPrefix: kubernetes\n"), env(nil))
	require.NoError(t, err)
	assert.Equal(t, "kubernetes/", c.Vault.KubernetesMountPrefix)
}

func TestLoad_ModuleDisabled(t *testing.T) {
	c, err := config.Load(write(t, base+"modules:\n  keda:\n    enabled: false\n"), env(nil))
	require.NoError(t, err)
	assert.Equal(t, "false", c.Modules["keda"].Enabled)
	assert.Equal(t, "auto", c.Modules["karpenter"].Enabled)
}

func TestMigrateOnStart_DefaultsByDriver(t *testing.T) {
	url := map[string]string{"KUBELOOK_DATABASE_URL": "postgres://x"}
	c, err := config.Load(write(t, base+"database:\n  driver: postgres\n"), env(url))
	require.NoError(t, err)
	assert.False(t, c.Database.MigrateOnStart())

	c, err = config.Load(write(t, base+"database:\n  driver: postgres\n  migrateOnStart: true\n"), env(url))
	require.NoError(t, err)
	assert.True(t, c.Database.MigrateOnStart())

	c, err = config.Load(write(t, base+"database:\n  migrateOnStart: false\n"), env(nil))
	require.NoError(t, err)
	assert.False(t, c.Database.MigrateOnStart())
}

func TestCookieSecure_FollowsPublicURLScheme(t *testing.T) {
	c, err := config.Load(write(t, base), env(nil))
	require.NoError(t, err)
	assert.True(t, c.CookieSecure())

	c, err = config.Load(write(t, "server:\n  publicURL: http://localhost:8080\n"), env(nil))
	require.NoError(t, err)
	assert.False(t, c.CookieSecure())
}

func TestLogValue_ContainsNoSecrets(t *testing.T) {
	c, err := config.Load("testdata/full.yaml", env(map[string]string{
		"KUBELOOK_ENCRYPTION_KEY":       base64.StdEncoding.EncodeToString([]byte(strings.Repeat("K", 32))),
		"KUBELOOK_DATABASE_URL":         "postgres://user:hunter2@h/db",
		"KUBELOOK_GOOGLE_CLIENT_SECRET": "google-secret-value",
		"KUBELOOK_GITHUB_CLIENT_SECRET": "github-secret-value",
	}))
	require.NoError(t, err)

	out := fmt.Sprintf("%v", c.LogValue())
	var sb strings.Builder
	logger := slog.New(slog.NewTextHandler(&sb, nil))
	logger.Info("loaded", "config", c)
	out += sb.String()

	assert.Contains(t, out, "kubelook.example.org")
	for _, secret := range []string{"hunter2", "google-secret-value", "github-secret-value", strings.Repeat("K", 32), base64.StdEncoding.EncodeToString([]byte(strings.Repeat("K", 32)))} {
		assert.NotContains(t, out, secret)
	}
}
