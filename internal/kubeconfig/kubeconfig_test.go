package kubeconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kubeconfig"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
	require.NoError(t, err)
	return b
}

func TestParse_ClientCertificateData_Accepted(t *testing.T) {
	f, err := kubeconfig.Parse("work", load(t, "cert"))
	require.NoError(t, err)
	assert.Equal(t, "work", f.Label)
	require.Len(t, f.Contexts, 1)
	assert.Equal(t, kubeconfig.ContextInfo{
		ID: "k:work/prod", Name: "prod-cluster", Context: "prod", User: "ada",
		Server: "https://api.prod.example:6443", Auth: kubeconfig.Certificate,
	}, f.Contexts[0])

	rc, err := f.RESTConfig("prod")
	require.NoError(t, err)
	assert.Equal(t, "https://api.prod.example:6443", rc.Host)
	assert.Equal(t, []byte("cert"), rc.CertData)
	assert.Equal(t, []byte("key"), rc.KeyData)
	assert.Equal(t, []byte("ca-data"), rc.CAData)
	assertNoExternalReferences(t, rc)
}

func TestParse_BearerToken_Accepted(t *testing.T) {
	f, err := kubeconfig.Parse("t", load(t, "token"))
	require.NoError(t, err)
	assert.Equal(t, kubeconfig.Token, f.Contexts[0].Auth)
	rc, err := f.RESTConfig("ctx")
	require.NoError(t, err)
	assert.Equal(t, "static-bearer-token", rc.BearerToken)
	assertNoExternalReferences(t, rc)
}

func TestParse_OIDCAuthProvider_AcceptedAndNotLeftToClientGo(t *testing.T) {
	f, err := kubeconfig.Parse("o", load(t, "oidc"))
	require.NoError(t, err)
	assert.Equal(t, kubeconfig.OIDC, f.Contexts[0].Auth)
	rc, err := f.RESTConfig("ctx")
	require.NoError(t, err)
	assert.Nil(t, rc.AuthProvider, "OIDC refresh is Kubelook's own (Step 3.1), not client-go's plugin")
	assert.Equal(t, "id-token-1", rc.BearerToken)
	assertNoExternalReferences(t, rc)
}

// assertNoExternalReferences asserts the REST config holds only in-memory data: no exec
// plugin, no auth-provider and no file path anywhere.
func assertNoExternalReferences(t *testing.T, rc *rest.Config) {
	t.Helper()
	assert.Nil(t, rc.ExecProvider)
	assert.Nil(t, rc.AuthProvider)
	assert.Empty(t, rc.CertFile)
	assert.Empty(t, rc.KeyFile)
	assert.Empty(t, rc.CAFile)
	assert.Empty(t, rc.BearerTokenFile)
}

func TestParse_Rejections(t *testing.T) {
	cases := []struct {
		file     string
		contains []string
		entry    string
	}{
		{"exec", []string{"exec credential plugins are not supported", `"eks-user"`}, "eks-user"},
		{"cert-path", []string{"file path references are not supported (client-certificate)"}, "u"},
		{"key-path", []string{"file path references are not supported (client-key)"}, "u"},
		{"ca-path", []string{"file path references are not supported (certificate-authority)"}, "c"},
		{"oidc-ca-path", []string{"file path references are not supported (idp-certificate-authority)"}, "u"},
		{"token-file", []string{"file path references are not supported (tokenFile)"}, "u"},
		{"other-provider", []string{`auth-provider "gcp" is not supported`}, "u"},
		{"basic-auth", []string{"username/password authentication is not supported"}, "u"},
		{"oidc-incomplete", []string{"idp-issuer-url"}, "u"},
		{"two-contexts-one-bad", []string{"exec credential plugins are not supported", `"eks-user"`}, "eks-user"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			f, err := kubeconfig.Parse("x", load(t, tc.file))
			require.Error(t, err)
			assert.Nil(t, f, "no partial load")
			for _, s := range tc.contains {
				assert.Contains(t, err.Error(), s)
			}
			var ue *kubeconfig.UnsupportedError
			require.ErrorAs(t, err, &ue)
			assert.Equal(t, tc.entry, ue.Entry)
			assert.ErrorIs(t, err, apperr.ErrInvalid)
		})
	}
}

func TestParse_ContextReferringToMissingEntries_NamesTheContext(t *testing.T) {
	_, err := kubeconfig.Parse("x", load(t, "missing-cluster"))
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Contains(t, err.Error(), `"broken"`)
	_, err = kubeconfig.Parse("x", load(t, "missing-user"))
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Contains(t, err.Error(), `"broken-user"`)
}

func TestParse_NoContexts(t *testing.T) {
	_, err := kubeconfig.Parse("x", load(t, "no-contexts"))
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Contains(t, err.Error(), "kubeconfig has no contexts")
}

func TestParse_InvalidYAML(t *testing.T) {
	for _, in := range []string{"{{{ not yaml", "just: [unclosed", "\t\tbad: indent:\n  - x"} {
		_, err := kubeconfig.Parse("x", []byte(in))
		require.ErrorIs(t, err, apperr.ErrInvalid, in)
		assert.Contains(t, err.Error(), "not a valid kubeconfig", in)
	}
}

func TestParse_TooLarge(t *testing.T) {
	_, err := kubeconfig.Parse("x", []byte(strings.Repeat("a", 1<<20+1)))
	require.ErrorIs(t, err, apperr.ErrTooLarge)
}

func TestParse_InvalidLabels(t *testing.T) {
	for _, label := range []string{"", "a/b", strings.Repeat("x", 64)} {
		_, err := kubeconfig.Parse(label, load(t, "cert"))
		require.ErrorIs(t, err, apperr.ErrInvalid, label)
	}
	_, err := kubeconfig.Parse(strings.Repeat("x", 63), load(t, "cert"))
	require.NoError(t, err)
}

func TestParse_TwoContexts_SortedByContextName(t *testing.T) {
	f, err := kubeconfig.Parse("p", load(t, "two-contexts"))
	require.NoError(t, err)
	require.Len(t, f.Contexts, 2)
	assert.Equal(t, "k:p/ctx1", f.Contexts[0].ID)
	assert.Equal(t, "https://one.example:6443", f.Contexts[0].Server)
	assert.Equal(t, "k:p/ctx2", f.Contexts[1].ID)
}

func TestRESTConfig_UnknownContext_Error(t *testing.T) {
	f, err := kubeconfig.Parse("p", load(t, "two-contexts"))
	require.NoError(t, err)
	_, err = f.RESTConfig("nope")
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestRESTConfig_ReadsNoFilesAndNoEnvironment(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent/kubeconfig")
	t.Setenv("HOME", t.TempDir())
	f, err := kubeconfig.Parse("p", load(t, "token"))
	require.NoError(t, err)
	rc, err := f.RESTConfig("ctx")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example:6443", rc.Host)
}
