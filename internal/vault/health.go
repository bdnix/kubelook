package vault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
)

const (
	healthPath      = "/v1/sys/health?standbyok=true&perfstandbyok=true"
	monitorInterval = 15 * time.Second
	failuresToFlip  = 2
)

// Health asks Vault whether it can serve requests, without a token. Active and standby nodes
// (200, 429, 472, 473) are reachable; a sealed (503) or uninitialized (501) Vault and network
// errors are not.
func (c *Client) Health(ctx context.Context) error {
	//nolint:gosec // G704: the address is operator configuration
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.address+healthPath, nil)
	if err != nil {
		return fmt.Errorf("building vault health request: %w", err)
	}
	req.Header.Set("X-Vault-Request", "true")
	resp, err := c.hc.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusTooManyRequests, 472, 473:
		return nil
	}
	return fmt.Errorf("%w: health answered HTTP %d", ErrUnreachable, resp.StatusCode)
}

// Prober is what the monitor asks.
type Prober interface {
	Health(ctx context.Context) error
}

// Status is what /api/v1/status reports about Vault.
type Status struct {
	Configured bool       `json:"configured"`
	Reachable  bool       `json:"reachable"`
	Since      *time.Time `json:"since"`
}

// Monitor tracks whether Vault is reachable. It runs without a user's token.
type Monitor struct {
	prober     Prober
	clock      clock.Clock
	configured bool

	mu        sync.Mutex
	reachable bool
	since     time.Time
	failures  int
}

// NewMonitor returns a monitor that starts out believing Vault is reachable. With configured
// false (no vault.address) it reports configured:false and never probes.
func NewMonitor(p Prober, c clock.Clock, configured bool) *Monitor {
	return &Monitor{prober: p, clock: c, configured: configured, reachable: true, since: c.Now()}
}

// Status returns the current state.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.configured {
		return Status{Reachable: true}
	}
	since := m.since
	return Status{Configured: true, Reachable: m.reachable, Since: &since}
}

// Run probes every 15 s until ctx is done. Two failures in a row mark Vault unreachable; one
// success marks it reachable again.
func (m *Monitor) Run(ctx context.Context) {
	if !m.configured {
		return
	}
	t := m.clock.NewTicker(monitorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			err := m.prober.Health(ctx)
			if ctx.Err() != nil {
				return
			}
			m.probed(err == nil)
		}
	}
}

func (m *Monitor) probed(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok {
		m.failures = 0
		m.set(true)
		return
	}
	m.failures++
	if m.failures >= failuresToFlip {
		m.set(false)
	}
}

// Observe records the outcome of a real API call: an unreachable Vault flips the state at once,
// any answer from Vault (even a refusal) flips it back.
func (m *Monitor) Observe(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if errors.Is(err, ErrUnreachable) {
		m.failures = failuresToFlip
		m.set(false)
		return
	}
	m.failures = 0
	m.set(true)
}

// set changes the state. Callers hold m.mu.
func (m *Monitor) set(reachable bool) {
	if m.reachable != reachable {
		m.reachable, m.since = reachable, m.clock.Now()
	}
}

// Wrap returns an API that reports the outcome of every call to the monitor.
func (m *Monitor) Wrap(api API) API { return &observed{API: api, m: m} }

type observed struct {
	API
	m *Monitor
}

func (o *observed) LookupSelf(ctx context.Context, token string) (TokenInfo, error) {
	v, err := o.API.LookupSelf(ctx, token)
	o.m.Observe(err)
	return v, err
}

func (o *observed) RenewSelf(ctx context.Context, token string) (time.Duration, error) {
	v, err := o.API.RenewSelf(ctx, token)
	o.m.Observe(err)
	return v, err
}

func (o *observed) Mounts(ctx context.Context, token string) (map[string]string, error) {
	v, err := o.API.Mounts(ctx, token)
	o.m.Observe(err)
	return v, err
}

func (o *observed) Capabilities(ctx context.Context, token string, paths []string) (map[string][]string, error) {
	v, err := o.API.Capabilities(ctx, token, paths)
	o.m.Observe(err)
	return v, err
}

func (o *observed) ClusterConfig(ctx context.Context, token, mount string) (ClusterConfig, error) {
	v, err := o.API.ClusterConfig(ctx, token, mount)
	o.m.Observe(err)
	return v, err
}

func (o *observed) Creds(ctx context.Context, token, mount, role string, req CredsRequest) (Creds, error) {
	v, err := o.API.Creds(ctx, token, mount, role, req)
	o.m.Observe(err)
	return v, err
}
