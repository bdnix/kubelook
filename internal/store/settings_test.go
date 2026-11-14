package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

func settingsEnv(t *testing.T) (*store.Users, string) {
	t.Helper()
	users := store.NewUsers(storetest.New(t), clock.NewFake(t0), func() string { return "u1" })
	u, err := users.Upsert(t.Context(), "google", "s", "e@x", "n")
	require.NoError(t, err)
	return users, u.ID
}

func valid() store.Settings {
	return store.Settings{
		FavouriteClusters: []string{"v:a"}, NamespaceFilters: map[string][]string{"v:a": {"ns1"}},
		ColumnPresets: map[string][]string{"/Pod": {"name", "status"}}, FilterSets: []store.FilterSet{{Name: "Failing", Kind: "Pod", Query: "status.phase = Failed"}},
		WatchIdleSeconds: 120, Theme: "dark",
	}
}

func TestSettings_RoundTrip(t *testing.T) {
	users, id := settingsEnv(t)
	require.NoError(t, users.PutSettings(t.Context(), id, valid()))
	got, err := users.GetSettings(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, valid(), got)
}

func TestSettings_Validation(t *testing.T) {
	many := make([]string, 201)
	for i := range many {
		many[i] = fmt.Sprintf("c%d", i)
	}
	huge := valid()
	huge.NamespaceFilters = map[string][]string{"c": {strings.Repeat("n", 300<<10)}}
	for name, mutate := range map[string]func(*store.Settings){
		"idle too low":        func(s *store.Settings) { s.WatchIdleSeconds = 29 },
		"idle too high":       func(s *store.Settings) { s.WatchIdleSeconds = 301 },
		"201 favourites":      func(s *store.Settings) { s.FavouriteClusters = many },
		"empty filter name":   func(s *store.Settings) { s.FilterSets = []store.FilterSet{{Name: " ", Kind: "Pod", Query: "a = 1"}} },
		"filter without kind": func(s *store.Settings) { s.FilterSets = []store.FilterSet{{Name: "x", Query: "a = 1"}} },
		"duplicate filter": func(s *store.Settings) {
			s.FilterSets = []store.FilterSet{{Name: "x", Kind: "Pod"}, {Name: "x", Kind: "Pod"}}
		},
		"unknown theme": func(s *store.Settings) { s.Theme = "neon" },
		"over 256 KiB":  func(s *store.Settings) { s.NamespaceFilters = huge.NamespaceFilters },
	} {
		users, id := settingsEnv(t)
		s := valid()
		mutate(&s)
		assert.ErrorIs(t, users.PutSettings(t.Context(), id, s), store.ErrInvalid, name)
		got, err := users.GetSettings(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, 30, got.WatchIdleSeconds, "%s: nothing was saved", name)
	}
}

func TestSettings_EdgesAreAllowed(t *testing.T) {
	users, id := settingsEnv(t)
	s := valid()
	s.WatchIdleSeconds = 30
	s.FavouriteClusters = make([]string, 200)
	s.Theme = ""
	require.NoError(t, users.PutSettings(t.Context(), id, s))
	s.WatchIdleSeconds = 300
	s.Theme = "system"
	require.NoError(t, users.PutSettings(t.Context(), id, s))
}
