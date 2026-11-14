package actions_test

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

var (
	nodeT    = actions.Target{Group: "core", Version: "v1", Resource: "nodes", Name: "node-1"}
	nodesAPI = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
)

func node(name string, taints ...map[string]any) *unstructured.Unstructured {
	spec := map[string]any{}
	if len(taints) > 0 {
		list := make([]any, len(taints))
		for i, t := range taints {
			list[i] = t
		}
		spec["taints"] = list
	}
	return fakekube.Object("v1", "Node", "", name, map[string]any{"spec": spec})
}

func (e *env) addNode(t *testing.T, n *unstructured.Unstructured) {
	t.Helper()
	require.NoError(t, e.fakes.Dyn.Tracker().Add(n))
}

func (e *env) taints(t *testing.T, name string) []any {
	t.Helper()
	n, err := e.fakes.Dyn.Resource(nodesAPI).Get(t.Context(), name, metav1.GetOptions{})
	require.NoError(t, err)
	list, _, _ := unstructured.NestedSlice(n.Object, "spec", "taints")
	return list
}

func TestCordonUncordon_MergePatchTheUnschedulableFlag(t *testing.T) {
	for action, want := range map[string]string{"cordon": `{"spec":{"unschedulable":true}}`, "uncordon": `{"spec":{"unschedulable":false}}`} {
		e := newEnv(t)
		require.NoError(t, e.do(action, nodeT, nil))
		p := e.lastPatch(t)
		assert.Equal(t, types.MergePatchType, p.PatchType)
		assert.JSONEq(t, want, string(p.Patch))
		assert.Equal(t, []string{action + ":requested", action + ":success"}, e.verbs(t))
	}
}

func TestCordon_OnAPod_IsInvalid(t *testing.T) {
	e := newEnv(t)
	assert.ErrorIs(t, e.do("cordon", podT, nil), apperr.ErrInvalid)
	assert.Empty(t, e.verbs(t))
}

func TestTaintAdd_AppendsTheTaintAndAuditsIt(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1", map[string]any{"key": "old", "effect": "NoSchedule"}))
	require.NoError(t, e.do("taintAdd", nodeT, map[string]any{"key": "dedicated", "value": "gpu", "effect": "NoExecute"}))
	assert.Equal(t, []any{
		map[string]any{"key": "old", "effect": "NoSchedule"},
		map[string]any{"key": "dedicated", "value": "gpu", "effect": "NoExecute"},
	}, e.taints(t, "node-1"))
	assert.Equal(t, []string{"taint:requested", "taint:success"}, e.verbs(t))
}

func TestTaintAdd_ValueIsOptional(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1"))
	require.NoError(t, e.do("taintAdd", nodeT, map[string]any{"key": "maintenance", "effect": "NoSchedule"}))
	assert.Equal(t, []any{map[string]any{"key": "maintenance", "effect": "NoSchedule"}}, e.taints(t, "node-1"))
}

func TestTaintAdd_DuplicateKeyAndEffect_IsRejectedWithoutAnAuditRecord(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1", map[string]any{"key": "dedicated", "value": "gpu", "effect": "NoSchedule"}))
	err := e.do("taintAdd", nodeT, map[string]any{"key": "dedicated", "value": "other", "effect": "NoSchedule"})
	assert.ErrorIs(t, err, apperr.ErrConflict)
	assert.Empty(t, e.verbs(t))
	// the same key with another effect is a different taint
	require.NoError(t, e.do("taintAdd", nodeT, map[string]any{"key": "dedicated", "effect": "PreferNoSchedule"}))
	assert.Len(t, e.taints(t, "node-1"), 2)
}

func TestTaint_Validation(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1"))
	for name, p := range map[string]map[string]any{
		"no key":         {"effect": "NoSchedule"},
		"bad effect":     {"key": "k", "effect": "Sometimes"},
		"no effect":      {"key": "k"},
		"key with space": {"key": "a b", "effect": "NoSchedule"},
	} {
		assert.ErrorIs(t, e.do("taintAdd", nodeT, p), apperr.ErrInvalid, name)
	}
	assert.ErrorIs(t, e.do("taintAdd", nodeT, nil), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("taintRemove", nodeT, map[string]any{"effect": "NoSchedule"}), apperr.ErrInvalid)
	assert.Empty(t, e.verbs(t))
}

func TestTaintRemove_RemovesOnlyTheMatchingTaint(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1",
		map[string]any{"key": "a", "effect": "NoSchedule"},
		map[string]any{"key": "a", "effect": "NoExecute"},
		map[string]any{"key": "b", "effect": "NoSchedule"}))
	require.NoError(t, e.do("taintRemove", nodeT, map[string]any{"key": "a", "effect": "NoSchedule"}))
	assert.Equal(t, []any{
		map[string]any{"key": "a", "effect": "NoExecute"},
		map[string]any{"key": "b", "effect": "NoSchedule"},
	}, e.taints(t, "node-1"))
	assert.Equal(t, []string{"taint:requested", "taint:success"}, e.verbs(t))
}

func TestTaintRemove_MissingTaint_IsNotFound(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1"))
	assert.ErrorIs(t, e.do("taintRemove", nodeT, map[string]any{"key": "a", "effect": "NoSchedule"}), apperr.ErrNotFound)
	assert.Empty(t, e.verbs(t))
}

// conflicts makes the first n updates of nodes fail with a conflict, as if someone else changed the node.
func (e *env) conflicts(n int32) *atomic.Int32 {
	var seen atomic.Int32
	e.fakes.Dyn.PrependReactor("update", "nodes", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if seen.Add(1) <= n {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, "node-1", nil)
		}
		return false, nil, nil
	})
	return &seen
}

func TestTaint_RetriesOnConflictUpToThreeTimes(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1"))
	seen := e.conflicts(3)
	require.NoError(t, e.do("taintAdd", nodeT, map[string]any{"key": "k", "effect": "NoSchedule"}))
	assert.EqualValues(t, 4, seen.Load())
	assert.Len(t, e.taints(t, "node-1"), 1)
}

func TestTaint_GivesUpAfterThreeRetries(t *testing.T) {
	e := newEnv(t)
	e.addNode(t, node("node-1"))
	seen := e.conflicts(10)
	err := e.do("taintAdd", nodeT, map[string]any{"key": "k", "effect": "NoSchedule"})
	assert.ErrorIs(t, err, apperr.ErrConflict)
	assert.EqualValues(t, 4, seen.Load())
	assert.Equal(t, []string{"taint:requested", "taint:failure"}, e.verbs(t))
}
