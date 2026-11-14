package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/bdnix/kubelook/internal/auth"
)

type fakeGitHub struct {
	srv *httptest.Server

	mu          sync.Mutex
	userStatus  int
	userBody    string
	memberships map[string]struct {
		status int
		body   string
	}
	requests []*http.Request
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{
		userStatus: http.StatusOK,
		userBody:   `{"id":42,"login":"octo","name":"Octo","email":"o@x.example"}`,
		memberships: map[string]struct {
			status int
			body   string
		}{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Clone(context.Background()))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/user":
			w.WriteHeader(f.userStatus)
			_, _ = w.Write([]byte(f.userBody))
		case strings.HasPrefix(r.URL.Path, "/user/memberships/orgs/"):
			org := strings.TrimPrefix(r.URL.Path, "/user/memberships/orgs/")
			m, ok := f.memberships[org]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			w.WriteHeader(m.status)
			_, _ = w.Write([]byte(m.body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) member(org string, status int, body string) {
	f.memberships[org] = struct {
		status int
		body   string
	}{status, body}
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeGitHub) provider(restrict bool, orgs ...string) auth.Provider {
	return auth.NewGitHub(auth.GitHubConfig{
		ClientID: "gh-client", ClientSecret: "gh-secret", BaseURL: "https://github.example",
		APIBaseURL: f.srv.URL, AllowedOrgs: orgs, Restrict: restrict,
	}, f.srv.Client())
}

var ghToken = &oauth2.Token{AccessToken: "gho_secret"}

func TestGitHub_ActiveMember_ReturnsIdentityWithNumericSubject(t *testing.T) {
	f := newFakeGitHub(t)
	f.member("acme", http.StatusOK, `{"state":"active"}`)
	id, err := f.provider(true, "acme").Identify(context.Background(), ghToken)
	require.NoError(t, err)
	assert.Equal(t, auth.Identity{Provider: "github", Subject: "42", Email: "o@x.example", Name: "Octo"}, id)
}

func TestGitHub_Name(t *testing.T) {
	assert.Equal(t, "github", newFakeGitHub(t).provider(false).Name())
}

func TestGitHub_PendingMembership_NotAllowed(t *testing.T) {
	f := newFakeGitHub(t)
	f.member("acme", http.StatusOK, `{"state":"pending"}`)
	_, err := f.provider(true, "acme").Identify(context.Background(), ghToken)
	require.ErrorIs(t, err, auth.ErrNotAllowed)
}

func TestGitHub_NotAMemberOfAnyListedOrg_NotAllowed(t *testing.T) {
	f := newFakeGitHub(t)
	_, err := f.provider(true, "acme", "other").Identify(context.Background(), ghToken)
	require.ErrorIs(t, err, auth.ErrNotAllowed)
}

func TestGitHub_MemberOfSecondOrg_Allowed(t *testing.T) {
	f := newFakeGitHub(t)
	f.member("second", http.StatusOK, `{"state":"active"}`)
	_, err := f.provider(true, "first", "second").Identify(context.Background(), ghToken)
	require.NoError(t, err)
}

func TestGitHub_Membership403_NotAllowedWithOwnerHint(t *testing.T) {
	f := newFakeGitHub(t)
	f.member("acme", http.StatusForbidden, `{"message":"restricted"}`)
	_, err := f.provider(true, "acme").Identify(context.Background(), ghToken)
	require.ErrorIs(t, err, auth.ErrNotAllowed)
	assert.Contains(t, err.Error(), "an organization owner must approve this OAuth app")
}

func TestGitHub_EmptyAllowlistWithRestrict_NotAllowed(t *testing.T) {
	f := newFakeGitHub(t)
	_, err := f.provider(true).Identify(context.Background(), ghToken)
	require.ErrorIs(t, err, auth.ErrNotAllowed)
}

func TestGitHub_RestrictOff_NoMembershipCallsAndMinimalScopes(t *testing.T) {
	f := newFakeGitHub(t)
	p := f.provider(false, "acme")
	_, err := p.Identify(context.Background(), ghToken)
	require.NoError(t, err)
	assert.Equal(t, 1, f.count(), "only GET /user")
	assert.Equal(t, []string{"read:user", "user:email"}, p.OAuth2("https://k/cb").Scopes)
}

func TestGitHub_RestrictOn_ScopesIncludeReadOrg(t *testing.T) {
	f := newFakeGitHub(t)
	assert.Contains(t, f.provider(true, "acme").OAuth2("https://k/cb").Scopes, "read:org")
}

func TestGitHub_OAuth2Config(t *testing.T) {
	cfg := newFakeGitHub(t).provider(false).OAuth2("https://k/cb")
	assert.Equal(t, "gh-client", cfg.ClientID)
	assert.Equal(t, "gh-secret", cfg.ClientSecret)
	assert.Equal(t, "https://k/cb", cfg.RedirectURL)
	assert.Equal(t, "https://github.example/login/oauth/authorize", cfg.Endpoint.AuthURL)
	assert.Equal(t, "https://github.example/login/oauth/access_token", cfg.Endpoint.TokenURL)
}

func TestGitHub_UserNon200_Error(t *testing.T) {
	f := newFakeGitHub(t)
	f.userStatus = http.StatusInternalServerError
	_, err := f.provider(false).Identify(context.Background(), ghToken)
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrNotAllowed)
}

func TestGitHub_UserBodyNotJSON_Error(t *testing.T) {
	f := newFakeGitHub(t)
	f.userBody = `not json`
	_, err := f.provider(false).Identify(context.Background(), ghToken)
	require.Error(t, err)
}

func TestGitHub_MembershipServerError_Error(t *testing.T) {
	f := newFakeGitHub(t)
	f.member("acme", http.StatusInternalServerError, `{}`)
	_, err := f.provider(true, "acme").Identify(context.Background(), ghToken)
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrNotAllowed)
}

func TestGitHub_NullEmail_EmptyAndAllowed(t *testing.T) {
	f := newFakeGitHub(t)
	f.userBody = `{"id":7,"login":"octo","name":null,"email":null}`
	id, err := f.provider(false).Identify(context.Background(), ghToken)
	require.NoError(t, err)
	assert.Equal(t, auth.Identity{Provider: "github", Subject: "7"}, id)
}

func TestGitHub_RequestHeaders(t *testing.T) {
	f := newFakeGitHub(t)
	f.member("acme", http.StatusOK, `{"state":"active"}`)
	_, err := f.provider(true, "acme").Identify(context.Background(), ghToken)
	require.NoError(t, err)
	require.Equal(t, 2, f.count())
	for _, r := range f.requests {
		assert.Equal(t, "Bearer gho_secret", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Equal(t, "2022-11-28", r.Header.Get("X-GitHub-Api-Version"))
	}
}

func TestGitHub_HugeResponse_IsLimited(t *testing.T) {
	f := newFakeGitHub(t)
	f.userBody = `{"id":1,"name":"` + strings.Repeat("x", 2<<20) + `"}`
	_, err := f.provider(false).Identify(context.Background(), ghToken)
	require.Error(t, err, "a body over 1 MiB is cut off and fails to decode")
}
