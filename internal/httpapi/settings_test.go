package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/store"
)

const settingsDoc = `{"favouriteClusters":["v:prod"],"namespaceFilters":{"v:prod":["ns1","ns2"]},"columnPresets":{"/Pod":["name","status"]},
"filterSets":[{"name":"Failing","kind":"Pod","query":"status.phase = Failed"}],"watchIdleSeconds":120,"theme":"dark"}`

func TestSettings_GetReturnsDefaultsWithEmptyCollections(t *testing.T) {
	e := newKubeEnv(t)
	rec := e.get("/api/v1/settings")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"favouriteClusters":[],"namespaceFilters":{},"columnPresets":{},"filterSets":[],"watchIdleSeconds":30,"theme":"system"}`, rec.Body.String())
}

func TestSettings_PutThenGetRoundTrips(t *testing.T) {
	e := newKubeEnv(t)
	rec := e.send(http.MethodPut, "/api/v1/settings", settingsDoc)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, settingsDoc, rec.Body.String())
	assert.JSONEq(t, settingsDoc, e.get("/api/v1/settings").Body.String())
	// the same document is what /me shows
	var me struct {
		Settings struct{ FavouriteClusters []string }
	}
	require.NoError(t, json.Unmarshal(e.get("/api/v1/me").Body.Bytes(), &me))
	assert.Equal(t, []string{"v:prod"}, me.Settings.FavouriteClusters)
}

func TestSettings_BelongToTheUserNotTheSession(t *testing.T) {
	e := newKubeEnv(t)
	require.Equal(t, http.StatusOK, e.send(http.MethodPut, "/api/v1/settings", settingsDoc).Code)
	userID, err := store.NewUsers(e.db, e.fake, func() string { return "x" }).Upsert(t.Context(), "google", "s", "e", "n") // the same user, found again
	require.NoError(t, err)
	id2, _, err := e.deps.Sessions.Create(t.Context(), userID.ID)
	require.NoError(t, err)
	assert.JSONEq(t, settingsDoc, e.doWithSession(http.MethodGet, "/api/v1/settings", id2, "").Body.String())

	other, err := store.NewUsers(e.db, e.fake, func() string { return "u2" }).Upsert(t.Context(), "google", "s2", "e2", "n2")
	require.NoError(t, err)
	id3, _, err := e.deps.Sessions.Create(t.Context(), other.ID)
	require.NoError(t, err)
	assert.Contains(t, e.doWithSession(http.MethodGet, "/api/v1/settings", id3, "").Body.String(), `"favouriteClusters":[]`)
}

func TestSettings_TheNewIdleValueIsWhatTheWatchHubReadsNext(t *testing.T) {
	e := newKubeEnv(t)
	require.Equal(t, http.StatusOK, e.send(http.MethodPut, "/api/v1/settings", settingsDoc).Code)
	s, err := store.NewUsers(e.db, e.fake, func() string { return "x" }).GetSettings(t.Context(), "u1")
	require.NoError(t, err)
	assert.Equal(t, 120, s.WatchIdleSeconds)
}

func TestSettings_ValidationErrorsAre400(t *testing.T) {
	e := newKubeEnv(t)
	many := make([]string, 201)
	for i := range many {
		many[i] = fmt.Sprintf("%q", fmt.Sprintf("c%d", i))
	}
	for name, body := range map[string]string{
		"idle 29":        `{"watchIdleSeconds":29}`,
		"idle 301":       `{"watchIdleSeconds":301}`,
		"201 favourites": `{"watchIdleSeconds":30,"favouriteClusters":[` + strings.Join(many, ",") + `]}`,
		"empty filter":   `{"watchIdleSeconds":30,"filterSets":[{"name":"","kind":"Pod","query":"a = 1"}]}`,
		"over 256 KiB":   `{"watchIdleSeconds":30,"namespaceFilters":{"c":["` + strings.Repeat("n", 300<<10) + `"]}}`,
		"unknown field":  `{"watchIdleSeconds":30,"admin":true}`,
		"not json":       `nonsense`,
		"bad theme":      `{"watchIdleSeconds":30,"theme":"neon"}`,
	} {
		rec := e.send(http.MethodPut, "/api/v1/settings", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Equal(t, "bad_request", errorCode(t, rec), name)
	}
	assert.JSONEq(t, `{"favouriteClusters":[],"namespaceFilters":{},"columnPresets":{},"filterSets":[],"watchIdleSeconds":30,"theme":"system"}`, e.get("/api/v1/settings").Body.String(), "nothing was saved")
}

func TestSettings_PutNeedsCSRFAndASession(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusForbidden, e.doWithSession(http.MethodPut, "/api/v1/settings", e.id, "").Code)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/settings", "", "").Code)
}
