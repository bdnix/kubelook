//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

type vaultAnswer struct {
	Sealed string `json:"sealed"`
	Vault  struct {
		TTLSeconds int  `json:"ttlSeconds"`
		CanRenew   bool `json:"canRenew"`
		Renewable  bool `json:"renewable"`
	} `json:"vault"`
}

func addToken(t *testing.T, c *it.Client, tokenName string) (int, vaultAnswer, it.APIError) {
	t.Helper()
	var ok vaultAnswer
	var bad it.APIError
	status, data := c.Do(http.MethodPost, "/api/v1/credentials/vault", mustJSON(t, map[string]string{"token": it.Token(tokenName)}), nil)
	unmarshal(data, &ok)
	unmarshal(data, &bad)
	return status, ok, bad
}

// TestVault_Discovery_ListsExactlyTheClustersThePoliciesAllow is the spec's Gate 1: a token lists
// the clusters its policies allow, and nothing else.
func TestVault_Discovery_ListsExactlyTheClustersThePoliciesAllow(t *testing.T) {
	type want struct {
		id    string
		roles []string
	}
	cases := []struct {
		name, token string
		want        []want
	}{
		{"alice", "ALICE_TOKEN", []want{{"v:alpha", []string{"devops-role"}}}},
		{"bob", "BOB_TOKEN", []want{{"v:alpha", []string{"devops-role", "viewer-role"}}, {"v:beta", []string{"devops-role"}}}},
		{"carol (create only, so no update)", "CAROL_TOKEN", nil},
		{"dave (a mount without configuration; the wildcard grants every candidate role)", "DAVE_TOKEN", []want{{"v:empty", []string{"devops-role", "viewer-role"}}}},
		{"viewer", "VIEWER_TOKEN", []want{{"v:alpha", []string{"viewer-role"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := signedIn(t, "vault-disc-"+tc.token)
			status, _, _ := addToken(t, c, tc.token)
			require.Equal(t, http.StatusOK, status)
			got := vaultClusters(t, c)
			require.Len(t, got, len(tc.want), "clusters: %+v", got)
			for i, w := range tc.want {
				assert.Equal(t, w.id, got[i].ID)
				assert.Equal(t, w.roles, got[i].Roles, w.id)
			}
		})
	}
}

func TestVault_Tokens_InvalidAndRevokedAreRejectedAndBatchCannotRenew(t *testing.T) {
	c := signedIn(t, "vault-tokens")
	status, _, bad := addToken(t, c, "REVOKED_TOKEN")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "Vault rejected this token.", bad.Error.Message)

	status, _, _ = addTokenValue(t, c, "hvs.not-a-token")
	assert.Equal(t, http.StatusBadRequest, status)

	status, ok, _ := addToken(t, c, "BATCH_TOKEN")
	require.Equal(t, http.StatusOK, status)
	assert.False(t, ok.Vault.CanRenew, "a batch token cannot be renewed")
	assert.Positive(t, ok.Vault.TTLSeconds)
}

func addTokenValue(t *testing.T, c *it.Client, token string) (int, vaultAnswer, it.APIError) {
	t.Helper()
	var ok vaultAnswer
	var bad it.APIError
	status, data := c.Do(http.MethodPost, "/api/v1/credentials/vault", mustJSON(t, map[string]string{"token": token}), nil)
	unmarshal(data, &ok)
	unmarshal(data, &bad)
	return status, ok, bad
}

func TestVault_Open_MintsCredentialsOnTheRealClusterAndAuditsThem(t *testing.T) {
	c := signedIn(t, "vault-open")
	addToken(t, c, "ALICE_TOKEN")

	status, st, apiErr := open(t, c, "v:alpha", "")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "open", st.State)
	assert.Equal(t, "devops-role", st.Role)
	require.NotNil(t, st.CredentialExpiresAt)

	status, items := listItems(t, c, "v:alpha", "core", "v1", "pods", "demo")
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, strings.Join(names(items), ","), "logger-", "the fixture pods of namespace demo")

	// Vault created a service account in the cluster for this credential
	assert.NotEmpty(t, it.Kubectl(t, "alpha", "get", "serviceaccounts", "-n", "itops", "-o", "name"),
		"Vault's creds endpoint creates a service account in the itops namespace")

	var issued bool
	for _, l := range it.AuditLines(t) {
		if l.Verb != "vault-credential-issued" || l.Account["email"] != "vault-open@corp.example" {
			continue
		}
		issued = true
		assert.Contains(t, l.Raw, "kubernetes/alpha/creds/devops-role", "the audit line names the creds path")
		assert.Contains(t, l.Raw, "lease", "and the lease")
		assert.NotContains(t, l.Raw, it.Token("ALICE_TOKEN"), "and never the token")
	}
	assert.True(t, issued, "vault-credential-issued is audited")
}

func TestVault_Open_AMountWithoutAPIServerSettingsIsRefusedWithItsMessage(t *testing.T) {
	c := signedIn(t, "vault-empty")
	addToken(t, c, "DAVE_TOKEN")
	status, _, apiErr := open(t, c, "v:empty", "")
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, apiErr.Error.Message, "no API server address or CA")
}

func TestVault_ViewerRole_IsReadOnlyInTheCluster(t *testing.T) {
	c := signedIn(t, "vault-viewer-role")
	addToken(t, c, "BOB_TOKEN")
	status, st, apiErr := open(t, c, "v:alpha", "viewer-role")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "viewer-role", st.Role)

	pod := podOf(t, "alpha", "web")
	status, data := c.Do(http.MethodDelete, "/api/v1/clusters/v:alpha/resources/core/v1/pods/"+pod+"?namespace=demo", nil, nil)
	assert.Equal(t, http.StatusForbidden, status, "the cluster refuses the delete: %s", data)

	var access struct {
		Results []struct{ Allowed bool } `json:"results"`
	}
	body := map[string]any{"checks": []map[string]string{{"verb": "delete", "group": "core", "resource": "pods", "namespace": "demo"}}}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/access", body, &access))
	require.Len(t, access.Results, 1)
	assert.False(t, access.Results[0].Allowed, "the access check reports delete pods as not allowed")
	assert.NotEmpty(t, podOf(t, "alpha", "web"), "and the pod is still there")
}

func TestVault_Remint_ReplacesTheCredentialBeforeItExpires(t *testing.T) {
	c := signedIn(t, "vault-remint")
	addToken(t, c, "ALICE_TOKEN")
	_, first, _ := open(t, c, "v:alpha", "")
	require.NotNil(t, first.CredentialExpiresAt)

	// remintBefore is 9m50s of a 10m credential, so the background refresher replaces it after ~10 s
	it.Eventually(t, 90*time.Second, func() bool {
		_, st, _ := open(t, c, "v:alpha", "")
		return st.CredentialExpiresAt != nil && st.CredentialExpiresAt.After(*first.CredentialExpiresAt)
	}, "credentialExpiresAt moves to a later time")

	status, items := listItems(t, c, "v:alpha", "core", "v1", "pods", "demo")
	assert.Equal(t, http.StatusOK, status)
	assert.NotEmpty(t, items, "the new credential works")
}

func TestVault_Renew_ExtendsTheTokenUntilItsExplicitMaximum(t *testing.T) {
	c := signedIn(t, "vault-renew")
	status, first, _ := addToken(t, c, "SHORT_TOKEN")
	require.Equal(t, http.StatusOK, status)
	require.True(t, first.Vault.CanRenew)

	var renewed struct {
		Vault struct {
			TTLSeconds int  `json:"ttlSeconds"`
			CanRenew   bool `json:"canRenew"`
		} `json:"vault"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/credentials/vault/renew", nil, &renewed))
	assert.Greater(t, renewed.Vault.TTLSeconds, first.Vault.TTLSeconds-60, "the renewed token has its time back")

	// the token's explicit maximum is 25 minutes: renewing keeps working until it is reached
	it.Eventually(t, 60*time.Second, func() bool {
		c.JSON(http.MethodPost, "/api/v1/credentials/vault/renew", nil, &renewed)
		return !renewed.Vault.CanRenew
	}, "canRenew becomes false once the explicit maximum TTL is reached")
}

func TestVault_SavedCopy_RestoresForTheSameUserOnly(t *testing.T) {
	c := signedIn(t, "vault-saved")
	_, ok, _ := addToken(t, c, "ALICE_TOKEN")
	require.NotEmpty(t, ok.Sealed)

	type results struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	restore := func(c *it.Client) string {
		var r results
		require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/credentials/restore", map[string]any{"items": []map[string]string{{"sealed": ok.Sealed}}}, &r))
		require.Len(t, r.Results, 1)
		return r.Results[0].Status
	}

	// a new session of the same account
	again := signedIn(t, "vault-saved")
	assert.Equal(t, "ok", restore(again))
	assert.NotEmpty(t, vaultClusters(t, again), "the restored token lists its clusters")

	// another user
	other := signedIn(t, "vault-saved-other")
	assert.Equal(t, "wrong_user", restore(other))
}

func TestVault_Logout_ClosesEveryOpenConnection(t *testing.T) {
	c := signedIn(t, "vault-logout")
	addToken(t, c, "ALICE_TOKEN")
	_, _, apiErr := open(t, c, "v:alpha", "")
	require.Empty(t, apiErr.Error.Message)
	logger, shell := podOf(t, "alpha", "logger"), podOf(t, "alpha", "shell")

	w1 := c.WS()
	w1.Open("watch.open", map[string]any{"cluster": "v:alpha", "group": "core", "version": "v1", "resource": "pods", "namespace": "demo"})
	w1.Open("logs.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": logger, "follow": true})
	w1.Open("exec.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": shell, "cols": 80, "rows": 24})
	w2 := c.WS()

	require.Equal(t, http.StatusNoContent, c.JSON(http.MethodPost, "/api/v1/auth/logout", nil, nil))
	assert.Equal(t, 4401, w1.Closed(10*time.Second), "the socket with the watch, the log follow and the shell closes")
	assert.Equal(t, 4401, w2.Closed(10*time.Second), "a second socket of the same session closes too")
}

func TestVault_Outage_OpenClustersKeepWorkingAndNewOnesReportVaultUnreachable(t *testing.T) {
	c := signedIn(t, "vault-outage")
	addToken(t, c, "BOB_TOKEN")
	status, _, apiErr := open(t, c, "v:alpha", "")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)

	it.Docker(t, "pause", "kubelook-it-vault")
	t.Cleanup(func() { it.TryDocker("unpause", "kubelook-it-vault") })

	type vaultStatus struct {
		Vault struct{ Reachable bool } `json:"vault"`
	}
	reachable := func() bool {
		var s vaultStatus
		c.JSON(http.MethodGet, "/api/v1/status", nil, &s)
		return s.Vault.Reachable
	}
	it.Eventually(t, 60*time.Second, func() bool { return !reachable() }, "/status reports Vault unreachable")

	status, items := listItems(t, c, "v:alpha", "core", "v1", "pods", "demo")
	assert.Equal(t, http.StatusOK, status, "an open cluster keeps working")
	assert.NotEmpty(t, items)

	status, _, apiErr = open(t, c, "v:beta", "")
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "vault_unreachable", apiErr.Error.Code)

	it.Docker(t, "unpause", "kubelook-it-vault")
	it.Eventually(t, 60*time.Second, reachable, "/status reports Vault reachable again")
	it.Eventually(t, 30*time.Second, func() bool {
		s, _, _ := open(t, c, "v:beta", "")
		return s == http.StatusOK
	}, "opening beta works again")
}
