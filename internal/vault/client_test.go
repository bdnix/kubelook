package vault_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/testutil/fakevault"
	"github.com/bdnix/kubelook/internal/vault"
)

const secretToken = "hvs.SUPER-SECRET-TOKEN"

func newFake(t *testing.T) (*fakevault.Server, *vault.Client) {
	t.Helper()
	f := fakevault.New()
	t.Cleanup(f.Close)
	f.AddToken(secretToken, fakevault.Token{
		TTLSeconds: 1800, Renewable: true, Type: "service", Policies: []string{"default", "devops"},
		EntityID: "ent-1", DisplayName: "oidc-ada", ExpireTime: "2026-11-14T12:30:00Z",
		Caps: map[string][]string{"kubernetes/prod/config": {"read"}, "kubernetes/prod/creds/devops-role": {"update"}},
	})
	return f, vault.NewClient(f.URL(), f.Client())
}

func TestLookupSelf_ParsesTheTokenInfo(t *testing.T) {
	_, c := newFake(t)
	info, err := c.LookupSelf(t.Context(), secretToken)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, info.TTL)
	assert.True(t, info.Renewable)
	assert.Equal(t, "service", info.Type)
	assert.Equal(t, []string{"default", "devops"}, info.Policies)
	assert.Equal(t, "ent-1", info.EntityID)
	assert.Equal(t, "oidc-ada", info.DisplayName)
	assert.Equal(t, time.Date(2026, 11, 14, 12, 30, 0, 0, time.UTC), info.ExpireTime)
	assert.False(t, info.NeverExpires())
}

func TestLookupSelf_ZeroTTL_NeverExpires(t *testing.T) {
	f, c := newFake(t)
	f.AddToken("root", fakevault.Token{TTLSeconds: 0})
	info, err := c.LookupSelf(t.Context(), "root")
	require.NoError(t, err)
	assert.True(t, info.NeverExpires())
	assert.True(t, info.ExpireTime.IsZero())
}

func TestRenewSelf_ReturnsTheNewTTL(t *testing.T) {
	_, c := newFake(t)
	ttl, err := c.RenewSelf(t.Context(), secretToken)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, ttl)
}

func TestMounts_ReturnsPathToType(t *testing.T) {
	f, c := newFake(t)
	f.AddMount("kubernetes/prod/", "kubernetes")
	f.AddMount("secret/", "kv")
	got, err := c.Mounts(t.Context(), secretToken)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"kubernetes/prod/": "kubernetes", "secret/": "kv"}, got)
}

func TestCapabilities_ParsesPathsAndSplitsAt100(t *testing.T) {
	f, c := newFake(t)
	paths := []string{"kubernetes/prod/config", "kubernetes/prod/creds/devops-role"}
	for i := range 230 {
		paths = append(paths, fmt.Sprintf("kubernetes/c%d/config", i))
	}
	got, err := c.Capabilities(t.Context(), secretToken, paths)
	require.NoError(t, err)
	assert.Equal(t, []string{"read"}, got["kubernetes/prod/config"])
	assert.Equal(t, []string{"update"}, got["kubernetes/prod/creds/devops-role"])
	assert.Equal(t, []string{"deny"}, got["kubernetes/c229/config"])
	assert.Len(t, got, 232)
	calls := f.CapabilityRequests()
	require.Len(t, calls, 3, "232 paths → ceil(232/100) calls")
	assert.Len(t, calls[0], 100)
	assert.Len(t, calls[2], 32)
}

func TestCapabilities_NoPaths_NoCall(t *testing.T) {
	f, c := newFake(t)
	got, err := c.Capabilities(t.Context(), secretToken, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 0, f.Calls("capabilities"))
}

func TestClusterConfig_ParsesHostAndCA(t *testing.T) {
	f, c := newFake(t)
	f.SetConfig("kubernetes/prod", fakevault.ClusterConfig{Host: "https://api.prod.example:6443", CACert: "-----BEGIN CERTIFICATE-----"})
	got, err := c.ClusterConfig(t.Context(), secretToken, "kubernetes/prod")
	require.NoError(t, err)
	assert.Equal(t, vault.ClusterConfig{Host: "https://api.prod.example:6443", CACert: "-----BEGIN CERTIFICATE-----"}, got)
}

func TestClusterConfig_NeverConfigured404_ZeroValueNoError(t *testing.T) {
	_, c := newFake(t)
	got, err := c.ClusterConfig(t.Context(), secretToken, "kubernetes/unconfigured")
	require.NoError(t, err)
	assert.Equal(t, vault.ClusterConfig{}, got)
}

func TestCreds_SendsTheRequestAndParsesTheAnswer(t *testing.T) {
	f, c := newFake(t)
	f.SetCreds("kubernetes/prod", "devops-role", fakevault.Creds{
		Token: "sa-token", ServiceAccount: "v-token-abc", Namespace: "itops", LeaseID: "kubernetes/prod/creds/devops-role/xyz", LeaseDurationSeconds: 1800,
	})
	got, err := c.Creds(t.Context(), secretToken, "kubernetes/prod", "devops-role",
		vault.CredsRequest{Namespace: "itops", ClusterRoleBinding: true, TTL: 30 * time.Minute})
	require.NoError(t, err)
	assert.Equal(t, vault.Creds{
		Token: "sa-token", ServiceAccount: "v-token-abc", Namespace: "itops",
		LeaseID: "kubernetes/prod/creds/devops-role/xyz", LeaseDuration: 30 * time.Minute,
	}, got)
	reqs := f.CredsRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, map[string]any{"kubernetes_namespace": "itops", "cluster_role_binding": true, "ttl": "30m0s"}, reqs[0])
}

func TestDo_SendsVaultHeaders(t *testing.T) {
	var gotToken, gotReq string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken, gotReq = r.Header.Get("X-Vault-Token"), r.Header.Get("X-Vault-Request")
		_, _ = w.Write([]byte(`{"data":{"ttl":1}}`))
	}))
	defer srv.Close()
	_, err := vault.NewClient(srv.URL, srv.Client()).LookupSelf(t.Context(), "tok-1")
	require.NoError(t, err)
	assert.Equal(t, "tok-1", gotToken)
	assert.Equal(t, "true", gotReq)
}

func TestErrors_StatusMapping(t *testing.T) {
	f, c := newFake(t)
	ctx := t.Context()

	_, err := c.LookupSelf(ctx, "unknown-token")
	require.ErrorIs(t, err, vault.ErrPermissionDenied)
	assert.Contains(t, err.Error(), "permission denied", "Vault's own message is kept")

	f.SetCredsError("kubernetes/prod", "devops-role", "bad CA bundle")
	_, err = c.Creds(ctx, secretToken, "kubernetes/prod", "devops-role", vault.CredsRequest{})
	var re *vault.ResponseError
	require.ErrorAs(t, err, &re, "a 500 with an errors list is an answer, not an outage")
	assert.Equal(t, http.StatusInternalServerError, re.Status)
	assert.Equal(t, []string{"bad CA bundle"}, re.Messages)
	assert.NotErrorIs(t, err, vault.ErrUnreachable)

	_, err = c.Creds(ctx, secretToken, "kubernetes/prod", "no-such-role", vault.CredsRequest{})
	require.ErrorIs(t, err, vault.ErrNotFound)

	for _, code := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		f.ForceStatus(code)
		_, err = c.LookupSelf(ctx, secretToken)
		require.ErrorIs(t, err, vault.ErrUnreachable, "status %d", code)
	}

	f.ForceStatus(http.StatusBadRequest)
	_, err = c.LookupSelf(ctx, secretToken)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, http.StatusBadRequest, re.Status)

	f.ForceStatus(http.StatusTeapot)
	_, err = c.LookupSelf(ctx, secretToken)
	require.Error(t, err)
	assert.NotErrorIs(t, err, vault.ErrUnreachable)
}

func TestErrors_ConnectionProblems_AreUnreachable(t *testing.T) {
	f, c := newFake(t)
	f.HangUp(true)
	_, err := c.LookupSelf(t.Context(), secretToken)
	require.ErrorIs(t, err, vault.ErrUnreachable)

	f.HangUp(false)
	f.Close()
	_, err = c.LookupSelf(t.Context(), secretToken)
	require.ErrorIs(t, err, vault.ErrUnreachable, "connection refused")
}

func TestErrors_Timeout_IsUnreachable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	c := vault.NewClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond})
	_, err := c.LookupSelf(t.Context(), secretToken)
	require.ErrorIs(t, err, vault.ErrUnreachable)
}

func TestErrors_ContextCancellationPropagates(t *testing.T) {
	_, c := newFake(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.LookupSelf(ctx, secretToken)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, vault.ErrUnreachable)
}

func TestErrors_ResponseOver4MiB_Fails(t *testing.T) {
	f, c := newFake(t)
	f.PadResponses(4<<20 + 1)
	_, err := c.LookupSelf(t.Context(), secretToken)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestErrors_NeverContainTheToken(t *testing.T) {
	f, c := newFake(t)
	ctx := t.Context()
	var errs []error
	collect := func(err error) { errs = append(errs, err) }

	_, err := c.LookupSelf(ctx, "unknown-"+secretToken)
	collect(err)
	f.ForceStatus(http.StatusServiceUnavailable)
	_, err = c.LookupSelf(ctx, secretToken)
	collect(err)
	f.ForceStatus(http.StatusBadRequest)
	_, err = c.LookupSelf(ctx, secretToken)
	collect(err)
	f.ForceStatus(0)
	f.HangUp(true)
	_, err = c.LookupSelf(ctx, secretToken)
	collect(err)

	// a server that echoes the token in its error text
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"errors":["bad token %s"]}`, r.Header.Get("X-Vault-Token"))
	}))
	defer srv.Close()
	_, err = vault.NewClient(srv.URL, srv.Client()).LookupSelf(ctx, secretToken)
	collect(err)

	for _, e := range errs {
		require.Error(t, e)
		assert.NotContains(t, e.Error(), secretToken)
	}
}
