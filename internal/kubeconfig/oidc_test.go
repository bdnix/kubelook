package kubeconfig_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kubeconfig"
)

func TestOIDC_ReadsTheProviderSettingsOfAContext(t *testing.T) {
	f, err := kubeconfig.Parse("work", load(t, "oidc"))
	require.NoError(t, err)
	s, ok := f.OIDC("ctx")
	require.True(t, ok)
	assert.Equal(t, kubeconfig.OIDCSettings{
		IssuerURL: "https://issuer.example", ClientID: "kubelook", ClientSecret: "s3cret", RefreshToken: "refresh-1", IDToken: "id-token-1",
	}, s)
}

func TestOIDC_DecodesTheIssuerCAData(t *testing.T) {
	const kc = `apiVersion: v1
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
        idp-issuer-url: https://issuer.example
        client-id: kubelook
        idp-certificate-authority-data: aWRwLWNh
contexts:
- name: ctx
  context: {cluster: c, user: u}
`
	f, err := kubeconfig.Parse("work", []byte(kc))
	require.NoError(t, err)
	s, ok := f.OIDC("ctx")
	require.True(t, ok)
	assert.Equal(t, []byte("idp-ca"), s.IDPCAData)
}

func TestOIDC_NotOIDCOrUnknownContext(t *testing.T) {
	f, err := kubeconfig.Parse("work", load(t, "token"))
	require.NoError(t, err)
	_, ok := f.OIDC("ctx")
	assert.False(t, ok)
	_, ok = f.OIDC("nope")
	assert.False(t, ok)
}

func TestSetOIDCTokens_UpdatesTheTokensAndSerializes(t *testing.T) {
	f, err := kubeconfig.Parse("work", load(t, "oidc"))
	require.NoError(t, err)
	out, err := f.SetOIDCTokens("ctx", "id-token-2", "refresh-2")
	require.NoError(t, err)

	again, err := kubeconfig.Parse("work", out)
	require.NoError(t, err)
	s, _ := again.OIDC("ctx")
	assert.Equal(t, "id-token-2", s.IDToken)
	assert.Equal(t, "refresh-2", s.RefreshToken)
	assert.Equal(t, "s3cret", s.ClientSecret, "other settings are kept")

	out, err = f.SetOIDCTokens("ctx", "id-token-3", "")
	require.NoError(t, err)
	again, _ = kubeconfig.Parse("work", out)
	s, _ = again.OIDC("ctx")
	assert.Equal(t, "id-token-3", s.IDToken)
	assert.Equal(t, "refresh-2", s.RefreshToken, "an empty refresh token keeps the old one")
}

func TestSetOIDCTokens_NoOIDCUser(t *testing.T) {
	f, err := kubeconfig.Parse("work", load(t, "token"))
	require.NoError(t, err)
	_, err = f.SetOIDCTokens("ctx", "x", "y")
	require.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = f.SetOIDCTokens("nope", "x", "y")
	require.ErrorIs(t, err, apperr.ErrNotFound)
}
