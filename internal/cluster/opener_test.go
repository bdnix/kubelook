package cluster_test

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakevault"
	"github.com/bdnix/kubelook/internal/vault"
)

const (
	apiHost  = "https://api.prod.example:6443"
	credsMsg = "a ClusterRoleBinding cannot ref a Role"
)

// credsSignal tells a test that a Creds call finished.
type credsSignal struct {
	vault.API
	done chan struct{}
}

func (c credsSignal) Creds(ctx context.Context, token, mount, role string, req vault.CredsRequest) (vault.Creds, error) {
	defer func() {
		select {
		case c.done <- struct{}{}:
		default:
		}
	}()
	return c.API.Creds(ctx, token, mount, role, req)
}

type openerEnv struct {
	*env
	op     *cluster.Opener
	signal chan struct{}
}

func newOpenerEnv(t *testing.T) *openerEnv {
	t.Helper()
	e := newEnv(t)
	e.cfg.Vault.CredsNamespace = "itops"
	e.cfg.Vault.TokenTTL = config.Duration(30 * time.Minute)
	e.cfg.Vault.RemintBefore = config.Duration(5 * time.Minute)
	e.cfg.Vault.CandidateRoles = []string{"devops-role", "ops-role"}
	e.cfg.Session.IdleTimeout = config.Duration(30 * time.Minute)
	e.vault.AddToken(vaultTok, fakevault.Token{
		TTLSeconds: 7200, Renewable: true, EntityID: "ent-1", DisplayName: "oidc-ada",
		Caps: map[string][]string{
			"kubernetes/prod/config": {"read"}, "kubernetes/prod/creds/devops-role": {"update"}, "kubernetes/prod/creds/ops-role": {"update"},
		},
	})
	e.vault.SetConfig("kubernetes/prod", fakevault.ClusterConfig{Host: apiHost, CACert: "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----\n"})
	e.vault.SetCreds("kubernetes/prod", "devops-role", fakevault.Creds{Token: "sa-token-A", ServiceAccount: "sa-1", Namespace: "itops", LeaseID: "lease-A", LeaseDurationSeconds: 1800})
	e.vault.SetCreds("kubernetes/prod", "ops-role", fakevault.Creds{Token: "sa-token-ops", ServiceAccount: "sa-2", Namespace: "itops", LeaseID: "lease-ops", LeaseDurationSeconds: 1800})
	e.add(t)
	sig := make(chan struct{}, 64)
	vc := credsSignal{API: e.vaultClient(), done: sig}
	e.reg = cluster.NewRegistry(cluster.RegistryDeps{Credentials: e.c, Vault: vc, Sessions: store.NewSessions(e.db), Cache: e.cache, Config: e.cfg, Log: e.logger()})
	op := cluster.NewOpener(cluster.OpenerDeps{
		Registry: e.reg, Credentials: e.c, Sessions: store.NewSessions(e.db), Sealer: e.sealer, Vault: vc, Clock: e.clock,
		Config: e.cfg, Audit: audit.NewRecorder(e.db, e.clock, e.auditOut, func() string { return "a" }),
		Closers: e.sessions.Registry(), Log: e.logger(),
	})
	return &openerEnv{env: e, op: op, signal: sig}
}

func TestOpen_VaultCluster_MintsAndStoresSealedCredential(t *testing.T) {
	e := newOpenerEnv(t)
	st, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	assert.Equal(t, "open", st.State)
	assert.Equal(t, apiHost, st.Server)
	assert.Equal(t, "devops-role", st.Role)
	require.NotNil(t, st.CredentialExpiresAt)
	assert.Equal(t, e.clock.Now().Add(30*time.Minute).UTC(), st.CredentialExpiresAt.UTC())

	reqs := e.vault.CredsRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "itops", reqs[0]["kubernetes_namespace"])
	assert.Equal(t, true, reqs[0]["cluster_role_binding"])
	assert.Equal(t, "30m0s", reqs[0]["ttl"])

	assert.Equal(t, 1, e.count(t, "cluster_cred"))
	row, err := store.NewSessions(e.db).GetCredential(t.Context(), e.sess.Hash, "cluster_cred", "v:prod")
	require.NoError(t, err)
	assert.NotContains(t, row.Sealed, "sa-token-A")
	assert.Equal(t, e.clock.Now().Add(30*time.Minute).UnixMilli(), row.ExpiresAt.UnixMilli())

	out := e.auditOut.String()
	assert.Contains(t, out, `"verb":"vault-credential-issued"`)
	assert.Contains(t, out, "kubernetes/prod/creds/devops-role")
	assert.Contains(t, out, "lease-A")
	assert.NotContains(t, out, "sa-token-A")
	assert.NotContains(t, e.logs.String(), "sa-token-A")
	assert.Equal(t, 0, e.vault.Calls("revoke"), "leases are never revoked")
}

func TestOpen_Role_DefaultAllowedRememberedAndForbidden(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "nope")
	require.ErrorIs(t, err, apperr.ErrForbidden)

	st, err := e.op.Open(t.Context(), e.sess, "v:prod", "ops-role")
	require.NoError(t, err)
	assert.Equal(t, "ops-role", st.Role)

	st, err = e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	assert.Equal(t, "ops-role", st.Role, "the chosen role is remembered")
	assert.Equal(t, 1, e.count(t, "cluster_role"))
}

func TestOpen_UnknownCluster_Unavailable(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:missing", "")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
	_, err = e.op.Open(t.Context(), e.sess, "garbage", "")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestOpen_MountWithoutConfig_Unprocessable(t *testing.T) {
	for name, cc := range map[string]*fakevault.ClusterConfig{
		"no host":   {Host: "", CACert: "pem"},
		"no ca":     {Host: apiHost},
		"no config": nil,
	} {
		t.Run(name, func(t *testing.T) {
			e := newOpenerEnv(t)
			if cc != nil {
				e.vault.SetConfig("kubernetes/prod", *cc)
			} else {
				e.vault.SetConfig("kubernetes/prod", fakevault.ClusterConfig{})
			}
			_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
			a := apiErr(t, err)
			assert.Equal(t, http.StatusUnprocessableEntity, a.Status)
			assert.Equal(t, "unprocessable", a.Code)
			assert.Equal(t, "This cluster's Vault mount has no API server address or CA configured, so Kubelook can't open it.", a.Message)
			assert.Equal(t, 0, e.count(t, "cluster_cred"))
			assert.Equal(t, 0, e.vault.Calls("creds"))
		})
	}
}

func TestOpen_VaultUnreachable_503(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.reg.List(t.Context(), e.sess) // fills the discovery cache
	require.NoError(t, err)
	e.vault.HangUp(true)
	_, err = e.op.Open(t.Context(), e.sess, "v:prod", "")
	a := apiErr(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, a.Status)
	assert.Equal(t, "vault_unreachable", a.Code)
	assert.Equal(t, "Vault is unreachable, so this cluster can't be opened right now", a.Message)
}

func TestOpen_VaultRefusesCreds_422WithVaultMessage(t *testing.T) {
	e := newOpenerEnv(t)
	e.vault.SetCredsError("kubernetes/prod", "devops-role", credsMsg)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	a := apiErr(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, a.Status)
	assert.Contains(t, a.Message, credsMsg)
}

func TestOpen_VaultTokenMissing_Unavailable(t *testing.T) {
	e := newOpenerEnv(t)
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestToken_FreshCredential_NoVaultCall(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	e.clock.Advance(20 * time.Minute) // 10 minutes left, more than remintBefore
	tok, err := e.op.Token(t.Context(), e.sess.Hash, "v:prod")
	require.NoError(t, err)
	assert.Equal(t, "sa-token-A", tok)
	assert.Equal(t, 1, e.vault.Calls("creds"))
}

func TestToken_NearExpiry_RemintsAndAudits(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	e.vault.SetCreds("kubernetes/prod", "devops-role", fakevault.Creds{Token: "sa-token-B", LeaseID: "lease-B", LeaseDurationSeconds: 1800})
	e.clock.Advance(26 * time.Minute)
	tok, err := e.op.Token(t.Context(), e.sess.Hash, "v:prod")
	require.NoError(t, err)
	assert.Equal(t, "sa-token-B", tok)
	assert.Equal(t, 2, e.vault.Calls("creds"))
	assert.Contains(t, e.auditOut.String(), "lease-B")
	st := e.op.State(t.Context(), e.sess, "v:prod")
	assert.Equal(t, e.clock.Now().Add(30*time.Minute).UTC(), st.CredentialExpiresAt.UTC())
}

func TestToken_ConcurrentRemint_OneCredsCall(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	e.clock.Advance(26 * time.Minute)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.op.Token(t.Context(), e.sess.Hash, "v:prod")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, 2, e.vault.Calls("creds"), "one call for the open, one for all re-mints")
}

func TestToken_VaultDownWhileCredentialValid_KeepsWorkingThenFails(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	e.vault.HangUp(true)
	e.clock.Advance(26 * time.Minute)
	tok, err := e.op.Token(t.Context(), e.sess.Hash, "v:prod")
	require.NoError(t, err)
	assert.Equal(t, "sa-token-A", tok)

	e.clock.Advance(5 * time.Minute) // expired
	_, err = e.op.Token(t.Context(), e.sess.Hash, "v:prod")
	assert.Equal(t, "vault_unreachable", apiErr(t, err).Code)
}

func TestToken_VaultTokenGone_Disconnected(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	e.vault.Forget(vaultTok)
	e.clock.Advance(26 * time.Minute)
	_, err = e.op.Token(t.Context(), e.sess.Hash, "v:prod")
	a := apiErr(t, err)
	assert.Equal(t, "unauthenticated", a.Code)
	assert.Contains(t, a.Message, "Paste a new Vault token")
	assert.Equal(t, "disconnected", e.op.State(t.Context(), e.sess, "v:prod").State)
}

func TestState_ClosedOpenAndFromStoredCredential(t *testing.T) {
	e := newOpenerEnv(t)
	assert.Equal(t, "closed", e.op.State(t.Context(), e.sess, "v:prod").State)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	assert.Equal(t, "open", e.op.State(t.Context(), e.sess, "v:prod").State)
	assert.Equal(t, "open", e.op.StateOf(t.Context(), e.sess, "v:prod"))

	// A replica that never opened the cluster still sees the stored credential.
	other := cluster.NewOpener(cluster.OpenerDeps{Sessions: store.NewSessions(e.db), Clock: e.clock, Config: e.cfg})
	assert.Equal(t, "open", other.State(t.Context(), e.sess, "v:prod").State)
}

func TestRunRefresher_RemintsActiveClustersAndStopsAfterSessionEnds(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	<-e.signal

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { e.op.RunRefresher(ctx); close(done) }()
	e.clock.BlockUntil(1)

	e.clock.Advance(26 * time.Minute)
	testutil.Recv(t, e.signal)
	assert.Equal(t, 2, e.vault.Calls("creds"))

	require.NoError(t, e.sessions.End(t.Context(), e.sess.Hash))
	before := e.vault.Calls("creds")
	e.clock.Advance(26 * time.Minute)
	cancel()
	<-done
	assert.Equal(t, before, e.vault.Calls("creds"), "no re-mint after the session ended")
	assert.Equal(t, "closed", e.op.State(t.Context(), e.sess, "v:prod").State)
}

func TestRunRefresher_IdleClusterIsNotRenewed(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	<-e.signal
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { e.op.RunRefresher(ctx); close(done) }()
	e.clock.BlockUntil(1)
	e.clock.Advance(40 * time.Minute) // idle longer than the session idle timeout
	cancel()
	<-done
	assert.Equal(t, 1, e.vault.Calls("creds"))
}

func TestRESTConfig_VaultCluster_AttachesCurrentTokenPerRequest(t *testing.T) {
	e := newOpenerEnv(t)
	var mu sync.Mutex
	var seen []string
	api := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	api.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	api.StartTLS()
	t.Cleanup(api.Close)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw})
	e.vault.SetConfig("kubernetes/prod", fakevault.ClusterConfig{Host: api.URL, CACert: string(caPEM)})

	cfg, err := e.op.RESTConfig(t.Context(), e.sess, "v:prod") // opens lazily
	require.NoError(t, err)
	assert.Equal(t, api.URL, cfg.Host)
	assert.Equal(t, caPEM, cfg.CAData)
	assert.Empty(t, cfg.BearerToken)
	assert.Empty(t, cfg.BearerTokenFile)
	assert.EqualValues(t, 50, cfg.QPS)
	assert.Equal(t, 100, cfg.Burst)
	assert.Equal(t, time.Duration(0), cfg.Timeout)
	assert.Contains(t, cfg.UserAgent, "kubelook/")

	hc, err := rest.HTTPClientFor(cfg)
	require.NoError(t, err)
	get := func() {
		resp, err := hc.Get(api.URL + "/x")
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	get()
	e.vault.SetCreds("kubernetes/prod", "devops-role", fakevault.Creds{Token: "sa-token-B", LeaseID: "lease-B", LeaseDurationSeconds: 1800})
	e.clock.Advance(26 * time.Minute)
	get()
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"Bearer sa-token-A", "Bearer sa-token-B"}, seen)
}

func TestRESTConfig_UnknownCluster_Unavailable(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.RESTConfig(t.Context(), e.sess, "v:missing")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestToken_WithoutEntry_OpensWithTheStoredSession(t *testing.T) {
	e := newOpenerEnv(t)
	tok, err := e.op.Token(t.Context(), e.sess.Hash, "v:prod")
	require.NoError(t, err)
	assert.Equal(t, "sa-token-A", tok)
	_, err = e.op.Token(t.Context(), "no-such-session", "v:prod")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestOpen_Kubeconfig_NoVaultCallsAndStateOpen(t *testing.T) {
	e := newOpenerEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", tokenKubeconfig)
	require.NoError(t, err)
	callsBefore := e.vault.Calls("creds") + e.vault.Calls("config")
	st, err := e.op.Open(t.Context(), e.sess, "k:work/ctx1", "")
	require.NoError(t, err)
	assert.Equal(t, "open", st.State)
	assert.Equal(t, apiHost, st.Server)
	assert.Equal(t, callsBefore, e.vault.Calls("creds")+e.vault.Calls("config"))

	cfg, err := e.op.RESTConfig(t.Context(), e.sess, "k:work/ctx1")
	require.NoError(t, err)
	assert.Equal(t, "static-token", cfg.BearerToken)
	tok, err := e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx1")
	require.NoError(t, err)
	assert.Empty(t, tok, "static kubeconfig credentials are carried by the config itself")

	_, err = e.op.Open(t.Context(), e.sess, "k:other/ctx1", "")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestSessionEnd_DropsClusterCredentialsAndMemory(t *testing.T) {
	e := newOpenerEnv(t)
	_, err := e.op.Open(t.Context(), e.sess, "v:prod", "")
	require.NoError(t, err)
	require.Equal(t, 1, e.count(t, "cluster_cred"))
	require.NoError(t, e.sessions.End(t.Context(), e.sess.Hash))
	assert.Equal(t, 0, e.count(t, "cluster_cred"))
	assert.Equal(t, 0, e.sessions.Registry().Count(e.sess.Hash))
	assert.Equal(t, "closed", e.op.State(t.Context(), e.sess, "v:prod").State)
	assert.Equal(t, 0, e.vault.Calls("revoke"))
}

func (e *env) logger() *slog.Logger { return slog.New(slog.NewJSONHandler(e.logs, nil)) }

func (e *env) store() *store.Sessions { return store.NewSessions(e.db) }
