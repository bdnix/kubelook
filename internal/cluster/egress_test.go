package cluster_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/egress"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil/fakeoidc"
)

// guardedOpener is the opener of the env with the egress guard switched on.
func (e *openerEnv) guardedOpener(t *testing.T) *cluster.Opener {
	t.Helper()
	return cluster.NewOpener(cluster.OpenerDeps{
		Registry: e.reg, Credentials: e.c, Sessions: store.NewSessions(e.db), Sealer: e.sealer, Vault: e.vaultClient(), Clock: e.clock,
		Config: e.cfg, Audit: audit.NewRecorder(e.db, e.clock, e.auditOut, func() string { return "a" }),
		Closers: e.sessions.Registry(), Log: e.logger(), BlockLocal: true,
	})
}

func kubeconfigFor(server string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "%s", insecure-skip-tls-verify: true}
users:
- name: u
  user: {token: static-token}
contexts:
- name: ctx
  context: {cluster: c, user: u}
`, server)
}

func TestOpen_KubeconfigWithALocalServerAddress_IsRefusedWhenTheGuardIsOn(t *testing.T) {
	for _, server := range []string{"https://127.0.0.1:6443", "https://[::1]:6443", "https://169.254.169.254", "https://0.0.0.0:6443"} {
		e := newOpenerEnv(t)
		_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", kubeconfigFor(server))
		require.NoError(t, err, "uploading never connects: "+server)

		_, err = e.guardedOpener(t).Open(t.Context(), e.sess, "k:work/ctx", "")

		require.ErrorIs(t, err, egress.ErrBlocked, server)
		assert.Equal(t, 422, apiErr(t, err).Status)
		assert.Equal(t, "This address isn't allowed: Kubelook doesn't connect to local or link-local addresses from a kubeconfig.", apiErr(t, err).Message)
		assert.Equal(t, "closed", e.guardedOpener(t).State(t.Context(), e.sess, "k:work/ctx").State)
	}
}

func TestOpen_KubeconfigWithALocalServerAddress_OpensWhenTheGuardIsOff(t *testing.T) {
	e := newOpenerEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", kubeconfigFor("https://127.0.0.1:6443"))
	require.NoError(t, err)
	st, err := e.op.Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)
	assert.Equal(t, "open", st.State)
}

func TestOpen_KubeconfigWithAnAddressOnAPrivateNetwork_Opens(t *testing.T) {
	e := newOpenerEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", kubeconfigFor("https://10.0.0.5:6443"))
	require.NoError(t, err)
	st, err := e.guardedOpener(t).Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)
	assert.Equal(t, "open", st.State)
}

func TestRESTConfig_Kubeconfig_ConnectionsAreGuarded_VaultClustersAreNot(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "{}") }))
	t.Cleanup(srv.Close)
	// A name, not an address literal: only the check at connection time can catch it.
	named := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	e := newOpenerEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", kubeconfigFor(named))
	require.NoError(t, err)
	op := e.guardedOpener(t)

	cfg, err := op.RESTConfig(t.Context(), e.sess, "k:work/ctx")
	require.NoError(t, err)
	require.NotNil(t, cfg.Dial)
	hc, err := rest.HTTPClientFor(cfg)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, named, nil)
	require.NoError(t, err)
	resp, err := hc.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, egress.ErrBlocked)

	vcfg, err := op.RESTConfig(t.Context(), e.sess, "v:prod")
	require.NoError(t, err)
	assert.Nil(t, vcfg.Dial, "a Vault cluster is set by the deployer and keeps its own transport")

	off, err := e.op.RESTConfig(t.Context(), e.sess, "k:work/ctx")
	require.NoError(t, err)
	assert.Nil(t, off.Dial, "with the guard off nothing is changed")
}

func TestToken_OIDCIssuerOnALocalAddress_IsRefusedWhenTheGuardIsOn(t *testing.T) {
	e := newOpenerEnv(t)
	idp := fakeoidc.New(e.clock.Now, "kubelook") // listens on 127.0.0.1
	t.Cleanup(idp.Close)
	idp.RefreshToken = "refresh-1"
	expired := idp.IDToken(fakeoidc.Claims{Sub: "ada", Exp: e.clock.Now().Add(-time.Hour).Unix()})
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", oidcKubeconfig(idp.URL(), expired))
	require.NoError(t, err)
	op := e.guardedOpener(t)
	_, err = op.Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)

	_, err = op.Token(t.Context(), e.sess.Hash, "k:work/ctx")

	require.ErrorIs(t, err, egress.ErrBlocked)
	assert.Equal(t, 0, idp.Refreshes(), "the identity provider was never contacted")
}
