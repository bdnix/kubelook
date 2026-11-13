// Package config loads and validates the Kubelook configuration: a YAML file plus
// a fixed set of environment variables for secrets.
package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Environment variable names (A5), not values. Secrets are never read from the file.
//
//nolint:gosec // G101 matches the names, which are not credentials
const (
	EnvEncryptionKey = "KUBELOOK_ENCRYPTION_KEY"
	EnvDatabaseURL   = "KUBELOOK_DATABASE_URL"
	EnvGoogleSecret  = "KUBELOOK_GOOGLE_CLIENT_SECRET"
	EnvGitHubSecret  = "KUBELOOK_GITHUB_CLIENT_SECRET"
)

// Duration wraps time.Duration so it can be written as "45m" in the file.
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalJSON parses a Go duration string.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string such as \"30m\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// Config is the whole configuration.
type Config struct {
	Server      Server            `json:"server"`
	Database    Database          `json:"database"`
	Session     Session           `json:"session"`
	Auth        Auth              `json:"auth"`
	Vault       Vault             `json:"vault"`
	Kubeconfig  Toggle            `json:"kubeconfig"`
	FileBrowser FileBrowser       `json:"fileBrowser"`
	Logs        Logs              `json:"logs"`
	Shell       Shell             `json:"shell"`
	NodeShell   NodeShell         `json:"nodeShell"`
	Debug       Debug             `json:"debug"`
	Helm        Helm              `json:"helm"`
	Watch       Watch             `json:"watch"`
	Memory      Memory            `json:"memory"`
	Audit       Audit             `json:"audit"`
	Maintenance Maintenance       `json:"maintenance"`
	Modules     map[string]Module `json:"modules"`
	Prometheus  Prometheus        `json:"prometheus"`
	Telemetry   Telemetry         `json:"telemetry"`

	// From the environment only; never serialised.
	EncryptionKey []byte `json:"-"`
	DatabaseURL   string `json:"-"`
	GoogleSecret  string `json:"-"`
	GitHubSecret  string `json:"-"`

	warnings []string
}

// Server holds listener settings.
type Server struct {
	ListenAddress string `json:"listenAddress"`
	PublicURL     string `json:"publicURL"`
}

// Database selects and locates the store.
type Database struct {
	Driver            string `json:"driver"`
	SQLitePath        string `json:"sqlitePath"`
	MigrateOnStartRaw *bool  `json:"migrateOnStart"`
}

// MigrateOnStart reports whether migrations run when the server starts.
func (d Database) MigrateOnStart() bool {
	if d.MigrateOnStartRaw != nil {
		return *d.MigrateOnStartRaw
	}
	return d.Driver == "sqlite"
}

// Session holds session lifetimes.
type Session struct {
	IdleTimeout Duration `json:"idleTimeout"`
	MaxLifetime Duration `json:"maxLifetime"`
}

// Auth holds sign-in settings.
type Auth struct {
	RestrictSignIn *bool  `json:"restrictSignIn"`
	Google         Google `json:"google"`
	GitHub         GitHub `json:"github"`
}

// Restrict reports whether sign-in is limited to the allow lists.
func (a Auth) Restrict() bool { return a.RestrictSignIn == nil || *a.RestrictSignIn }

// Google configures Google sign-in.
type Google struct {
	ClientID       string   `json:"clientId"`
	AllowedDomains []string `json:"allowedDomains"`
	IssuerURL      string   `json:"issuerURL"`
}

// GitHub configures GitHub sign-in.
type GitHub struct {
	ClientID    string   `json:"clientId"`
	AllowedOrgs []string `json:"allowedOrgs"`
	BaseURL     string   `json:"baseURL"`
	APIBaseURL  string   `json:"apiBaseURL"`
}

// Vault configures the Vault integration.
type Vault struct {
	Address               string   `json:"address"`
	KubernetesMountPrefix string   `json:"kubernetesMountPrefix"`
	CredsNamespace        string   `json:"credsNamespace"`
	ClusterRoleBindingRaw *bool    `json:"clusterRoleBinding"`
	CandidateRoles        []string `json:"candidateRoles"`
	TokenTTL              Duration `json:"tokenTTL"`
	ExpiryWarning         Duration `json:"expiryWarning"`
	RemintBefore          Duration `json:"remintBefore"`
}

// ClusterRoleBinding reports whether Vault creates cluster role bindings.
func (v Vault) ClusterRoleBinding() bool {
	return v.ClusterRoleBindingRaw == nil || *v.ClusterRoleBindingRaw
}

// Toggle is a feature switch that defaults to on.
type Toggle struct {
	Enabled *bool `json:"enabled"`
}

// IsEnabled reports whether the feature is on.
func (t Toggle) IsEnabled() bool { return t.Enabled == nil || *t.Enabled }

// FileBrowser configures the pod file browser.
type FileBrowser struct {
	Toggle
	MaxTransferBytes int64 `json:"maxTransferBytes"`
}

// Logs configures log viewing.
type Logs struct {
	HistoryMaxLines   int `json:"historyMaxLines"`
	LiveBufferLines   int `json:"liveBufferLines"`
	MaxLinesPerSecond int `json:"maxLinesPerSecond"`
}

// Shell configures pod shells.
type Shell struct {
	IdleTimeout Duration `json:"idleTimeout"`
}

// NodeShell configures node shells (off by default).
type NodeShell struct {
	Enabled *bool  `json:"enabled"`
	Image   string `json:"image"`
}

// IsEnabled reports whether node shells are allowed.
func (n NodeShell) IsEnabled() bool { return n.Enabled != nil && *n.Enabled }

// DebugProfile is a named ephemeral-container profile.
type DebugProfile struct {
	Name                  string `json:"name"`
	Profile               string `json:"profile"`
	ShareProcessNamespace bool   `json:"shareProcessNamespace"`
}

// Debug configures the debug toolkit.
type Debug struct {
	Image    string         `json:"image"`
	Shell    string         `json:"shell"`
	Profiles []DebugProfile `json:"profiles"`
}

// HelmRepo is an organisation-wide chart repository.
type HelmRepo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Helm configures the Helm module.
type Helm struct {
	Version           int        `json:"version"`
	MaxConcurrentJobs int        `json:"maxConcurrentJobs"`
	MaxChartBytes     int64      `json:"maxChartBytes"`
	Repositories      []HelmRepo `json:"repositories"`
}

// Watch limits watches.
type Watch struct {
	MaxPerUser int `json:"maxPerUser"`
}

// Memory configures the memory guard.
type Memory struct {
	RefuseNewWorkPercent int `json:"refuseNewWorkPercent"`
}

// Audit configures audit retention.
type Audit struct {
	RetentionDays int `json:"retentionDays"`
}

// Maintenance holds the maintenance banner.
type Maintenance struct {
	Banner string `json:"banner"`
}

// Module is the switch for one ecosystem module: "auto" or "false".
type Module struct {
	Enabled string `json:"enabled"`
}

// UnmarshalJSON accepts `enabled: auto` and `enabled: false`.
func (m *Module) UnmarshalJSON(b []byte) error {
	var aux struct {
		Enabled json.RawMessage `json:"enabled"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&aux); err != nil {
		return err
	}
	if len(aux.Enabled) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(aux.Enabled, &s) == nil {
		m.Enabled = s
		return nil
	}
	var v bool
	if err := json.Unmarshal(aux.Enabled, &v); err != nil {
		return fmt.Errorf("enabled must be \"auto\" or false: %w", err)
	}
	m.Enabled = fmt.Sprint(v)
	return nil
}

// ModuleNames lists the modules the configuration knows about.
var ModuleNames = []string{"karpenter", "keda", "externalSecrets", "vaultSecrets", "certManager", "gatewayApi", "trivy", "permissions"}

// PrometheusCluster is one cluster's Prometheus endpoint.
type PrometheusCluster struct {
	URL string `json:"url"`
}

// Prometheus maps cluster IDs to Prometheus endpoints.
type Prometheus struct {
	Clusters map[string]PrometheusCluster `json:"clusters"`
}

// Telemetry configures OpenTelemetry export.
type Telemetry struct {
	OTLPEndpoint string `json:"otlpEndpoint"`
}

// Load reads the file at path, applies defaults, reads the secrets from getenv and validates.
func Load(path string, getenv func(string) string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is operator-supplied configuration
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	c := &Config{}
	if err := yaml.UnmarshalStrict(raw, c); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.readEnv(getenv); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func orDefault[T comparable](v *T, d T) {
	var zero T
	if *v == zero {
		*v = d
	}
}

func (c *Config) applyDefaults() {
	orDefault(&c.Server.ListenAddress, ":8080")
	orDefault(&c.Database.Driver, "sqlite")
	orDefault(&c.Database.SQLitePath, "/data/kubelook.db")
	orDefault(&c.Session.IdleTimeout, Duration(30*time.Minute))
	orDefault(&c.Session.MaxLifetime, Duration(12*time.Hour))
	orDefault(&c.Auth.Google.IssuerURL, "https://accounts.google.com")
	orDefault(&c.Auth.GitHub.BaseURL, "https://github.com")
	orDefault(&c.Auth.GitHub.APIBaseURL, "https://api.github.com")
	orDefault(&c.Vault.KubernetesMountPrefix, "kubernetes/")
	if !strings.HasSuffix(c.Vault.KubernetesMountPrefix, "/") {
		c.Vault.KubernetesMountPrefix += "/"
	}
	orDefault(&c.Vault.CredsNamespace, "itops")
	orDefault(&c.Vault.TokenTTL, Duration(30*time.Minute))
	orDefault(&c.Vault.ExpiryWarning, Duration(30*time.Minute))
	orDefault(&c.Vault.RemintBefore, Duration(5*time.Minute))
	orDefault(&c.FileBrowser.MaxTransferBytes, 26214400)
	orDefault(&c.Logs.HistoryMaxLines, 200000)
	orDefault(&c.Logs.LiveBufferLines, 10000)
	orDefault(&c.Logs.MaxLinesPerSecond, 2000)
	orDefault(&c.Shell.IdleTimeout, Duration(15*time.Minute))
	orDefault(&c.Debug.Image, "ghcr.io/bdnix/kubelook-netshoot:v0.16")
	orDefault(&c.Debug.Shell, "/bin/bash")
	orDefault(&c.NodeShell.Image, c.Debug.Image)
	orDefault(&c.Helm.Version, 4)
	orDefault(&c.Helm.MaxConcurrentJobs, 5)
	orDefault(&c.Helm.MaxChartBytes, 10485760)
	orDefault(&c.Watch.MaxPerUser, 50)
	orDefault(&c.Memory.RefuseNewWorkPercent, 80)
	if c.Modules == nil {
		c.Modules = map[string]Module{}
	}
	for _, n := range ModuleNames {
		m := c.Modules[n]
		orDefault(&m.Enabled, "auto")
		c.Modules[n] = m
	}
}

func (c *Config) readEnv(getenv func(string) string) error {
	c.DatabaseURL = getenv(EnvDatabaseURL)
	c.GoogleSecret = getenv(EnvGoogleSecret)
	c.GitHubSecret = getenv(EnvGitHubSecret)
	enc := getenv(EnvEncryptionKey)
	if enc == "" {
		return nil // reported by validate
	}
	key, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return fmt.Errorf("%s is not valid base64: %w", EnvEncryptionKey, err)
	}
	c.EncryptionKey = key
	return nil
}

func (c *Config) validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if u, err := url.Parse(c.Server.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		add("server.publicURL must be an http or https URL")
	}
	switch c.Database.Driver {
	case "sqlite":
	case "postgres":
		if c.DatabaseURL == "" {
			add("database.driver is postgres: %s is required", EnvDatabaseURL)
		}
	default:
		add("database.driver must be sqlite or postgres, got %q", c.Database.Driver)
	}
	switch {
	case len(c.EncryptionKey) == 0:
		add("%s is required", EnvEncryptionKey)
	case len(c.EncryptionKey) != 32:
		add("%s must decode to 32 bytes, got %d", EnvEncryptionKey, len(c.EncryptionKey))
	}
	if c.Helm.Version != 3 && c.Helm.Version != 4 {
		add("helm.version must be 3 or 4, got %d", c.Helm.Version)
	}
	if c.Auth.Google.ClientID != "" && c.GoogleSecret == "" {
		add("auth.google.clientId is set: %s is required", EnvGoogleSecret)
	}
	if c.Auth.GitHub.ClientID != "" && c.GitHubSecret == "" {
		add("auth.github.clientId is set: %s is required", EnvGitHubSecret)
	}
	known := map[string]bool{}
	for _, n := range ModuleNames {
		known[n] = true
	}
	for name, m := range c.Modules {
		if !known[name] {
			add("modules.%s: unknown module", name)
		}
		if m.Enabled != "auto" && m.Enabled != "false" {
			add("modules.%s.enabled must be auto or false, got %q", name, m.Enabled)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	c.warnings = nil
	if c.Auth.Restrict() {
		if c.Auth.Google.ClientID != "" && len(c.Auth.Google.AllowedDomains) == 0 {
			c.warnings = append(c.warnings, "auth.restrictSignIn is true but auth.google.allowedDomains is empty: Google sign-in is refused until a domain is listed")
		}
		if c.Auth.GitHub.ClientID != "" && len(c.Auth.GitHub.AllowedOrgs) == 0 {
			c.warnings = append(c.warnings, "auth.restrictSignIn is true but auth.github.allowedOrgs is empty: GitHub sign-in is refused until an organisation is listed")
		}
	}
	return nil
}

// Warnings lists non-fatal configuration problems.
func (c *Config) Warnings() []string { return append([]string(nil), c.warnings...) }

// CookieSecure reports whether the session cookie gets the Secure attribute (A8).
func (c *Config) CookieSecure() bool { return strings.HasPrefix(c.Server.PublicURL, "https://") }

// LogValue implements slog.LogValuer and exposes only non-secret fields.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("listenAddress", c.Server.ListenAddress),
		slog.String("publicURL", c.Server.PublicURL),
		slog.String("databaseDriver", c.Database.Driver),
		slog.Bool("vaultConfigured", c.Vault.Address != ""),
		slog.Bool("googleConfigured", c.Auth.Google.ClientID != ""),
		slog.Bool("githubConfigured", c.Auth.GitHub.ClientID != ""),
		slog.Int("helmVersion", c.Helm.Version),
	)
}
