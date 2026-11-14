package kube_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type source struct{ calls atomic.Int32 }

func (s *source) RESTConfig(_ context.Context, sess session.Session, id string) (*rest.Config, error) {
	s.calls.Add(1)
	if id == "missing" {
		return nil, apperr.ErrClusterUnavailable
	}
	return &rest.Config{Host: "http://" + sess.Hash + "." + id + ".example"}, nil
}

// countingDisc counts calls that reach the "cluster".
type countingDisc struct {
	discovery.DiscoveryInterface
	n *atomic.Int32
}

func (c countingDisc) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	c.n.Add(1)
	return c.DiscoveryInterface.ServerGroupsAndResources()
}

type clientEnv struct {
	src     *source
	clients kube.Clients
	closers *session.Registry
	clock   *clock.Fake
	builds  atomic.Int32
	disc    atomic.Int32
	fakes   *fakekube.Clients
}

func newClientEnv() *clientEnv {
	e := &clientEnv{src: &source{}, closers: session.NewRegistry(), clock: clock.NewFake(t0), fakes: fakekube.New()}
	e.clients = kube.NewClients(kube.Deps{
		Config: e.src, Clock: e.clock, Closers: e.closers,
		Build: func(*rest.Config) (kube.Built, error) {
			e.builds.Add(1)
			return kube.Built{Dynamic: e.fakes.Dyn, Typed: e.fakes.Kube, Discovery: countingDisc{e.fakes.Disc, &e.disc}}, nil
		},
	})
	return e
}

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

var (
	sessA = session.Session{Hash: "a", UserID: "u1"}
	sessB = session.Session{Hash: "b", UserID: "u2"}
)

func TestClients_SameSessionAndClusterIsCachedOtherSessionIsNot(t *testing.T) {
	e := newClientEnv()
	ctx := t.Context()
	for range 3 {
		_, err := e.clients.Dynamic(ctx, sessA, "v:prod")
		require.NoError(t, err)
		_, err = e.clients.Typed(ctx, sessA, "v:prod")
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, e.builds.Load())
	assert.EqualValues(t, 1, e.src.calls.Load())

	_, err := e.clients.Dynamic(ctx, sessB, "v:prod")
	require.NoError(t, err)
	assert.EqualValues(t, 2, e.builds.Load(), "another session gets its own clients")

	_, err = e.clients.Dynamic(ctx, sessA, "v:other")
	require.NoError(t, err)
	assert.EqualValues(t, 3, e.builds.Load())
}

func TestClients_EndingTheSessionDropsItsClients(t *testing.T) {
	e := newClientEnv()
	_, err := e.clients.Typed(t.Context(), sessA, "v:prod")
	require.NoError(t, err)
	_, err = e.clients.Typed(t.Context(), sessB, "v:prod")
	require.NoError(t, err)
	require.NoError(t, e.closers.Close("a"))

	_, err = e.clients.Typed(t.Context(), sessB, "v:prod")
	require.NoError(t, err)
	assert.EqualValues(t, 2, e.builds.Load(), "the other session keeps its clients")
	_, err = e.clients.Typed(t.Context(), sessA, "v:prod")
	require.NoError(t, err)
	assert.EqualValues(t, 3, e.builds.Load())
}

func TestClients_REST_ReturnsACopy(t *testing.T) {
	e := newClientEnv()
	cfg, err := e.clients.REST(t.Context(), sessA, "v:prod")
	require.NoError(t, err)
	assert.Equal(t, "http://a.v:prod.example", cfg.Host)
	cfg.Host = "changed"
	again, err := e.clients.REST(t.Context(), sessA, "v:prod")
	require.NoError(t, err)
	assert.Equal(t, "http://a.v:prod.example", again.Host)
}

func TestClients_ErrorsFromTheOpenerAreReturned(t *testing.T) {
	e := newClientEnv()
	for _, call := range []func() error{
		func() error { _, err := e.clients.Dynamic(t.Context(), sessA, "missing"); return err },
		func() error { _, err := e.clients.Typed(t.Context(), sessA, "missing"); return err },
		func() error { _, err := e.clients.REST(t.Context(), sessA, "missing"); return err },
		func() error { _, err := e.clients.Discovery(t.Context(), sessA, "missing"); return err },
	} {
		require.ErrorIs(t, call(), apperr.ErrClusterUnavailable)
	}
}

func TestClients_Discovery_CachedForSixtySecondsAndInvalidated(t *testing.T) {
	e := newClientEnv()
	ctx := t.Context()
	d1, err := e.clients.Discovery(ctx, sessA, "v:prod")
	require.NoError(t, err)
	d2, err := e.clients.Discovery(ctx, sessA, "v:prod")
	require.NoError(t, err)
	assert.Same(t, d1, d2)
	assert.EqualValues(t, 1, e.disc.Load())

	e.clock.Advance(59 * time.Second)
	_, _ = e.clients.Discovery(ctx, sessA, "v:prod")
	assert.EqualValues(t, 1, e.disc.Load())
	e.clock.Advance(2 * time.Second)
	_, _ = e.clients.Discovery(ctx, sessA, "v:prod")
	assert.EqualValues(t, 2, e.disc.Load())

	e.clients.(kube.Invalidator).Invalidate(sessA, "v:prod")
	_, _ = e.clients.Discovery(ctx, sessA, "v:prod")
	assert.EqualValues(t, 3, e.disc.Load())
	e.clients.(kube.Invalidator).Invalidate(sessB, "v:none") // unknown: no effect
}

func TestClients_DefaultBuilder_BuildsRealClientsWithoutConnecting(t *testing.T) {
	c := kube.NewClients(kube.Deps{Config: &source{}, Clock: clock.NewFake(t0)})
	typed, err := c.Typed(t.Context(), sessA, "prod")
	require.NoError(t, err)
	assert.NotNil(t, typed)
	dyn, err := c.Dynamic(t.Context(), sessA, "prod")
	require.NoError(t, err)
	assert.NotNil(t, dyn)
}

type badSource struct{}

func (badSource) RESTConfig(context.Context, session.Session, string) (*rest.Config, error) {
	return &rest.Config{Host: "://bad host"}, nil
}

func TestClients_DefaultBuilder_BadConfigFails(t *testing.T) {
	c := kube.NewClients(kube.Deps{Config: badSource{}, Clock: clock.NewFake(t0)})
	_, err := c.Typed(t.Context(), sessA, "v:prod")
	require.Error(t, err)
}
