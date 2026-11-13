package session_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

type env struct {
	m      *session.Manager
	fake   *clock.Fake
	db     *store.DB
	store  *store.Sessions
	userID string
}

func newEnv(t *testing.T, secure bool) env {
	t.Helper()
	db := storetest.New(t)
	fake := clock.NewFake(t0)
	u, err := store.NewUsers(db, fake, func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e@x", "n")
	require.NoError(t, err)
	st := store.NewSessions(db)
	m := session.NewManager(session.Deps{
		Store: st, Clock: fake, Rand: testutil.Rand("seed"), Log: nopLogger(),
		IdleTimeout: 30 * time.Minute, MaxLifetime: 12 * time.Hour, CookieSecure: secure,
	})
	return env{m: m, fake: fake, db: db, store: st, userID: u.ID}
}

func (e env) request(id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	if id != "" {
		r.AddCookie(&http.Cookie{Name: "kubelook_session", Value: id})
	}
	return r
}

func TestCreate_IDAndCSRFAre43CharBase64URL_StoreHoldsOnlyHash(t *testing.T) {
	e := newEnv(t, false)
	id, s, err := e.m.Create(t.Context(), e.userID)
	require.NoError(t, err)

	b64 := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	assert.Regexp(t, b64, id)
	assert.Regexp(t, b64, s.CSRFToken)
	assert.NotEqual(t, id, s.CSRFToken)

	sum := sha256.Sum256([]byte(id))
	assert.Equal(t, hex.EncodeToString(sum[:]), session.Hash(id))
	assert.Equal(t, session.Hash(id), s.Hash)
	assert.Equal(t, e.userID, s.UserID)
	assert.Equal(t, t0, s.CreatedAt)
	assert.Equal(t, t0.Add(12*time.Hour), s.ExpiresAt)

	var stored string
	require.NoError(t, e.db.QueryRow(t.Context(), "SELECT id_hash FROM sessions").Scan(&stored))
	assert.Equal(t, session.Hash(id), stored)
	var n int
	require.NoError(t, e.db.QueryRow(t.Context(), "SELECT COUNT(*) FROM sessions WHERE id_hash = ? OR csrf_token = ?", id, id).Scan(&n))
	assert.Zero(t, n, "the plain session ID is never stored")
}

func TestCreate_TwoSessionsDiffer(t *testing.T) {
	e := newEnv(t, false)
	a, _, _ := e.m.Create(t.Context(), e.userID)
	b, _, _ := e.m.Create(t.Context(), e.userID)
	assert.NotEqual(t, a, b)
}

func TestCreate_RandomSourceFails_ReturnsError(t *testing.T) {
	e := newEnv(t, false)
	m := session.NewManager(session.Deps{Store: e.store, Clock: e.fake, Rand: failingReader{}, Log: nopLogger(), IdleTimeout: time.Minute, MaxLifetime: time.Hour})
	_, _, err := m.Create(t.Context(), e.userID)
	assert.Error(t, err)
}

func TestCreate_UnknownUser_ReturnsError(t *testing.T) {
	_, _, err := newEnv(t, false).m.Create(t.Context(), "ghost")
	assert.Error(t, err)
}

func TestSetCookie_Attributes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		e := newEnv(t, secure)
		rec := httptest.NewRecorder()
		e.m.SetCookie(rec, "the-id")
		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		c := cookies[0]
		assert.Equal(t, "kubelook_session", c.Name)
		assert.Equal(t, "the-id", c.Value)
		assert.Equal(t, "/", c.Path)
		assert.True(t, c.HttpOnly)
		assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
		assert.Equal(t, secure, c.Secure)
		assert.Zero(t, c.MaxAge, "session cookie")
		assert.True(t, c.Expires.IsZero(), "session cookie")
	}
}

func TestClearCookie_ExpiresTheCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	newEnv(t, true).m.ClearCookie(rec)
	c := rec.Result().Cookies()[0]
	assert.Equal(t, "kubelook_session", c.Name)
	assert.Empty(t, c.Value)
	assert.Equal(t, -1, c.MaxAge)
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
}

func TestLoad_ValidCookie_ReturnsSession(t *testing.T) {
	e := newEnv(t, false)
	id, created, _ := e.m.Create(t.Context(), e.userID)
	got, err := e.m.Load(t.Context(), e.request(id))
	require.NoError(t, err)
	assert.Equal(t, created, got)
}

func TestLoad_NoCookieOrUnknownCookie_ErrUnauthenticated(t *testing.T) {
	e := newEnv(t, false)
	for _, id := range []string{"", "not-a-session"} {
		_, err := e.m.Load(t.Context(), e.request(id))
		assert.ErrorIs(t, err, apperr.ErrUnauthenticated, id)
	}
}

func TestLoad_TouchesLastSeenAtMostOncePerMinute(t *testing.T) {
	e := newEnv(t, false)
	id, s, _ := e.m.Create(t.Context(), e.userID)
	lastSeen := func() time.Time {
		row, err := e.store.Get(t.Context(), s.Hash)
		require.NoError(t, err)
		return row.LastSeenAt
	}

	e.fake.Advance(10 * time.Second)
	_, err := e.m.Load(t.Context(), e.request(id))
	require.NoError(t, err)
	assert.Equal(t, t0, lastSeen(), "10 s after the last touch: no write")

	e.fake.Advance(10 * time.Second)
	_, _ = e.m.Load(t.Context(), e.request(id))
	assert.Equal(t, t0, lastSeen())

	e.fake.Advance(41 * time.Second) // 61 s since the creation
	_, _ = e.m.Load(t.Context(), e.request(id))
	assert.Equal(t, t0.Add(61*time.Second), lastSeen())
}

func TestLoad_IdleTimeout_EndsSession(t *testing.T) {
	e := newEnv(t, false)
	id, s, _ := e.m.Create(t.Context(), e.userID)
	e.fake.Advance(31 * time.Minute)
	_, err := e.m.Load(t.Context(), e.request(id))
	assert.ErrorIs(t, err, apperr.ErrUnauthenticated)
	_, err = e.store.Get(t.Context(), s.Hash)
	assert.ErrorIs(t, err, store.ErrNotFound, "the row is deleted")
}

func TestLoad_ActiveSessionSurvivesIdleButNotAbsoluteLifetime(t *testing.T) {
	e := newEnv(t, false)
	id, _, _ := e.m.Create(t.Context(), e.userID)
	for i := 0; i < 71; i++ { // up to 11 h 50 min, a load every 10 minutes
		e.fake.Advance(10 * time.Minute)
		_, err := e.m.Load(t.Context(), e.request(id))
		require.NoError(t, err, i)
	}
	e.fake.Advance(11 * time.Minute) // 12 h 1 min
	_, err := e.m.Load(t.Context(), e.request(id))
	assert.ErrorIs(t, err, apperr.ErrUnauthenticated)
}

func TestKeepAlive_TouchesLikeARequest(t *testing.T) {
	e := newEnv(t, false)
	id, s, _ := e.m.Create(t.Context(), e.userID)
	e.fake.Advance(20 * time.Minute)
	e.m.KeepAlive(t.Context(), s.Hash)
	e.fake.Advance(20 * time.Minute) // 40 min since creation, 20 since the keep-alive
	_, err := e.m.Load(t.Context(), e.request(id))
	assert.NoError(t, err)

	e.m.KeepAlive(t.Context(), "unknown-hash") // must not panic or fail
}

func TestEnd_DeletesSessionAndCredentials_RunsClosersOnceInOrder(t *testing.T) {
	e := newEnv(t, false)
	_, s, _ := e.m.Create(t.Context(), e.userID)
	require.NoError(t, e.store.PutCredential(t.Context(), s.Hash, store.CredentialRow{Kind: store.CredVaultToken, Sealed: "x"}, t0))

	var order []string
	boomA, boomC := errors.New("boom a"), errors.New("boom c")
	reg := e.m.Registry()
	reg.Register(s.Hash, "a", func() error { order = append(order, "a"); return boomA })
	reg.Register(s.Hash, "b", func() error { order = append(order, "b"); return nil })
	reg.Register(s.Hash, "c", func() error { order = append(order, "c"); return boomC })

	err := e.m.End(t.Context(), s.Hash)
	assert.ErrorIs(t, err, boomA)
	assert.ErrorIs(t, err, boomC)
	assert.Equal(t, []string{"a", "b", "c"}, order)

	_, err = e.store.Get(t.Context(), s.Hash)
	assert.ErrorIs(t, err, store.ErrNotFound)
	_, err = e.store.GetCredential(t.Context(), s.Hash, store.CredVaultToken, "")
	assert.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, e.m.End(t.Context(), s.Hash))
	assert.Equal(t, []string{"a", "b", "c"}, order, "closers run exactly once")
}

func TestEnd_StoreFailure_StillRunsClosersAndReportsError(t *testing.T) {
	e := newEnv(t, false)
	_, s, _ := e.m.Create(t.Context(), e.userID)
	ran := false
	e.m.Registry().Register(s.Hash, "a", func() error { ran = true; return nil })
	require.NoError(t, e.db.Close())
	assert.Error(t, e.m.End(t.Context(), s.Hash))
	assert.True(t, ran)
}

func TestRunWatchdog_ClosesRegistryEntriesOfSessionsEndedElsewhere(t *testing.T) {
	e := newEnv(t, false)
	_, alive, _ := e.m.Create(t.Context(), e.userID)
	_, gone, _ := e.m.Create(t.Context(), e.userID)
	closed := make(chan string, 2)
	e.m.Registry().Register(alive.Hash, "alive", func() error { closed <- "alive"; return nil })
	e.m.Registry().Register(gone.Hash, "gone", func() error { closed <- "gone"; return nil })

	// simulate logout on another replica: the row disappears without End on this one
	require.NoError(t, e.store.Delete(t.Context(), gone.Hash))

	ctx, cancel := contextWithCancel(t)
	done := runAsync(func() { e.m.RunWatchdog(ctx) })
	e.fake.BlockUntil(1)
	e.fake.Advance(5 * time.Second)

	assert.Equal(t, "gone", testutil.Recv(t, closed))
	cancel()
	testutil.Recv(t, done)
	testutil.NoRecv(t, closed)
	assert.Equal(t, 1, e.m.Registry().Count(alive.Hash))
}

func TestRunWatchdog_NothingRegistered_DoesNotQueryOrFail(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := contextWithCancel(t)
	done := runAsync(func() { e.m.RunWatchdog(ctx) })
	e.fake.BlockUntil(1)
	e.fake.Advance(5 * time.Second)
	cancel()
	testutil.Recv(t, done)
}

func TestRunSweep_DeletesExpiredSessionsAndClosesTheirEntries(t *testing.T) {
	e := newEnv(t, false)
	_, s, _ := e.m.Create(t.Context(), e.userID)
	closed := make(chan struct{}, 1)
	e.m.Registry().Register(s.Hash, "stream", func() error { closed <- struct{}{}; return nil })

	ctx, cancel := contextWithCancel(t)
	done := runAsync(func() { e.m.RunSweep(ctx) })
	e.fake.BlockUntil(1)
	e.fake.Advance(31 * time.Minute) // fires the 1-minute ticker; the session is idle by then
	testutil.Recv(t, closed)

	cancel()
	testutil.Recv(t, done)
	_, err := e.store.Get(t.Context(), s.Hash)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunSweep_StoreFailure_IsLoggedAndLoopContinues(t *testing.T) {
	e := newEnv(t, false)
	require.NoError(t, e.db.Close())
	ctx, cancel := contextWithCancel(t)
	done := runAsync(func() { e.m.RunSweep(ctx) })
	e.fake.BlockUntil(1)
	e.fake.Advance(time.Minute)
	e.fake.Advance(time.Minute)
	cancel()
	testutil.Recv(t, done)
}
