package vault_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/vault"
)

// slowAPI takes d of fake time for every call and answers with err.
type slowAPI struct {
	clk *clock.Fake
	d   time.Duration
	err error
}

func (a slowAPI) take() error { a.clk.Advance(a.d); return a.err }

func (a slowAPI) LookupSelf(context.Context, string) (vault.TokenInfo, error) {
	return vault.TokenInfo{}, a.take()
}
func (a slowAPI) RenewSelf(context.Context, string) (time.Duration, error) { return 0, a.take() }
func (a slowAPI) Mounts(context.Context, string) (map[string]string, error) {
	return nil, a.take()
}
func (a slowAPI) Capabilities(context.Context, string, []string) (map[string][]string, error) {
	return nil, a.take()
}
func (a slowAPI) ClusterConfig(context.Context, string, string) (vault.ClusterConfig, error) {
	return vault.ClusterConfig{}, a.take()
}
func (a slowAPI) Creds(context.Context, string, string, string, vault.CredsRequest) (vault.Creds, error) {
	return vault.Creds{}, a.take()
}

type obs struct {
	endpoint, outcome string
	d                 time.Duration
}

func TestInstrument_EveryCallIsObservedWithItsEndpointOutcomeAndDuration(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC))
	var got []obs
	observe := func(endpoint, outcome string, d time.Duration) { got = append(got, obs{endpoint, outcome, d}) }
	ctx := t.Context()

	api := vault.Instrument(slowAPI{clk: clk, d: 40 * time.Millisecond}, clk, observe)
	_, _ = api.LookupSelf(ctx, "t")
	_, _ = api.RenewSelf(ctx, "t")
	_, _ = api.Mounts(ctx, "t")
	_, _ = api.Capabilities(ctx, "t", nil)
	_, _ = api.ClusterConfig(ctx, "t", "kubernetes/prod")
	_, _ = api.Creds(ctx, "t", "kubernetes/prod", "role", vault.CredsRequest{})

	want := []string{"lookup_self", "renew_self", "mounts", "capabilities", "cluster_config", "creds"}
	require.Len(t, got, len(want))
	for i, w := range want {
		assert.Equal(t, obs{w, "ok", 40 * time.Millisecond}, got[i])
	}
}

func TestInstrument_Outcomes_OkDeniedError_AndTheErrorIsPassedOn(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		err     error
		outcome string
	}{
		{nil, "ok"},
		{fmt.Errorf("%w: no", vault.ErrPermissionDenied), "denied"},
		{vault.ErrUnreachable, "error"},
		{errors.New("boom"), "error"},
	} {
		var got obs
		api := vault.Instrument(slowAPI{clk: clk, err: tc.err}, clk, func(e, o string, d time.Duration) { got = obs{e, o, d} })
		_, err := api.RenewSelf(t.Context(), "t")
		assert.Equal(t, tc.err, err)
		assert.Equal(t, tc.outcome, got.outcome)
	}
}

func TestInstrument_NoObserver_ReturnsTheAPIUnchanged(t *testing.T) {
	api := slowAPI{}
	assert.Equal(t, vault.API(api), vault.Instrument(api, clock.NewFake(time.Time{}), nil))
}
