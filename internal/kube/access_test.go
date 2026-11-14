package kube_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type accessEnv struct {
	access     *kube.Access
	fk         *fakekube.Clients
	clock      *clock.Fake
	closers    *session.Registry
	rulesCalls atomic.Int32
	ssarCalls  atomic.Int32
	rules      []authv1.ResourceRule
	incomplete bool
	rulesErr   error
	ssar       func(attrs *authv1.ResourceAttributes) (bool, error)
	lastAttrs  *authv1.ResourceAttributes
}

func newAccessEnv() *accessEnv {
	e := &accessEnv{fk: fakekube.New(), clock: clock.NewFake(t0), closers: session.NewRegistry()}
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return false, nil }
	e.fk.Kube.PrependReactor("create", "selfsubjectrulesreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		e.rulesCalls.Add(1)
		if e.rulesErr != nil {
			return true, nil, e.rulesErr
		}
		return true, &authv1.SelfSubjectRulesReview{Status: authv1.SubjectRulesReviewStatus{ResourceRules: e.rules, Incomplete: e.incomplete}}, nil
	})
	e.fk.Kube.PrependReactor("create", "selfsubjectaccessreviews", func(a clienttesting.Action) (bool, runtime.Object, error) {
		e.ssarCalls.Add(1)
		review := a.(clienttesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		e.lastAttrs = review.Spec.ResourceAttributes
		allowed, err := e.ssar(review.Spec.ResourceAttributes)
		if err != nil {
			return true, nil, err
		}
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: allowed}}, nil
	})
	e.access = kube.NewAccess(e.fk, e.clock, e.closers)
	return e
}

func (e *accessEnv) can(t *testing.T, c kube.Check) (bool, string) {
	t.Helper()
	ok, reason, err := e.access.Can(t.Context(), sessA, "c", c)
	require.NoError(t, err)
	return ok, reason
}

var listPods = kube.Check{Verb: "list", Resource: "pods", Namespace: "ns1"}

func TestCan_RulesReviewAllows_NoAccessReview(t *testing.T) {
	e := newAccessEnv()
	e.rules = []authv1.ResourceRule{{Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"}}}
	ok, reason := e.can(t, listPods)
	assert.True(t, ok)
	assert.Empty(t, reason)
	assert.EqualValues(t, 0, e.ssarCalls.Load())
}

func TestCan_RulesReviewDoesNotAllow_AccessReviewDecides(t *testing.T) {
	e := newAccessEnv()
	e.rules = []authv1.ResourceRule{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods"}}}
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	ok, _ := e.can(t, listPods)
	assert.True(t, ok)
	assert.EqualValues(t, 1, e.ssarCalls.Load())
	assert.Equal(t, "ns1", e.lastAttrs.Namespace)
	assert.Equal(t, "list", e.lastAttrs.Verb)

	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return false, nil }
	ok, reason := e.can(t, kube.Check{Verb: "delete", Resource: "pods", Subresource: "exec", Namespace: "ns1"})
	assert.False(t, ok)
	assert.Equal(t, "missing permission: delete pods/exec", reason)
	ok, reason = e.can(t, kube.Check{Verb: "delete", Resource: "pods", Namespace: "ns1"})
	assert.False(t, ok)
	assert.Equal(t, "missing permission: delete pods", reason)
}

func TestCan_IncompleteRulesAreOnlyAFirstPass(t *testing.T) {
	e := newAccessEnv()
	e.incomplete = true
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	ok, _ := e.can(t, listPods)
	assert.True(t, ok, "the access review overrides a negative first pass")
	assert.EqualValues(t, 1, e.ssarCalls.Load())
}

func TestCan_RuleMatching(t *testing.T) {
	for name, tc := range map[string]struct {
		rule  authv1.ResourceRule
		check kube.Check
		want  bool
	}{
		"all wildcards":        {authv1.ResourceRule{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}, kube.Check{Verb: "delete", Group: "apps", Resource: "deployments", Namespace: "n"}, true},
		"wrong verb":           {authv1.ResourceRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods"}}, kube.Check{Verb: "delete", Resource: "pods", Namespace: "n"}, false},
		"wrong group":          {authv1.ResourceRule{Verbs: []string{"get"}, APIGroups: []string{"apps"}, Resources: []string{"pods"}}, kube.Check{Verb: "get", Resource: "pods", Namespace: "n"}, false},
		"wrong resource":       {authv1.ResourceRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"secrets"}}, kube.Check{Verb: "get", Resource: "pods", Namespace: "n"}, false},
		"subresource exact":    {authv1.ResourceRule{Verbs: []string{"create"}, APIGroups: []string{""}, Resources: []string{"pods/exec"}}, kube.Check{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: "n"}, true},
		"pods does not cover":  {authv1.ResourceRule{Verbs: []string{"create"}, APIGroups: []string{""}, Resources: []string{"pods"}}, kube.Check{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: "n"}, false},
		"pods/* covers":        {authv1.ResourceRule{Verbs: []string{"create"}, APIGroups: []string{""}, Resources: []string{"pods/*"}}, kube.Check{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: "n"}, true},
		"*/exec covers":        {authv1.ResourceRule{Verbs: []string{"create"}, APIGroups: []string{""}, Resources: []string{"*/exec"}}, kube.Check{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: "n"}, true},
		"*/exec not plain":     {authv1.ResourceRule{Verbs: []string{"create"}, APIGroups: []string{""}, Resources: []string{"*/exec"}}, kube.Check{Verb: "create", Resource: "pods", Namespace: "n"}, false},
		"names allow match":    {authv1.ResourceRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"db"}}, kube.Check{Verb: "get", Resource: "secrets", Name: "db", Namespace: "n"}, true},
		"names reject other":   {authv1.ResourceRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"db"}}, kube.Check{Verb: "get", Resource: "secrets", Name: "other", Namespace: "n"}, false},
		"names reject no name": {authv1.ResourceRule{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"db"}}, kube.Check{Verb: "list", Resource: "secrets", Namespace: "n"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			e := newAccessEnv()
			e.rules = []authv1.ResourceRule{tc.rule}
			e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return false, nil }
			ok, _ := e.can(t, tc.check)
			assert.Equal(t, tc.want, ok)
			assert.Equal(t, !tc.want, e.ssarCalls.Load() == 1, "a rule match skips the access review")
		})
	}
}

func TestCan_ClusterScopedSkipsTheRulesReview(t *testing.T) {
	e := newAccessEnv()
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	ok, _ := e.can(t, kube.Check{Verb: "list", Resource: "nodes"})
	assert.True(t, ok)
	assert.EqualValues(t, 0, e.rulesCalls.Load())
	assert.EqualValues(t, 1, e.ssarCalls.Load())
}

func TestCan_RulesReviewFailureFallsBackToTheAccessReview(t *testing.T) {
	e := newAccessEnv()
	e.rulesErr = errors.New("not supported")
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	ok, _ := e.can(t, listPods)
	assert.True(t, ok)
}

func TestCan_CachedForSixtySecondsErrorsNotCached(t *testing.T) {
	e := newAccessEnv()
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	e.can(t, listPods)
	e.can(t, listPods)
	assert.EqualValues(t, 1, e.ssarCalls.Load())
	e.clock.Advance(59 * time.Second)
	e.can(t, listPods)
	assert.EqualValues(t, 1, e.ssarCalls.Load())
	e.clock.Advance(2 * time.Second)
	e.can(t, listPods)
	assert.EqualValues(t, 2, e.ssarCalls.Load())

	other := listPods
	other.Name = "x"
	e.can(t, other)
	assert.EqualValues(t, 3, e.ssarCalls.Load(), "each check has its own entry")

	e.ssar = func(*authv1.ResourceAttributes) (bool, error) {
		return false, apierrors.NewForbidden(schema.GroupResource{Resource: "selfsubjectaccessreviews"}, "", errors.New("no"))
	}
	e.clock.Advance(2 * time.Minute)
	_, _, err := e.access.Can(t.Context(), sessA, "c", listPods)
	require.ErrorIs(t, err, apperr.ErrForbidden)
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	ok, _ := e.can(t, listPods)
	assert.True(t, ok, "the failure was not cached")
}

func TestCan_CacheIsPerSessionAndEndsWithTheSession(t *testing.T) {
	e := newAccessEnv()
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	e.can(t, listPods)
	_, _, err := e.access.Can(t.Context(), sessB, "c", listPods)
	require.NoError(t, err)
	assert.EqualValues(t, 2, e.ssarCalls.Load())

	require.NoError(t, e.closers.Close("a"))
	_, _, err = e.access.Can(t.Context(), sessB, "c", listPods)
	require.NoError(t, err)
	assert.EqualValues(t, 2, e.ssarCalls.Load(), "the other session keeps its entries")
	assert.Equal(t, 1, e.closers.Count("b"))
}

func TestCan_UnknownClusterAndTypedFailure(t *testing.T) {
	e := newAccessEnv()
	e.fk.Known = map[string]bool{}
	_, _, err := e.access.Can(t.Context(), sessA, "c", listPods)
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestCanAll_OrderAndLimit(t *testing.T) {
	e := newAccessEnv()
	e.ssar = func(a *authv1.ResourceAttributes) (bool, error) { return a.Resource == "nodes", nil }
	res, err := e.access.CanAll(t.Context(), sessA, "c", []kube.Check{
		{Verb: "list", Resource: "pods"}, {Verb: "list", Resource: "nodes"},
	})
	require.NoError(t, err)
	assert.Equal(t, []kube.Result{{Allowed: false, Reason: "missing permission: list pods"}, {Allowed: true}}, res)

	_, err = e.access.CanAll(t.Context(), sessA, "c", make([]kube.Check, 101))
	require.ErrorIs(t, err, apperr.ErrInvalid)

	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return false, errors.New("boom") }
	_, err = e.access.CanAll(t.Context(), sessA, "c", []kube.Check{{Verb: "get", Resource: "x"}})
	require.Error(t, err)
}

func TestCanExec_IsCreateOnPodsExec(t *testing.T) {
	e := newAccessEnv()
	e.ssar = func(*authv1.ResourceAttributes) (bool, error) { return true, nil }
	ok, _, err := e.access.CanExec(t.Context(), sessA, "c", "ns1", "web-1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, &authv1.ResourceAttributes{Namespace: "ns1", Verb: "create", Resource: "pods", Subresource: "exec", Name: "web-1"}, e.lastAttrs)
}
