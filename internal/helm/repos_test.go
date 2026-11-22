package helm_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/egress"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

var t0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// world is a database with one user and two sessions.
type world struct {
	db       *store.DB
	sessions *store.Sessions
	sealer   *crypto.Sealer
	clock    *clock.Fake
	user     string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := storetest.New(t)
	u, err := store.NewUsers(db, clock.NewFake(t0), func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e@x", "n")
	require.NoError(t, err)
	sealer, err := crypto.NewSealer(bytes.Repeat([]byte{7}, 32), testutil.Rand("helm"))
	require.NoError(t, err)
	w := &world{db: db, sessions: store.NewSessions(db), sealer: sealer, clock: clock.NewFake(t0), user: u.ID}
	for _, h := range []string{"h1", "h2"} {
		require.NoError(t, w.sessions.Create(t.Context(), h, u.ID, "csrf", t0, t0.Add(time.Hour)))
	}
	return w
}

func (w *world) session(hash string) session.Session {
	return session.Session{Hash: hash, UserID: w.user}
}

func (w *world) repos(org ...config.HelmRepo) *helm.Repos {
	return &helm.Repos{Org: org, Sessions: w.sessions, Sealer: w.sealer, Clock: w.clock}
}

func TestRepos_ListsOrgAndSessionRepositoriesWithoutPasswords(t *testing.T) {
	w := newWorld(t)
	r := w.repos(config.HelmRepo{Name: "bitnami", URL: "https://charts.example.org"})
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "mine", "https://charts.internal", "alice", "s3cret"))
	got, err := r.List(t.Context(), w.session("h1"))
	require.NoError(t, err)
	assert.Equal(t, []helm.RepoInfo{
		{Name: "bitnami", URL: "https://charts.example.org", Scope: "org"},
		{Name: "mine", URL: "https://charts.internal", Scope: "session", HasAuth: true},
	}, got)
	raw, _ := json.Marshal(got)
	assert.NotContains(t, string(raw), "s3cret")
}

func TestRepos_SessionRepositoriesAreNotSharedBetweenSessions(t *testing.T) {
	w := newWorld(t)
	r := w.repos()
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "mine", "https://charts.internal", "", ""))
	got, err := r.List(t.Context(), w.session("h2"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRepos_Resolve_HasTheCredentials(t *testing.T) {
	w := newWorld(t)
	r := w.repos(config.HelmRepo{Name: "bitnami", URL: "https://charts.example.org"})
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "mine", "https://charts.internal", "alice", "s3cret"))
	mine, err := r.Resolve(t.Context(), w.session("h1"), "mine")
	require.NoError(t, err)
	assert.Equal(t, helm.Repository{Name: "mine", URL: "https://charts.internal", Username: "alice", Password: "s3cret", Session: true}, mine)
	org, err := r.Resolve(t.Context(), w.session("h1"), "bitnami")
	require.NoError(t, err)
	assert.Equal(t, helm.Repository{Name: "bitnami", URL: "https://charts.example.org"}, org)
	_, err = r.Resolve(t.Context(), w.session("h1"), "nope")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestRepos_TheSealedCopyIsBoundToTheSession(t *testing.T) {
	w := newWorld(t)
	r := w.repos()
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "mine", "https://charts.internal", "alice", "s3cret"))
	row, err := w.sessions.GetCredential(t.Context(), "h1", store.CredHelmRepo, "mine")
	require.NoError(t, err)
	assert.NotContains(t, row.Sealed, "s3cret")
	require.NoError(t, w.sessions.PutCredential(t.Context(), "h2", store.CredentialRow{Kind: store.CredHelmRepo, Ref: "mine", Sealed: row.Sealed}, t0))
	_, err = r.Resolve(t.Context(), w.session("h2"), "mine")
	assert.Error(t, err, "a copy moved to another session cannot be opened")
}

func TestRepos_AddRefusesBadInput(t *testing.T) {
	w := newWorld(t)
	r := w.repos(config.HelmRepo{Name: "bitnami", URL: "https://charts.example.org"})
	s := w.session("h1")
	assert.ErrorIs(t, r.Add(t.Context(), s, "Bad Name", "https://x", "", ""), apperr.ErrInvalid)
	assert.ErrorIs(t, r.Add(t.Context(), s, "ok", "ftp://x", "", ""), apperr.ErrInvalid)
	assert.ErrorIs(t, r.Add(t.Context(), s, "ok", "https://", "", ""), apperr.ErrInvalid)
	assert.ErrorIs(t, r.Add(t.Context(), s, "bitnami", "https://other", "", ""), apperr.ErrConflict, "an org-wide name is taken")
}

func TestRepos_DeleteRemovesSessionRepositoriesOnly(t *testing.T) {
	w := newWorld(t)
	r := w.repos(config.HelmRepo{Name: "bitnami", URL: "https://charts.example.org"})
	s := w.session("h1")
	require.NoError(t, r.Add(t.Context(), s, "mine", "https://charts.internal", "", ""))
	require.NoError(t, r.Delete(t.Context(), s, "mine"))
	_, err := r.Resolve(t.Context(), s, "mine")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	assert.ErrorIs(t, r.Delete(t.Context(), s, "bitnami"), apperr.ErrForbidden)
	assert.ErrorIs(t, r.Delete(t.Context(), s, "nope"), apperr.ErrNotFound)
}

func TestRepos_ACorruptEntryIsAnError(t *testing.T) {
	w := newWorld(t)
	r := w.repos()
	require.NoError(t, w.sessions.PutCredential(t.Context(), "h1", store.CredentialRow{Kind: store.CredHelmRepo, Ref: "mine", Sealed: "garbage"}, t0))
	_, err := r.List(t.Context(), w.session("h1"))
	assert.Error(t, err)
	_, err = r.Resolve(t.Context(), w.session("h1"), "mine")
	assert.Error(t, err)
}

func TestRepos_Add_AddressOnALocalAddress_IsRefusedWhenTheGuardIsOn(t *testing.T) {
	w := newWorld(t)
	r := w.repos()
	r.BlockLocal = true
	for _, addr := range []string{"http://127.0.0.1:8080", "https://169.254.169.254/charts", "http://[::1]/x"} {
		err := r.Add(t.Context(), w.session("h1"), "mine", addr, "", "")
		require.ErrorIs(t, err, egress.ErrBlocked, addr)
	}
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "mine", "http://10.0.0.1:8080", "", ""))
	r.BlockLocal = false
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "local", "http://127.0.0.1:8080", "", ""))
}

func TestRepos_Resolve_MarksSessionRepositoriesOnly(t *testing.T) {
	w := newWorld(t)
	r := w.repos(config.HelmRepo{Name: "bitnami", URL: "https://charts.example.org"})
	require.NoError(t, r.Add(t.Context(), w.session("h1"), "mine", "https://charts.internal", "", ""))

	org, err := r.Resolve(t.Context(), w.session("h1"), "bitnami")
	require.NoError(t, err)
	assert.False(t, org.Session)
	mine, err := r.Resolve(t.Context(), w.session("h1"), "mine")
	require.NoError(t, err)
	assert.True(t, mine.Session)
}
