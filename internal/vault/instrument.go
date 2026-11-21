package vault

import (
	"context"
	"errors"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
)

// Instrument returns an API that reports the endpoint, outcome ("ok", "denied" or "error") and duration of
// every call to observe. The endpoint is a fixed name, never a mount or a role. A nil observe returns api.
func Instrument(api API, c clock.Clock, observe func(endpoint, outcome string, d time.Duration)) API {
	if observe == nil {
		return api
	}
	return &timed{API: api, clock: c, observe: observe}
}

type timed struct {
	API
	clock   clock.Clock
	observe func(endpoint, outcome string, d time.Duration)
}

// record observes a call that started at start and ended with err.
func (t *timed) record(endpoint string, start time.Time, err error) {
	outcome := "ok"
	switch {
	case errors.Is(err, ErrPermissionDenied):
		outcome = "denied"
	case err != nil:
		outcome = "error"
	}
	t.observe(endpoint, outcome, t.clock.Now().Sub(start))
}

func (t *timed) LookupSelf(ctx context.Context, token string) (TokenInfo, error) {
	start := t.clock.Now()
	v, err := t.API.LookupSelf(ctx, token)
	t.record("lookup_self", start, err)
	return v, err
}

func (t *timed) RenewSelf(ctx context.Context, token string) (time.Duration, error) {
	start := t.clock.Now()
	v, err := t.API.RenewSelf(ctx, token)
	t.record("renew_self", start, err)
	return v, err
}

func (t *timed) Mounts(ctx context.Context, token string) (map[string]string, error) {
	start := t.clock.Now()
	v, err := t.API.Mounts(ctx, token)
	t.record("mounts", start, err)
	return v, err
}

func (t *timed) Capabilities(ctx context.Context, token string, paths []string) (map[string][]string, error) {
	start := t.clock.Now()
	v, err := t.API.Capabilities(ctx, token, paths)
	t.record("capabilities", start, err)
	return v, err
}

func (t *timed) ClusterConfig(ctx context.Context, token, mount string) (ClusterConfig, error) {
	start := t.clock.Now()
	v, err := t.API.ClusterConfig(ctx, token, mount)
	t.record("cluster_config", start, err)
	return v, err
}

func (t *timed) Creds(ctx context.Context, token, mount, role string, req CredsRequest) (Creds, error) {
	start := t.clock.Now()
	v, err := t.API.Creds(ctx, token, mount, role, req)
	t.record("creds", start, err)
	return v, err
}
