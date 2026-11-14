package kube

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
)

const (
	accessTTL = 60 * time.Second
	// MaxChecks is the most checks one request may carry.
	MaxChecks = 100
	// maxCachedChecks bounds the memory of the check cache; a full cache is emptied.
	maxCachedChecks = 20000
)

// Check asks whether the user may do something.
type Check struct {
	Verb        string `json:"verb"`
	Group       string `json:"group"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
}

// Result is the answer to one Check.
type Result struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

type cacheKey struct {
	hash, cluster string
	check         Check
}

type cacheVal struct {
	res Result
	at  time.Time
}

// Access answers permission questions for the UI. SelfSubjectRulesReview is a first pass that can
// only say yes; SelfSubjectAccessReview decides everything else. Mutations are never pre-checked
// by the server: the API server is the authority.
type Access struct {
	clients Clients
	clock   clock.Clock
	closers *session.Registry

	mu       sync.Mutex
	cache    map[cacheKey]cacheVal
	sessions map[string]bool
}

// NewAccess returns the access checker. closers may be nil.
func NewAccess(c Clients, clk clock.Clock, closers *session.Registry) *Access {
	return &Access{clients: c, clock: clk, closers: closers, cache: map[cacheKey]cacheVal{}, sessions: map[string]bool{}}
}

func (c Check) resourceName() string {
	if c.Subresource != "" {
		return c.Resource + "/" + c.Subresource
	}
	return c.Resource
}

func (a *Access) cached(k cacheKey) (Result, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.cache[k]
	if !ok || a.clock.Now().Sub(v.at) >= accessTTL {
		delete(a.cache, k)
		return Result{}, false
	}
	return v.res, true
}

func (a *Access) store(k cacheKey, r Result) {
	a.mu.Lock()
	if len(a.cache) >= maxCachedChecks {
		clear(a.cache)
	}
	a.cache[k] = cacheVal{r, a.clock.Now()}
	first := !a.sessions[k.hash]
	a.sessions[k.hash] = true
	a.mu.Unlock()
	if first && a.closers != nil {
		a.closers.Register(k.hash, "access-cache", func() error { a.forget(k.hash); return nil })
	}
}

func (a *Access) forget(hash string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, hash)
	for k := range a.cache {
		if k.hash == hash {
			delete(a.cache, k)
		}
	}
}

// Can reports whether the session's user may do what the check describes, and why not.
func (a *Access) Can(ctx context.Context, sess session.Session, clusterID string, c Check) (bool, string, error) {
	k := cacheKey{sess.Hash, clusterID, c}
	if r, ok := a.cached(k); ok {
		return r.Allowed, r.Reason, nil
	}
	typed, err := a.clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return false, "", err
	}
	if c.Namespace != "" {
		// A failed first pass is not an error: the access review decides.
		if rev, err := typed.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx,
			&authv1.SelfSubjectRulesReview{Spec: authv1.SelfSubjectRulesReviewSpec{Namespace: c.Namespace}}, metav1.CreateOptions{}); err == nil {
			for _, rule := range rev.Status.ResourceRules {
				if ruleAllows(rule, c) {
					a.store(k, Result{Allowed: true})
					return true, "", nil
				}
			}
		}
	}
	rev, err := typed.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authv1.ResourceAttributes{
			Namespace: c.Namespace, Verb: c.Verb, Group: c.Group, Resource: c.Resource, Subresource: c.Subresource, Name: c.Name,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, "", MapError(err)
	}
	res := Result{Allowed: rev.Status.Allowed}
	if !res.Allowed {
		res.Reason = fmt.Sprintf("missing permission: %s %s", c.Verb, c.resourceName())
	}
	a.store(k, res)
	return res.Allowed, res.Reason, nil
}

// CanAll answers the checks in order.
func (a *Access) CanAll(ctx context.Context, sess session.Session, clusterID string, checks []Check) ([]Result, error) {
	if len(checks) > MaxChecks {
		return nil, fmt.Errorf("at most %d checks per request: %w", MaxChecks, apperr.ErrInvalid)
	}
	out := make([]Result, len(checks))
	for i, c := range checks {
		allowed, reason, err := a.Can(ctx, sess, clusterID, c)
		if err != nil {
			return nil, err
		}
		out[i] = Result{Allowed: allowed, Reason: reason}
	}
	return out, nil
}

// CanExec reports whether the user may open a shell in the pod: "create" on pods/exec (Kubernetes
// 1.35 requires create for WebSocket exec).
func (a *Access) CanExec(ctx context.Context, sess session.Session, clusterID, ns, pod string) (bool, string, error) {
	return a.Can(ctx, sess, clusterID, Check{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: ns, Name: pod})
}

// ruleAllows reports whether one RBAC rule grants the check.
func ruleAllows(rule authv1.ResourceRule, c Check) bool {
	if !matchesAny(rule.Verbs, c.Verb) || !matchesAny(rule.APIGroups, c.Group) {
		return false
	}
	ok := false
	for _, r := range rule.Resources {
		if resourceMatches(r, c) {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	if len(rule.ResourceNames) > 0 {
		return c.Name != "" && slices.Contains(rule.ResourceNames, c.Name)
	}
	return true
}

func matchesAny(list []string, want string) bool {
	return slices.Contains(list, "*") || slices.Contains(list, want)
}

// resourceMatches follows Kubernetes' rule matching for "pods", "pods/exec", "*", "*/exec" and "pods/*".
func resourceMatches(rule string, c Check) bool {
	full := c.resourceName()
	switch {
	case rule == "*", rule == full:
		return true
	case c.Subresource != "" && rule == "*/"+c.Subresource:
		return true
	case c.Subresource != "" && strings.HasSuffix(rule, "/*") && strings.TrimSuffix(rule, "/*") == c.Resource:
		return true
	}
	return false
}
