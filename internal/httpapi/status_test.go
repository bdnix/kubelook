package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/vault"
)

type downProber struct{}

func (downProber) Health(context.Context) error { return vault.ErrUnreachable }

func TestStatus_VaultStateFollowsTheMonitor(t *testing.T) {
	e := newKubeEnv(t)
	rec := e.get("/api/v1/status")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"vault":{"configured":false,"reachable":true,"since":null}}`, rec.Body.String())

	m := vault.NewMonitor(downProber{}, e.fake, true)
	e.deps.VaultMonitor = m
	var got struct {
		Vault struct {
			Configured, Reachable bool
			Since                 *time.Time
		}
	}
	rec = e.get("/api/v1/status")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, got.Vault.Configured)
	assert.True(t, got.Vault.Reachable)
	require.NotNil(t, got.Vault.Since)

	m.Observe(vault.ErrUnreachable)
	rec = e.get("/api/v1/status")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.False(t, got.Vault.Reachable)
}

func TestStatus_RequiresASession(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/status", "", "").Code)
}
