package store_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func newUsers(t *testing.T) (*store.Users, *clock.Fake) {
	t.Helper()
	fake := clock.NewFake(t0)
	n := 0
	newID := func() string { n++; return "user-" + strconv.Itoa(n) }
	return store.NewUsers(storetest.New(t), fake, newID), fake
}

func TestUsers_Upsert_CreatesUserWithInjectedID(t *testing.T) {
	u, _ := newUsers(t)
	got, err := u.Upsert(t.Context(), "google", "sub-1", "a@example.com", "Ann")
	require.NoError(t, err)
	assert.Equal(t, store.User{
		ID: "user-1", Provider: "google", Subject: "sub-1", Email: "a@example.com", Name: "Ann",
		CreatedAt: t0, LastLoginAt: t0,
	}, got)
}

func TestUsers_Upsert_SameSubjectKeepsIDAndCreatedAt(t *testing.T) {
	u, fake := newUsers(t)
	first, err := u.Upsert(t.Context(), "google", "sub-1", "a@example.com", "Ann")
	require.NoError(t, err)
	fake.Advance(time.Hour)

	second, err := u.Upsert(t.Context(), "google", "sub-1", "new@example.com", "Ann B")
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "new@example.com", second.Email)
	assert.Equal(t, "Ann B", second.Name)
	assert.Equal(t, t0, second.CreatedAt)
	assert.Equal(t, t0.Add(time.Hour), second.LastLoginAt)

	stored, err := u.Get(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, second, stored)
}

func TestUsers_Upsert_SameSubjectDifferentProvider_IsADifferentUser(t *testing.T) {
	u, _ := newUsers(t)
	a, err := u.Upsert(t.Context(), "google", "sub-1", "a@example.com", "Ann")
	require.NoError(t, err)
	b, err := u.Upsert(t.Context(), "github", "sub-1", "a@example.com", "Ann")
	require.NoError(t, err)
	assert.NotEqual(t, a.ID, b.ID)
}

func TestUsers_Get_UnknownID_ErrNotFound(t *testing.T) {
	u, _ := newUsers(t)
	_, err := u.Get(t.Context(), "nope")
	assert.ErrorIs(t, err, store.ErrNotFound)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestUsers_GetSettings_NoRow_ReturnsDefaults(t *testing.T) {
	u, _ := newUsers(t)
	user, _ := u.Upsert(t.Context(), "google", "s", "e", "n")
	s, err := u.GetSettings(t.Context(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, store.Settings{WatchIdleSeconds: 30}, s)
}

func TestUsers_PutSettings_RoundTripsEveryField(t *testing.T) {
	u, fake := newUsers(t)
	user, _ := u.Upsert(t.Context(), "google", "s", "e", "n")
	want := store.Settings{
		FavouriteClusters: []string{"v:prod", "k:file/ctx"},
		NamespaceFilters:  map[string][]string{"v:prod": {"a", "b"}},
		ColumnPresets:     map[string][]string{"apps/Deployment": {"name", "ready"}},
		FilterSets:        []store.FilterSet{{Name: "bad", Kind: "Pod", Query: "status=Failed"}},
		WatchIdleSeconds:  120,
		Theme:             "dark",
	}
	require.NoError(t, u.PutSettings(t.Context(), user.ID, want))
	got, err := u.GetSettings(t.Context(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	fake.Advance(time.Minute)
	want.Theme = "light"
	require.NoError(t, u.PutSettings(t.Context(), user.ID, want))
	got, _ = u.GetSettings(t.Context(), user.ID)
	assert.Equal(t, "light", got.Theme)
}

func TestUsers_PutSettings_RecordsUpdatedAt(t *testing.T) {
	db := storetest.New(t)
	fake := clock.NewFake(t0.Add(5 * time.Second))
	u := store.NewUsers(db, fake, func() string { return "u1" })
	user, _ := u.Upsert(t.Context(), "p", "s", "e", "n")
	require.NoError(t, u.PutSettings(t.Context(), user.ID, store.Settings{WatchIdleSeconds: 30}))
	var ms int64
	require.NoError(t, db.QueryRow(t.Context(), "SELECT updated_at FROM user_settings WHERE user_id = ?", user.ID).Scan(&ms))
	assert.Equal(t, t0.Add(5*time.Second).UnixMilli(), ms)
}

func TestUsers_PutSettings_WatchIdleOutOfRange_ErrInvalid(t *testing.T) {
	u, _ := newUsers(t)
	user, _ := u.Upsert(t.Context(), "google", "s", "e", "n")
	for _, secs := range []int{-1, 0, 29, 301} {
		err := u.PutSettings(t.Context(), user.ID, store.Settings{WatchIdleSeconds: secs})
		assert.ErrorIs(t, err, store.ErrInvalid, secs)
	}
	for _, secs := range []int{30, 300} {
		assert.NoError(t, u.PutSettings(t.Context(), user.ID, store.Settings{WatchIdleSeconds: secs}), secs)
	}
}

func TestUsers_PutSettings_UnknownUser_ReturnsError(t *testing.T) {
	u, _ := newUsers(t)
	assert.Error(t, u.PutSettings(t.Context(), "ghost", store.Settings{WatchIdleSeconds: 30}))
}

func TestNewID_Is32LowercaseHexAndUnique(t *testing.T) {
	a, b := store.NewID(), store.NewID()
	assert.Len(t, a, 32)
	assert.Regexp(t, `^[0-9a-f]{32}$`, a)
	assert.NotEqual(t, a, b)
}
