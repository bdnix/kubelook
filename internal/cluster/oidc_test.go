package cluster_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/testutil/fakeoidc"
)

func oidcKubeconfig(issuer, idToken string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "https://api.example:6443", certificate-authority-data: Y2EtZGF0YQ==}
users:
- name: u
  user:
    auth-provider:
      name: oidc
      config:
        idp-issuer-url: %s
        client-id: kubelook
        client-secret: s3cret
        refresh-token: refresh-1
        id-token: %s
contexts:
- name: ctx
  context: {cluster: c, user: u}
`, issuer, idToken)
}

type oidcEnv struct {
	*openerEnv
	idp *fakeoidc.Issuer
}

func newOIDCEnv(t *testing.T, expired bool) *oidcEnv {
	t.Helper()
	e := newOpenerEnv(t)
	idp := fakeoidc.New(e.clock.Now, "kubelook")
	t.Cleanup(idp.Close)
	idp.RefreshToken = "refresh-1"
	idp.NewRefreshToken = "refresh-2"
	exp := e.clock.Now().Add(time.Hour).Unix()
	if expired {
		exp = e.clock.Now().Add(-time.Hour).Unix()
	}
	old := idp.IDToken(fakeoidc.Claims{Sub: "ada", Exp: exp})
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", oidcKubeconfig(idp.URL(), old))
	require.NoError(t, err)
	return &oidcEnv{openerEnv: e, idp: idp}
}

func TestToken_OIDCExpired_RefreshesAndQueuesNewBrowserCopy(t *testing.T) {
	e := newOIDCEnv(t, true)
	_, err := e.op.Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)

	tok, err := e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx")
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	assert.Equal(t, 1, e.idp.Refreshes())

	again, err := e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx")
	require.NoError(t, err)
	assert.Equal(t, tok, again)
	assert.Equal(t, 1, e.idp.Refreshes(), "the new token is reused")

	// the session's kubeconfig carries the rotated refresh token
	row, err := e.store().GetCredential(t.Context(), e.sess.Hash, "kubeconfig", "work")
	require.NoError(t, err)
	pt, err := e.sealer.Open(row.Sealed, crypto.AAD(crypto.PurposeSessionCredential, e.sess.Hash))
	require.NoError(t, err)
	assert.Contains(t, string(pt), "refresh-2")

	pending, err := e.op.Pending(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "work", pending[0].Label)
	saved := e.openSaved(t, pending[0].Sealed)
	assert.Equal(t, "kubeconfig", saved["kind"])
	assert.True(t, strings.Contains(saved["content"].(string), "refresh-2"))

	require.NoError(t, e.op.AckPending(t.Context(), e.sess, []string{"work"}))
	pending, err = e.op.Pending(t.Context(), e.sess)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestToken_OIDCStillValid_NoRefresh(t *testing.T) {
	e := newOIDCEnv(t, false)
	_, err := e.op.Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)
	tok, err := e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx")
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	assert.Equal(t, 0, e.idp.Refreshes())

	e.clock.Advance(2 * time.Hour) // the token expires
	_, err = e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx")
	require.NoError(t, err)
	assert.Equal(t, 1, e.idp.Refreshes())
}

func TestToken_OIDCRefreshRejected_Disconnected(t *testing.T) {
	e := newOIDCEnv(t, true)
	e.idp.RefreshToken = "something-else"
	_, err := e.op.Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)
	_, err = e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx")
	assert.Equal(t, "unauthenticated", apiErr(t, err).Code)
	assert.Equal(t, "disconnected", e.op.State(t.Context(), e.sess, "k:work/ctx").State)
}

func TestToken_OIDCIssuerDown_Busy(t *testing.T) {
	e := newOIDCEnv(t, true)
	_, err := e.op.Open(t.Context(), e.sess, "k:work/ctx", "")
	require.NoError(t, err)
	e.idp.SetDown(true)
	_, err = e.op.Token(t.Context(), e.sess.Hash, "k:work/ctx")
	require.ErrorContains(t, err, "identity provider")
}

func TestRESTConfig_OIDC_NoStaticToken(t *testing.T) {
	e := newOIDCEnv(t, false)
	cfg, err := e.op.RESTConfig(t.Context(), e.sess, "k:work/ctx")
	require.NoError(t, err)
	assert.Empty(t, cfg.BearerToken)
	assert.NotNil(t, cfg.WrapTransport)
}
