package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/auth"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/testutil"
)

const publicURL = "https://kubelook.example.com"

type fakeProvider struct{ name string }

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) OAuth2(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    "client-" + f.name,
		RedirectURL: redirectURL,
		Scopes:      []string{"openid", "email"},
		Endpoint:    oauth2.Endpoint{AuthURL: "https://idp.example.com/authorize", TokenURL: "https://idp.example.com/token"},
	}
}
func (f fakeProvider) Identify(context.Context, *oauth2.Token) (auth.Identity, error) {
	return auth.Identity{Provider: f.name}, nil
}

type env struct {
	h      *auth.Handlers
	mux    *http.ServeMux
	clock  *clock.Fake
	sealer *crypto.Sealer
}

func newEnv(t *testing.T, names ...string) *env {
	t.Helper()
	sealer, err := crypto.NewSealer(make([]byte, 32), testutil.Rand("seal"))
	require.NoError(t, err)
	fake := clock.NewFake(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	var ps []auth.Provider
	for _, n := range names {
		ps = append(ps, fakeProvider{n})
	}
	h := auth.NewHandlers(auth.Deps{
		Providers: ps, PublicURL: publicURL, Sealer: sealer, Clock: fake, Rand: testutil.Rand("state"), CookieSecure: true,
		Fail: func(w http.ResponseWriter, _ *http.Request, err error) {
			status := http.StatusInternalServerError
			if errors.Is(err, apperr.ErrNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return &env{h: h, mux: mux, clock: fake, sealer: sealer}
}

func (e *env) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

func oauthCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "kubelook_oauth" {
			return c
		}
	}
	t.Fatal("no kubelook_oauth cookie")
	return nil
}

func TestProviders_ListsConfiguredSorted(t *testing.T) {
	e := newEnv(t, "google", "github")
	w := e.get("/api/v1/auth/providers")
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"providers":["github","google"]}`, w.Body.String())
}

func TestProviders_NoneConfigured_EmptyList(t *testing.T) {
	e := newEnv(t)
	w := e.get("/api/v1/auth/providers")
	assert.JSONEq(t, `{"providers":[]}`, w.Body.String())
}

func TestLogin_RedirectsToProviderWithPKCEAndState(t *testing.T) {
	e := newEnv(t, "google")
	w := e.get("/auth/google/login")
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "idp.example.com", loc.Host)
	q := loc.Query()
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "client-google", q.Get("client_id"))
	assert.Equal(t, publicURL+"/auth/google/callback", q.Get("redirect_uri"))
	assert.Equal(t, "openid email", q.Get("scope"))
	assert.NotEmpty(t, q.Get("state"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))

	c := oauthCookie(t, w)
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/auth", c.Path)
	assert.Equal(t, 600, c.MaxAge)

	plain, err := e.sealer.Open(c.Value, crypto.AAD(crypto.PurposeOAuthState, "google"))
	require.NoError(t, err)
	var st struct {
		State     string    `json:"state"`
		Verifier  string    `json:"verifier"`
		CreatedAt time.Time `json:"createdAt"`
	}
	require.NoError(t, json.Unmarshal(plain, &st))
	assert.Equal(t, q.Get("state"), st.State)
	assert.Equal(t, e.clock.Now(), st.CreatedAt)
	sum := sha256.Sum256([]byte(st.Verifier))
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), q.Get("code_challenge"))
}

func TestLogin_TwoLogins_DifferentStateAndVerifier(t *testing.T) {
	e := newEnv(t, "google")
	a := e.get("/auth/google/login").Header().Get("Location")
	b := e.get("/auth/google/login").Header().Get("Location")
	assert.NotEqual(t, a, b)
}

func TestLogin_IgnoresRedirectParameter(t *testing.T) {
	e := newEnv(t, "google")
	w := e.get("/auth/google/login?redirect=https://evil.example.com")
	assert.NotContains(t, w.Header().Get("Location"), "evil.example.com")
}

func TestLogin_UnknownProvider_404(t *testing.T) {
	e := newEnv(t, "google")
	assert.Equal(t, http.StatusNotFound, e.get("/auth/gitlab/login").Code)
}

// startLogin performs a login and returns the cookie and the state sent to the provider.
func startLogin(t *testing.T, e *env, provider string) (*http.Cookie, string) {
	t.Helper()
	w := e.get("/auth/" + provider + "/login")
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return oauthCookie(t, w), loc.Query().Get("state")
}

func parse(e *env, provider, query string, c *http.Cookie) (code, verifier string, w *httptest.ResponseRecorder, err error) {
	r := httptest.NewRequest(http.MethodGet, "/auth/"+provider+"/callback?"+query, nil)
	if c != nil {
		r.AddCookie(c)
	}
	w = httptest.NewRecorder()
	code, verifier, err = auth.ParseCallback(e.h, w, r, provider)
	return
}

func TestParseCallback_Valid_ReturnsCodeAndVerifierAndClearsCookie(t *testing.T) {
	e := newEnv(t, "google")
	c, state := startLogin(t, e, "google")
	code, verifier, w, err := parse(e, "google", "code=abc&state="+state, c)
	require.NoError(t, err)
	assert.Equal(t, "abc", code)
	assert.NotEmpty(t, verifier)
	cleared := oauthCookie(t, w)
	assert.Equal(t, -1, cleared.MaxAge)
	assert.Equal(t, "/auth", cleared.Path)
}

func TestParseCallback_MissingCookie_ErrState(t *testing.T) {
	e := newEnv(t, "google")
	_, _, _, err := parse(e, "google", "code=abc&state=x", nil)
	assert.ErrorIs(t, err, auth.ErrState)
}

func TestParseCallback_StateMismatch_ErrState(t *testing.T) {
	e := newEnv(t, "google")
	c, _ := startLogin(t, e, "google")
	_, _, _, err := parse(e, "google", "code=abc&state=other", c)
	assert.ErrorIs(t, err, auth.ErrState)
}

func TestParseCallback_Older_Than_10Minutes_ErrState(t *testing.T) {
	e := newEnv(t, "google")
	c, state := startLogin(t, e, "google")
	e.clock.Advance(10*time.Minute + time.Second)
	_, _, _, err := parse(e, "google", "code=abc&state="+state, c)
	assert.ErrorIs(t, err, auth.ErrState)
}

func TestParseCallback_CookieSealedForOtherProvider_ErrState(t *testing.T) {
	e := newEnv(t, "google", "github")
	c, state := startLogin(t, e, "google")
	_, _, _, err := parse(e, "github", "code=abc&state="+state, c)
	assert.ErrorIs(t, err, auth.ErrState)
}

func TestParseCallback_AccessDenied_ErrDenied(t *testing.T) {
	e := newEnv(t, "google")
	c, state := startLogin(t, e, "google")
	_, _, _, err := parse(e, "google", "error=access_denied&state="+state, c)
	assert.ErrorIs(t, err, auth.ErrDenied)
}

func TestParseCallback_MissingCode_ErrState(t *testing.T) {
	e := newEnv(t, "google")
	c, state := startLogin(t, e, "google")
	_, _, _, err := parse(e, "google", "state="+state, c)
	assert.ErrorIs(t, err, auth.ErrState)
}

type notReadyProvider struct{ fakeProvider }

func (notReadyProvider) Ready(context.Context) error { return errors.New("discovery failed") }

func TestLogin_ProviderNotReady_FailsWithoutRedirectOrCookie(t *testing.T) {
	e := newEnv(t)
	h := auth.NewHandlers(auth.Deps{
		Providers: []auth.Provider{notReadyProvider{fakeProvider{"google"}}}, PublicURL: publicURL,
		Sealer: e.sealer, Clock: e.clock, Rand: testutil.Rand("state"), CookieSecure: true,
		Fail: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/google/login", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.Empty(t, w.Result().Cookies())
}
