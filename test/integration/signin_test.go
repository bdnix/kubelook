//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

func boolPtr(b bool) *bool { return &b }

type meBody struct {
	User struct {
		ID, Provider, Email string
	} `json:"user"`
	CSRFToken string `json:"csrfToken"`
}

func me(t *testing.T, c *it.Client) (int, meBody) {
	t.Helper()
	var m meBody
	status := c.JSON(http.MethodGet, "/api/v1/me", nil, &m)
	return status, m
}

func TestSignIn_GoogleAllowedDomain_SignsInWithASecureCookieAndAStableUserID(t *testing.T) {
	c := it.NewClient(t)
	final := c.SignInGoogle(it.Account{Sub: "si-g-1", Email: "first@corp.example", HD: "corp.example"})
	require.Empty(t, final.Query().Get("signin_error"), "final URL %s", final)

	ck := c.SetCookie("kubelook_session")
	require.NotNil(t, ck, "the callback sets the session cookie")
	assert.True(t, ck.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)

	status, first := me(t, c)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "first@corp.example", first.User.Email)

	// the same Google account again, with another e-mail address: the same user
	c2 := it.NewClient(t)
	c2.SignInGoogle(it.Account{Sub: "si-g-1", Email: "second@corp.example", HD: "corp.example"})
	status, second := me(t, c2)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, first.User.ID, second.User.ID, "the user ID follows the provider subject, not the e-mail")
	assert.Equal(t, "second@corp.example", second.User.Email)
}

func TestSignIn_GoogleRefused_RedirectsWithNotAllowedAndStaysSignedOut(t *testing.T) {
	cases := map[string]it.Account{
		"no hosted domain":      {Sub: "si-g-2", Email: "a@gmail.example"},
		"another hosted domain": {Sub: "si-g-3", Email: "a@other.example", HD: "other.example"},
		"unverified e-mail":     {Sub: "si-g-4", Email: "a@corp.example", HD: "corp.example", EmailVerified: boolPtr(false)},
	}
	for name, acct := range cases {
		t.Run(name, func(t *testing.T) {
			c := it.NewClient(t)
			final := c.SignInGoogle(acct)
			assert.Equal(t, "not_allowed", final.Query().Get("signin_error"))
			status, _ := me(t, c)
			assert.Equal(t, http.StatusUnauthorized, status)
		})
	}
}

func TestSignIn_GitHub_OnlyActiveMembersOfAnAllowedOrganization(t *testing.T) {
	t.Run("member", func(t *testing.T) {
		c := it.NewClient(t)
		final := c.SignInGitHub(it.Account{Sub: "si-h-1", GitHubID: 9101, Email: "m@x.example", Name: "Em", GitHubOrgs: []string{"acme"}})
		require.Empty(t, final.Query().Get("signin_error"), "final URL %s", final)
		status, m := me(t, c)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "github", m.User.Provider)
	})
	t.Run("not a member", func(t *testing.T) {
		c := it.NewClient(t)
		final := c.SignInGitHub(it.Account{Sub: "si-h-2", GitHubID: 9102, GitHubOrgs: []string{"elsewhere"}})
		assert.Equal(t, "not_allowed", final.Query().Get("signin_error"))
		status, _ := me(t, c)
		assert.Equal(t, http.StatusUnauthorized, status)
	})
	t.Run("organization restricts the OAuth app (403)", func(t *testing.T) {
		c := it.NewClient(t)
		final := c.SignInGitHub(it.Account{Sub: "si-h-3", GitHubID: 9103, GitHubOrgs: []string{"acme"}, Org403: true})
		assert.Equal(t, "not_allowed", final.Query().Get("signin_error"))
	})
}

func TestSignIn_CallbackWithAWrongState_IsRefused(t *testing.T) {
	c := it.NewClient(t)
	// the first hop sets the state cookie; the callback then arrives with a state that is not ours
	req, err := http.NewRequest(http.MethodGet, it.BaseURL()+"/auth/google/login", nil)
	require.NoError(t, err)
	resp, err := c.NoFollow().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)

	resp2, err := c.HC.Get(it.BaseURL() + "/auth/google/callback?code=forged&state=not-the-state") //nolint:noctx // test helper
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, "state", resp2.Request.URL.Query().Get("signin_error"))
	status, _ := me(t, c)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestSignIn_MutatingCallsNeedTheCSRFToken(t *testing.T) {
	c := it.NewClient(t)
	c.SignInGoogle(it.Account{Sub: "si-csrf", Email: "c@corp.example", HD: "corp.example"})

	status, body := c.Do(http.MethodPut, "/api/v1/settings", []byte(`{}`), map[string]string{"X-CSRF-Token": ""})
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, string(body), `"csrf"`)

	status, _ = c.Do(http.MethodPut, "/api/v1/settings", []byte(`{}`), nil)
	assert.NotEqual(t, http.StatusForbidden, status, "with the token the call is not refused for CSRF")
}

func TestSignIn_Logout_EndsTheSession(t *testing.T) {
	c := it.NewClient(t)
	c.SignInGoogle(it.Account{Sub: "si-out", Email: "o@corp.example", HD: "corp.example"})
	require.Equal(t, http.StatusNoContent, c.JSON(http.MethodPost, "/api/v1/auth/logout", nil, nil))
	status, _ := me(t, c)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestSignIn_Audit_RecordsSignInAndSignOutAndNeverTheClientSecret(t *testing.T) {
	c := it.NewClient(t)
	c.SignInGoogle(it.Account{Sub: "si-aud-ok", Email: "aud-ok@corp.example", HD: "corp.example"})
	c.JSON(http.MethodPost, "/api/v1/auth/logout", nil, nil)
	bad := it.NewClient(t)
	bad.SignInGoogle(it.Account{Sub: "si-aud-bad", Email: "aud-bad@gmail.example"})

	lines := it.AuditLines(t)
	var okIn, failIn, out bool
	for _, l := range lines {
		switch {
		case l.Verb == "sign-in" && l.Outcome == "success" && l.Account["email"] == "aud-ok@corp.example":
			okIn = true
		case l.Verb == "sign-in" && l.Outcome == "failure" && l.Details["reason"] == "not_allowed":
			failIn = true
		case l.Verb == "sign-out" && l.Account["email"] == "aud-ok@corp.example":
			out = true
		}
	}
	assert.True(t, okIn, "a successful sign-in is audited")
	assert.True(t, failIn, "a refused sign-in is audited with its reason")
	assert.True(t, out, "sign-out is audited")
	assert.False(t, strings.Contains(it.Logs(t), "it-secret"), "the client secret never reaches the log")
}
