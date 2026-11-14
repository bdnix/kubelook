package vault_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakevault"
	"github.com/bdnix/kubelook/internal/vault"
)

func TestHealth_StatusCodes(t *testing.T) {
	fv := fakevault.New()
	t.Cleanup(fv.Close)
	c := vault.NewClient(fv.URL(), fv.Client())
	for code, reachable := range map[int]bool{200: true, 429: true, 472: true, 473: true, 501: false, 503: false, 500: false} {
		fv.SetHealthStatus(code)
		err := c.Health(t.Context())
		if reachable {
			require.NoError(t, err, code)
		} else {
			require.ErrorIs(t, err, vault.ErrUnreachable, code)
		}
	}
}

func TestHealth_NeedsNoToken_AndNetworkErrorIsUnreachable(t *testing.T) {
	fv := fakevault.New()
	t.Cleanup(fv.Close)
	c := vault.NewClient(fv.URL(), fv.Client())
	require.NoError(t, c.Health(t.Context()))
	assert.Equal(t, 1, fv.Calls("health"))
	fv.HangUp(true)
	require.ErrorIs(t, c.Health(t.Context()), vault.ErrUnreachable)
}

type prober struct {
	err   atomic.Value // error or nil
	calls chan struct{}
}

func newProber() *prober { return &prober{calls: make(chan struct{}, 64)} }

func (p *prober) set(err error) { p.err.Store(&err) }

func (p *prober) Health(context.Context) error {
	defer func() { p.calls <- struct{}{} }()
	if e, ok := p.err.Load().(*error); ok {
		return *e
	}
	return nil
}

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func runMonitor(t *testing.T) (*vault.Monitor, *prober, *clock.Fake) {
	t.Helper()
	fc := clock.NewFake(t0)
	p := newProber()
	m := vault.NewMonitor(p, fc, true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	fc.BlockUntil(1)
	return m, p, fc
}

func tick(t *testing.T, fc *clock.Fake, p *prober) {
	t.Helper()
	fc.Advance(15 * time.Second)
	testutil.Recv(t, p.calls)
}

func TestMonitor_FlipsAfterTwoConsecutiveFailuresAndBackAfterOneSuccess(t *testing.T) {
	m, p, fc := runMonitor(t)
	assert.True(t, m.Status().Reachable)
	p.set(vault.ErrUnreachable)
	tick(t, fc, p)
	assert.True(t, m.Status().Reachable, "one failure is not enough")
	tick(t, fc, p)
	st := m.Status()
	assert.False(t, st.Reachable)
	assert.True(t, st.Configured)
	require.NotNil(t, st.Since)
	assert.Equal(t, t0.Add(30*time.Second), *st.Since)

	p.set(nil)
	tick(t, fc, p)
	assert.True(t, m.Status().Reachable, "one success is enough")
	assert.Equal(t, t0.Add(45*time.Second), *m.Status().Since)
}

func TestMonitor_ASuccessBetweenFailuresResetsTheCount(t *testing.T) {
	m, p, fc := runMonitor(t)
	p.set(vault.ErrUnreachable)
	tick(t, fc, p)
	p.set(nil)
	tick(t, fc, p)
	p.set(vault.ErrUnreachable)
	tick(t, fc, p)
	assert.True(t, m.Status().Reachable)
}

func TestMonitor_RealCallsFlipImmediately(t *testing.T) {
	fv := fakevault.New()
	t.Cleanup(fv.Close)
	fv.AddToken("tok", fakevault.Token{TTLSeconds: 60})
	fc := clock.NewFake(t0)
	raw := vault.NewClient(fv.URL(), fv.Client())
	m := vault.NewMonitor(raw, fc, true)
	api := m.Wrap(raw)

	fv.HangUp(true)
	_, err := api.LookupSelf(t.Context(), "tok")
	require.ErrorIs(t, err, vault.ErrUnreachable)
	assert.False(t, m.Status().Reachable)

	fv.HangUp(false)
	fc.Advance(time.Minute)
	_, err = api.LookupSelf(t.Context(), "tok")
	require.NoError(t, err)
	assert.True(t, m.Status().Reachable)
	assert.Equal(t, t0.Add(time.Minute), *m.Status().Since)

	// an answer from Vault, even a refusal, proves it is reachable
	fv.HangUp(true)
	_, _ = api.LookupSelf(t.Context(), "tok")
	fv.HangUp(false)
	_, err = api.LookupSelf(t.Context(), "bad-token")
	require.ErrorIs(t, err, vault.ErrPermissionDenied)
	assert.True(t, m.Status().Reachable)
}

func TestMonitor_WrapObservesEveryCall(t *testing.T) {
	fv := fakevault.New()
	t.Cleanup(fv.Close)
	fv.AddToken("tok", fakevault.Token{TTLSeconds: 60, Renewable: true, Caps: map[string][]string{"a/config": {"read"}}})
	fv.AddMount("kubernetes/a/", "kubernetes")
	fc := clock.NewFake(t0)
	raw := vault.NewClient(fv.URL(), fv.Client())
	m := vault.NewMonitor(raw, fc, true)
	api := m.Wrap(raw)
	fv.HangUp(true)
	ctx := t.Context()
	for name, call := range map[string]func() error{
		"renew":  func() error { _, err := api.RenewSelf(ctx, "tok"); return err },
		"mounts": func() error { _, err := api.Mounts(ctx, "tok"); return err },
		"caps":   func() error { _, err := api.Capabilities(ctx, "tok", []string{"a/config"}); return err },
		"config": func() error { _, err := api.ClusterConfig(ctx, "tok", "kubernetes/a"); return err },
		"creds":  func() error { _, err := api.Creds(ctx, "tok", "kubernetes/a", "r", vault.CredsRequest{}); return err },
	} {
		m.Observe(nil)
		require.True(t, m.Status().Reachable, name)
		require.Error(t, call(), name)
		assert.False(t, m.Status().Reachable, name)
	}
}

func TestMonitor_NotConfigured_NeverProbes(t *testing.T) {
	p := newProber()
	m := vault.NewMonitor(p, clock.NewFake(t0), false)
	m.Run(t.Context()) // returns at once
	st := m.Status()
	assert.False(t, st.Configured)
	assert.True(t, st.Reachable)
	assert.Nil(t, st.Since)
	assert.Empty(t, p.calls)
}

func TestMonitor_RunStopsWithItsContext(t *testing.T) {
	fc := clock.NewFake(t0)
	m := vault.NewMonitor(newProber(), fc, true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	fc.BlockUntil(1)
	cancel()
	testutil.Recv(t, done)
	m.Observe(errors.New("an ordinary error"))
	assert.True(t, m.Status().Reachable)
}
