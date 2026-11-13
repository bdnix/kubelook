package audit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

func seed(t *testing.T, e env, ages ...time.Duration) {
	t.Helper()
	for _, age := range ages {
		_, err := e.db.Exec(t.Context(), `INSERT INTO audit_events (id, time, account_provider, account_subject, account_email,
vault_entity_id, vault_display_name, kube_context, kube_user, cluster, namespace, verb, resource_kind, resource_name, outcome, details)
VALUES (?, ?, '', '', '', '', '', '', '', '', '', 'x', '', '', 'success', '{}')`,
			"seed-"+age.String(), t0.Add(-age).UnixMilli())
		require.NoError(t, err)
	}
}

func TestCleanup_DeletesOlderThanRetention(t *testing.T) {
	e := newEnv(t)
	day := 24 * time.Hour
	seed(t, e, 8*day, 7*day+time.Second, 7*day, 6*day, time.Hour)

	n, err := audit.Cleanup(t.Context(), e.db, clock.NewFake(t0), 7)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n) // strictly older than now-7d: the 7-day-old row stays
	assert.Len(t, e.rows(t), 3)
}

func TestCleanup_ZeroRetention_DeletesNothing(t *testing.T) {
	e := newEnv(t)
	seed(t, e, 1000*24*time.Hour)
	n, err := audit.Cleanup(t.Context(), e.db, clock.NewFake(t0), 0)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Len(t, e.rows(t), 1)
}

func TestCleanup_NegativeRetention_ReturnsError(t *testing.T) {
	_, err := audit.Cleanup(t.Context(), storetest.New(t), clock.NewFake(t0), -1)
	assert.Error(t, err)
}

func TestCleanup_DatabaseFailure_ReturnsError(t *testing.T) {
	db := storetest.New(t)
	require.NoError(t, db.Close())
	_, err := audit.Cleanup(t.Context(), db, clock.NewFake(t0), 7)
	assert.Error(t, err)
}
