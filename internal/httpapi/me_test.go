package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

func TestMe_NothingWiredYet_401(t *testing.T) {
	rec := newHarness("https://k.example.com").do(http.MethodGet, "/api/v1/me")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthenticated", errorCode(t, rec))
}

func TestMe_WithSessionsButNoCookie_401(t *testing.T) {
	h, _, _ := withSessions(t)
	rec := h.doWithSession(http.MethodGet, "/api/v1/me", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthenticated", errorCode(t, rec))
}

func TestMe_ValidSession_ReturnsTheUser(t *testing.T) {
	h, id, _ := withSessions(t)
	h.deps.Users = store.NewUsers(h.db, h.fake, func() string { return "unused" })

	rec := h.doWithSession(http.MethodGet, "/api/v1/me", id, "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"u1","email":"e","name":"n"}`, rec.Body.String())
}

func TestMe_UserRowGone_404(t *testing.T) {
	h, id, _ := withSessions(t)
	h.deps.Users = store.NewUsers(storetest.New(t), h.fake, func() string { return "x" }) // a different, empty database
	rec := h.doWithSession(http.MethodGet, "/api/v1/me", id, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))
}
