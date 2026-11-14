package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/auth"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

// scriptedProvider exchanges codes against a loopback token endpoint and answers Identify as scripted.
type scriptedProvider struct {
	tokenURL string
	id       auth.Identity
	err      error
}

func (scriptedProvider) Name() string { return "google" }
func (p scriptedProvider) OAuth2(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: "c", ClientSecret: "s", RedirectURL: redirectURL,
		Endpoint: oauth2.Endpoint{AuthURL: "https://idp.example.com/authorize", TokenURL: p.tokenURL},
	}
}
func (p scriptedProvider) Identify(context.Context, *oauth2.Token) (auth.Identity, error) {
	return p.id, p.err
}

type signinEnv struct {
	handler  http.Handler
	provider *scriptedProvider
	sessions *session.Manager
	db       *store.DB
	users    *store.Users
	auditOut *bytes.Buffer
	logs     *bytes.Buffer
	tokenOK  bool
	clock    *clock.Fake
}

func newSigninEnv(t *testing.T) *signinEnv {
	t.Helper()
	e := &signinEnv{auditOut: &bytes.Buffer{}, logs: &bytes.Buffer{}, tokenOK: true}
	e.clock = clock.NewFake(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !e.tokenOK {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer"}`))
	}))
	t.Cleanup(tokenSrv.Close)
	e.provider = &scriptedProvider{
		tokenURL: tokenSrv.URL,
		id:       auth.Identity{Provider: "google", Subject: "sub-1", Email: "a@corp.example", Name: "Ada"},
	}

	e.db = storetest.New(t)
	n := 0
	newID := func() string { n++; return fmt.Sprintf("id-%d", n) }
	e.users = store.NewUsers(e.db, e.clock, newID)
	log := slog.New(slog.NewJSONHandler(e.logs, nil))
	e.sessions = session.NewManager(session.Deps{
		Store: store.NewSessions(e.db), Clock: e.clock, Rand: testutil.Rand("sessions"), Log: log,
		IdleTimeout: 30 * time.Minute, MaxLifetime: 12 * time.Hour,
	})
	sealer, err := crypto.NewSealer(make([]byte, 32), testutil.Rand("seal"))
	require.NoError(t, err)
	h := auth.NewHandlers(auth.Deps{
		Providers: []auth.Provider{e.provider}, PublicURL: publicURL, Sealer: sealer, Clock: e.clock,
		Rand: testutil.Rand("state"), CookieSecure: true,
		Users: e.users, Sessions: e.sessions, Audit: audit.NewRecorder(e.db, e.clock, e.auditOut, newID), Log: log,
		Fail: func(w http.ResponseWriter, r *http.Request, err error) { httpapi.WriteError(w, r, log, err) },
	})
	e.handler = httpapi.New(httpapi.Deps{
		Config: &config.Config{Server: config.Server{PublicURL: publicURL}}, Clock: e.clock, Log: log,
		WebFS: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, Sessions: e.sessions, Users: e.users, Auth: h,
		KeyID: sealer.KeyID(),
	})
	return e
}

func (e *signinEnv) do(r *http.Request, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// signIn runs login then the callback and returns the callback response. mutate may change the callback query.
func (e *signinEnv) signIn(t *testing.T, existing *http.Cookie, mutate func(url.Values)) *httptest.ResponseRecorder {
	t.Helper()
	var cs []*http.Cookie
	if existing != nil {
		cs = append(cs, existing)
	}
	login := e.do(httptest.NewRequest(http.MethodGet, "/auth/google/login", nil), cs...)
	require.Equal(t, http.StatusFound, login.Code)
	loc, err := url.Parse(login.Header().Get("Location"))
	require.NoError(t, err)
	q := url.Values{"code": {"the-code"}, "state": {loc.Query().Get("state")}}
	if mutate != nil {
		mutate(q)
	}
	cs = append(cs, cookieNamed(login, "kubelook_oauth"))
	return e.do(httptest.NewRequest(http.MethodGet, "/auth/google/callback?"+q.Encode(), nil), cs...)
}

func (e *signinEnv) auditLines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(e.auditOut.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		out = append(out, m)
	}
	return out
}

func (e *signinEnv) sessionCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.QueryRow(t.Context(), "SELECT COUNT(*) FROM sessions").Scan(&n))
	return n
}

func TestCallback_Success_CreatesUserAndSessionAndAudits(t *testing.T) {
	e := newSigninEnv(t)
	w := e.signIn(t, nil, nil)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))

	sc := cookieNamed(w, "kubelook_session")
	require.NotNil(t, sc)
	assert.True(t, sc.HttpOnly)
	assert.Equal(t, 1, e.sessionCount(t))
	assert.Empty(t, cookieNamed(w, "kubelook_oauth").Value, "the state cookie is cleared")

	me := e.do(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), &http.Cookie{Name: "kubelook_session", Value: sc.Value})
	require.Equal(t, http.StatusOK, me.Code)
	assert.Contains(t, me.Body.String(), `"email":"a@corp.example"`)

	lines := e.auditLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, "sign-in", lines[0]["verb"])
	assert.Equal(t, "success", lines[0]["outcome"])
	assert.Equal(t, map[string]any{"provider": "google", "subject": "sub-1", "email": "a@corp.example"}, lines[0]["account"])
}

func TestCallback_RepeatedSignIn_UpdatesTheSameUser(t *testing.T) {
	e := newSigninEnv(t)
	e.signIn(t, nil, nil)
	e.provider.id.Name = "Ada L."
	e.signIn(t, nil, nil)
	var n int
	require.NoError(t, e.db.QueryRow(t.Context(), "SELECT COUNT(*) FROM users").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestCallback_Failures_RedirectWithFixedReasonAndNoSession(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*signinEnv)
		mutate func(url.Values)
		want   string
		reason string
	}{
		{"not allowed", func(e *signinEnv) { e.provider.err = fmt.Errorf("account x refused: %w", auth.ErrNotAllowed) }, nil, "not_allowed", "not_allowed"},
		{"state mismatch", nil, func(q url.Values) { q.Set("state", "forged") }, "state", "state"},
		{"denied", nil, func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }, "denied", "denied"},
		{"other error", func(e *signinEnv) { e.provider.err = errors.New("secret-detail boom") }, nil, "failed", "failed"},
		{"exchange fails", func(e *signinEnv) { e.tokenOK = false }, nil, "failed", "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSigninEnv(t)
			if tc.setup != nil {
				tc.setup(e)
			}
			w := e.signIn(t, nil, tc.mutate)
			require.Equal(t, http.StatusFound, w.Code)
			assert.Equal(t, "/?signin_error="+tc.want, w.Header().Get("Location"))
			assert.Nil(t, cookieNamed(w, "kubelook_session"))
			assert.Equal(t, 0, e.sessionCount(t))
			lines := e.auditLines(t)
			require.Len(t, lines, 1)
			assert.Equal(t, "sign-in", lines[0]["verb"])
			assert.Equal(t, "failure", lines[0]["outcome"])
			assert.Equal(t, tc.reason, lines[0]["details"].(map[string]any)["reason"])
			assert.NotContains(t, w.Header().Get("Location"), "boom", "provider error text never reaches the URL")
		})
	}
}

func TestCallback_OtherErrorDetailsOnlyInTheLog(t *testing.T) {
	e := newSigninEnv(t)
	e.provider.err = errors.New("secret-detail boom")
	e.signIn(t, nil, nil)
	assert.Contains(t, e.logs.String(), "secret-detail boom")
	assert.NotContains(t, e.auditOut.String(), "boom")
}

func TestCallback_AlreadySignedIn_EndsTheOldSessionFirst(t *testing.T) {
	e := newSigninEnv(t)
	first := cookieNamed(e.signIn(t, nil, nil), "kubelook_session")
	require.NotNil(t, first)
	second := cookieNamed(e.signIn(t, &http.Cookie{Name: "kubelook_session", Value: first.Value}, nil), "kubelook_session")
	require.NotNil(t, second)
	assert.NotEqual(t, first.Value, second.Value)
	assert.Equal(t, 1, e.sessionCount(t), "the old session row is gone")
}

func TestLogout_EndsSessionClearsCookieAndAudits(t *testing.T) {
	e := newSigninEnv(t)
	sc := cookieNamed(e.signIn(t, nil, nil), "kubelook_session")
	cookie := &http.Cookie{Name: "kubelook_session", Value: sc.Value}
	me := e.do(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), cookie)
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	require.NoError(t, json.Unmarshal(me.Body.Bytes(), &body))

	closed := 0
	hash := session.Hash(sc.Value)
	e.sessions.Registry().Register(hash, "test-stream", func() error { closed++; return nil })

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.Header.Set("X-CSRF-Token", body.CSRFToken)
	w := e.do(r, cookie)
	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, -1, cookieNamed(w, "kubelook_session").MaxAge)
	assert.Equal(t, 0, e.sessionCount(t))
	assert.Equal(t, 1, closed)

	lines := e.auditLines(t)
	require.Len(t, lines, 2)
	assert.Equal(t, "sign-out", lines[1]["verb"])
	assert.Equal(t, "success", lines[1]["outcome"])
	assert.Equal(t, "a@corp.example", lines[1]["account"].(map[string]any)["email"])
}

func TestLogout_WithoutCSRFToken_403(t *testing.T) {
	e := newSigninEnv(t)
	sc := cookieNamed(e.signIn(t, nil, nil), "kubelook_session")
	w := e.do(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil), &http.Cookie{Name: "kubelook_session", Value: sc.Value})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 1, e.sessionCount(t))
}

func TestLogout_WithoutSession_401(t *testing.T) {
	e := newSigninEnv(t)
	w := e.do(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
