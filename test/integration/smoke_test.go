//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

func TestSmoke_HealthAndSignIn(t *testing.T) {
	c := it.NewClient(t)

	status, _ := c.Do(http.MethodGet, "/healthz", nil, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, http.StatusUnauthorized, c.JSON(http.MethodGet, "/api/v1/me", nil, nil))

	final := c.SignInGoogle(it.Account{Sub: "smoke-1", Email: "a@corp.example", HD: "corp.example"})
	require.Empty(t, final.Query().Get("signin_error"), "signed in, final URL %s", final)

	var me struct {
		User              struct{ Email string } `json:"user"`
		MaintenanceBanner string                 `json:"maintenanceBanner"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/me", nil, &me))
	assert.Equal(t, "a@corp.example", me.User.Email)
	assert.Equal(t, "integration test banner", me.MaintenanceBanner)
}
