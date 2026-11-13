package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

type sessionEnv struct {
	db       *store.DB
	sessions *store.Sessions
	userID   string
}

func newSessions(t *testing.T) sessionEnv {
	t.Helper()
	db := storetest.New(t)
	u, err := store.NewUsers(db, clock.NewFake(t0), func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e@x", "n")
	require.NoError(t, err)
	return sessionEnv{db: db, sessions: store.NewSessions(db), userID: u.ID}
}

func (e sessionEnv) create(t *testing.T, hash string, expires time.Time) {
	t.Helper()
	require.NoError(t, e.sessions.Create(t.Context(), hash, e.userID, "csrf-"+hash, t0, expires))
}

func (e sessionEnv) count(t *testing.T, table string) int {
	t.Helper()
	return count(t, e.db, table)
}

func TestSessions_CreateThenGet(t *testing.T) {
	e := newSessions(t)
	e.create(t, "h1", t0.Add(12*time.Hour))
	got, err := e.sessions.Get(t.Context(), "h1")
	require.NoError(t, err)
	assert.Equal(t, store.SessionRow{
		Hash: "h1", UserID: e.userID, CSRFToken: "csrf-h1",
		CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(12 * time.Hour),
	}, got)
}

func TestSessions_Get_Unknown_ErrNotFound(t *testing.T) {
	_, err := newSessions(t).sessions.Get(t.Context(), "nope")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestSessions_Create_UnknownUser_ReturnsError(t *testing.T) {
	e := newSessions(t)
	assert.Error(t, e.sessions.Create(t.Context(), "h", "ghost", "c", t0, t0.Add(time.Hour)))
}

func TestSessions_Touch_UpdatesLastSeen_UnknownIsNotFound(t *testing.T) {
	e := newSessions(t)
	e.create(t, "h1", t0.Add(time.Hour))
	require.NoError(t, e.sessions.Touch(t.Context(), "h1", t0.Add(time.Minute)))
	got, _ := e.sessions.Get(t.Context(), "h1")
	assert.Equal(t, t0.Add(time.Minute), got.LastSeenAt)

	assert.ErrorIs(t, e.sessions.Touch(t.Context(), "nope", t0), store.ErrNotFound)
}

func TestSessions_Delete_RemovesSessionAndCredentials(t *testing.T) {
	e := newSessions(t)
	e.create(t, "h1", t0.Add(time.Hour))
	e.create(t, "h2", t0.Add(time.Hour))
	require.NoError(t, e.sessions.PutCredential(t.Context(), "h1", store.CredentialRow{Kind: store.CredVaultToken, Sealed: "s"}, t0))
	require.NoError(t, e.sessions.PutCredential(t.Context(), "h2", store.CredentialRow{Kind: store.CredVaultToken, Sealed: "s"}, t0))

	require.NoError(t, e.sessions.Delete(t.Context(), "h1"))
	assert.Equal(t, 1, e.count(t, "sessions"))
	assert.Equal(t, 1, e.count(t, "session_credentials"))
	assert.NoError(t, e.sessions.Delete(t.Context(), "h1"), "deleting twice is not an error")
}

func TestSessions_DeleteExpired_RemovesAbsoluteAndIdleReturningSortedHashes(t *testing.T) {
	e := newSessions(t)
	now := t0.Add(time.Hour)
	idle := 30 * time.Minute
	e.create(t, "z-expired", now)               // expires_at <= now
	e.create(t, "a-idle", now.Add(time.Hour))   // last seen at t0 = now-60m
	e.create(t, "fresh", now.Add(time.Hour))    // touched below
	e.create(t, "boundary", now.Add(time.Hour)) // last seen exactly now-idle
	require.NoError(t, e.sessions.Touch(t.Context(), "fresh", now.Add(-time.Minute)))
	require.NoError(t, e.sessions.Touch(t.Context(), "boundary", now.Add(-idle)))

	got, err := e.sessions.DeleteExpired(t.Context(), now, idle)
	require.NoError(t, err)
	assert.Equal(t, []string{"a-idle", "boundary", "z-expired"}, got)
	assert.Equal(t, 1, e.count(t, "sessions"))

	got, err = e.sessions.DeleteExpired(t.Context(), now, idle)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSessions_Exists_ReturnsSubset(t *testing.T) {
	e := newSessions(t)
	e.create(t, "h1", t0.Add(time.Hour))
	e.create(t, "h2", t0.Add(time.Hour))

	got, err := e.sessions.Exists(t.Context(), []string{"h2", "gone", "h1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"h1", "h2"}, got)

	got, err = e.sessions.Exists(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSessions_Credentials_PutGetListDelete(t *testing.T) {
	ctx := t.Context()
	e := newSessions(t)
	e.create(t, "h1", t0.Add(time.Hour))

	exp := t0.Add(30 * time.Minute)
	require.NoError(t, e.sessions.PutCredential(ctx, "h1", store.CredentialRow{Kind: store.CredKubeconfig, Ref: "prod", Sealed: "s1", ExpiresAt: exp}, t0))
	require.NoError(t, e.sessions.PutCredential(ctx, "h1", store.CredentialRow{Kind: store.CredKubeconfig, Ref: "dev", Sealed: "s2"}, t0))
	require.NoError(t, e.sessions.PutCredential(ctx, "h1", store.CredentialRow{Kind: store.CredVaultToken, Sealed: "tok"}, t0))

	got, err := e.sessions.GetCredential(ctx, "h1", store.CredKubeconfig, "prod")
	require.NoError(t, err)
	assert.Equal(t, store.CredentialRow{Kind: "kubeconfig", Ref: "prod", Sealed: "s1", ExpiresAt: exp}, got)

	got, err = e.sessions.GetCredential(ctx, "h1", store.CredKubeconfig, "dev")
	require.NoError(t, err)
	assert.True(t, got.ExpiresAt.IsZero(), "no expiry is the zero time")

	list, err := e.sessions.ListCredentials(ctx, "h1", store.CredKubeconfig)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, []string{"dev", "prod"}, []string{list[0].Ref, list[1].Ref})

	// upsert replaces
	require.NoError(t, e.sessions.PutCredential(ctx, "h1", store.CredentialRow{Kind: store.CredKubeconfig, Ref: "prod", Sealed: "s1b"}, t0))
	got, _ = e.sessions.GetCredential(ctx, "h1", store.CredKubeconfig, "prod")
	assert.Equal(t, "s1b", got.Sealed)
	assert.Equal(t, 3, e.count(t, "session_credentials"))

	require.NoError(t, e.sessions.DeleteCredential(ctx, "h1", store.CredKubeconfig, "prod"))
	_, err = e.sessions.GetCredential(ctx, "h1", store.CredKubeconfig, "prod")
	assert.ErrorIs(t, err, store.ErrNotFound)
	assert.Equal(t, 2, e.count(t, "session_credentials"))
}

func TestSessions_PutCredential_MissingSession_ReturnsError(t *testing.T) {
	e := newSessions(t)
	err := e.sessions.PutCredential(t.Context(), "ghost", store.CredentialRow{Kind: store.CredVaultToken, Sealed: "x"}, t0)
	assert.Error(t, err)
}

func TestSessions_CredentialKindConstants(t *testing.T) {
	assert.Equal(t, "vault_token", store.CredVaultToken)
	assert.Equal(t, "kubeconfig", store.CredKubeconfig)
	assert.Equal(t, "cluster_cred", store.CredClusterCred)
	assert.Equal(t, "cluster_role", store.CredClusterRole)
	assert.Equal(t, "helm_repo", store.CredHelmRepo)
}
