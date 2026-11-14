package kube_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type openapiEnv struct {
	api   *kube.OpenAPI
	clock *clock.Fake
	hits  atomic.Int32
	paths chan string
	auth  chan string
	body  string
	code  int
}

func newOpenAPIEnv(t *testing.T) *openapiEnv {
	t.Helper()
	e := &openapiEnv{clock: clock.NewFake(t0), paths: make(chan string, 16), auth: make(chan string, 16), body: `{"openapi":"3.0.0"}`, code: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		e.paths <- r.URL.Path
		e.auth <- r.Header.Get("Authorization")
		w.WriteHeader(e.code)
		_, _ = w.Write([]byte(e.body))
	}))
	t.Cleanup(srv.Close)
	fk := fakekube.New()
	fk.RESTConfig = &rest.Config{Host: srv.URL, BearerToken: "user-token"}
	e.api = kube.NewOpenAPI(fk, e.clock)
	return e
}

func TestOpenAPI_ProxiesTheRightPathWithTheUsersCredentials(t *testing.T) {
	e := newOpenAPIEnv(t)
	got, err := e.api.Get(t.Context(), sessA, "c", "core", "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"openapi":"3.0.0"}`, string(got))
	assert.Equal(t, "/openapi/v3/api/v1", <-e.paths)
	assert.Equal(t, "Bearer user-token", <-e.auth)

	_, err = e.api.Get(t.Context(), sessA, "c", "apps", "v1")
	require.NoError(t, err)
	assert.Equal(t, "/openapi/v3/apis/apps/v1", <-e.paths)
}

func TestOpenAPI_CachesForFiveMinutesPerSessionAndCluster(t *testing.T) {
	e := newOpenAPIEnv(t)
	for range 3 {
		_, err := e.api.Get(t.Context(), sessA, "c", "apps", "v1")
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, e.hits.Load())

	_, err := e.api.Get(t.Context(), session.Session{Hash: "other", UserID: "u2"}, "c", "apps", "v1")
	require.NoError(t, err)
	_, err = e.api.Get(t.Context(), sessA, "c2", "apps", "v1")
	require.NoError(t, err)
	assert.EqualValues(t, 3, e.hits.Load(), "another session or cluster has its own entry")

	e.clock.Advance(5*time.Minute + time.Second)
	_, err = e.api.Get(t.Context(), sessA, "c", "apps", "v1")
	require.NoError(t, err)
	assert.EqualValues(t, 4, e.hits.Load())
}

func TestOpenAPI_Errors(t *testing.T) {
	tests := []struct {
		name string
		code int
		is   error
	}{
		{"not served", http.StatusNotFound, apperr.ErrNotFound},
		{"forbidden", http.StatusForbidden, apperr.ErrForbidden},
		{"unauthorized", http.StatusUnauthorized, apperr.ErrForbidden},
		{"server error", http.StatusInternalServerError, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newOpenAPIEnv(t)
			e.code = tc.code
			_, err := e.api.Get(t.Context(), sessA, "c", "apps", "v1")
			require.Error(t, err)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}
			_, err = e.api.Get(t.Context(), sessA, "c", "apps", "v1")
			require.Error(t, err)
			assert.EqualValues(t, 2, e.hits.Load(), "failures are not cached")
		})
	}
}

func TestOpenAPI_RejectsUnsafeNamesAndHugeAnswers(t *testing.T) {
	e := newOpenAPIEnv(t)
	for _, gv := range [][2]string{{"../x", "v1"}, {"apps", "v1/../../x"}, {"", "v1"}, {"apps", ""}, {"core", "v2"}, {"a b", "v1"}} {
		_, err := e.api.Get(t.Context(), sessA, "c", gv[0], gv[1])
		assert.ErrorIs(t, err, apperr.ErrInvalid, "%v", gv)
	}
	assert.Zero(t, e.hits.Load())

	e.body = strings.Repeat("x", kube.MaxOpenAPIBytes+1)
	_, err := e.api.Get(t.Context(), sessA, "c", "apps", "v1")
	assert.ErrorIs(t, err, apperr.ErrTooLarge)
}

func TestOpenAPI_UnknownClusterAndUnreachableServer(t *testing.T) {
	fk := fakekube.New()
	fk.Known = map[string]bool{}
	_, err := kube.NewOpenAPI(fk, clock.NewFake(t0)).Get(t.Context(), sessA, "nope", "apps", "v1")
	assert.ErrorIs(t, err, apperr.ErrClusterUnavailable)

	fk = fakekube.New()
	fk.RESTConfig = &rest.Config{Host: "http://127.0.0.1:1"}
	_, err = kube.NewOpenAPI(fk, clock.NewFake(t0)).Get(t.Context(), sessA, "c", "apps", "v1")
	require.Error(t, err)
}
