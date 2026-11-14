package actions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func cmTarget(name string) actions.Target {
	return actions.Target{Group: "core", Version: "v1", Resource: "configmaps", Namespace: "ns1", Name: name}
}

func nodeTarget(name string) actions.Target {
	return actions.Target{Group: "core", Version: "v1", Resource: "nodes", Name: name}
}

func bulkJob(t *testing.T, req actions.BulkRequest) *jobs.Job {
	t.Helper()
	raw, err := json.Marshal(req)
	require.NoError(t, err)
	return &jobs.Job{ID: "b1", UserID: sess.UserID, SessionHash: sess.Hash, Cluster: "c", Type: "bulk", Params: raw}
}

func (e *env) bulkHandler() jobs.Handler {
	return actions.NewBulkHandler(actions.Deps{Clients: e.fakes, Clock: e.clock, Audit: e.audit})
}

// audited returns "verb:outcome resource/name" per audit line.
func (e *env) audited(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m struct {
			Verb, Outcome string
			Resource      struct{ Kind, Name string }
		}
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		out = append(out, fmt.Sprintf("%s:%s %s/%s", m.Verb, m.Outcome, m.Resource.Kind, m.Resource.Name))
	}
	return out
}

func TestValidateBulk(t *testing.T) {
	many := make([]actions.Target, 501)
	for i := range many {
		many[i] = cmTarget(fmt.Sprintf("cm-%d", i))
	}
	ok := []actions.BulkRequest{
		{Action: "delete", Targets: []actions.Target{cmTarget("a"), cmTarget("b")}},
		{Action: "restart", Targets: []actions.Target{depT}},
		{Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`{"add":{"env":"prod"},"remove":["old"]}`)},
		{Action: "cordon", Targets: []actions.Target{nodeTarget("n1")}},
		{Action: "uncordon", Targets: []actions.Target{nodeTarget("n1")}},
		{Action: "drain", Targets: []actions.Target{nodeTarget("n1"), nodeTarget("n2")}},
		{Action: "delete", Targets: many[:500]},
	}
	for _, r := range ok {
		assert.NoError(t, actions.ValidateBulk(r), r.Action)
	}
	bad := map[string]actions.BulkRequest{
		"unknown action":      {Action: "explode", Targets: []actions.Target{podT}},
		"no targets":          {Action: "delete"},
		"too many targets":    {Action: "delete", Targets: many},
		"target without name": {Action: "delete", Targets: []actions.Target{{Version: "v1", Resource: "pods"}}},
		"target without kind": {Action: "delete", Targets: []actions.Target{{Name: "x"}}},
		"cordon a pod":        {Action: "cordon", Targets: []actions.Target{podT}},
		"drain a pod":         {Action: "drain", Targets: []actions.Target{podT}},
		"label no params":     {Action: "label", Targets: []actions.Target{podT}},
		"label empty":         {Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`{}`)},
		"label bad key":       {Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`{"add":{"a b":"c"}}`)},
		"label bad value":     {Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`{"add":{"a":"has space"}}`)},
		"label bad json":      {Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`nonsense`)},
		"label bad remove":    {Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`{"remove":["a b"]}`)},
	}
	for name, r := range bad {
		assert.ErrorIs(t, actions.ValidateBulk(r), apperr.ErrInvalid, name)
	}
	assert.ErrorContains(t, actions.ValidateBulk(bad["too many targets"]), "500")
}


func TestBulkHandler_Type(t *testing.T) {
	assert.Equal(t, "bulk", newEnv(t).bulkHandler().Type())
}

func TestBulkDelete_OneAtATimeInOrder_EachAuditedIndividually_AFailureDoesNotStopTheJob(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"cm-a", "cm-b"} {
		require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", n, map[string]any{"a": "1"})))
	}
	req := actions.BulkRequest{Action: "delete", Targets: []actions.Target{cmTarget("cm-b"), cmTarget("missing"), cmTarget("cm-a")}}
	var reports []jobs.Progress
	err := e.bulkHandler().Run(t.Context(), bulkJob(t, req), func(p jobs.Progress) { reports = append(reports, p) })

	require.Error(t, err)
	assert.Equal(t, "1 of 3 items failed", err.Error())
	assert.Equal(t, []string{
		"delete:requested configmaps/cm-b", "delete:success configmaps/cm-b",
		"delete:requested configmaps/missing", "delete:failure configmaps/missing",
		"delete:requested configmaps/cm-a", "delete:success configmaps/cm-a",
	}, e.audited(t))
	require.Len(t, reports, 3)
	last := reports[2]
	assert.Equal(t, 3, last.Done)
	assert.Equal(t, 3, last.Total)
	assert.Equal(t, "Processed 3 of 3 items", last.Text)
	require.Len(t, last.Items, 3)
	assert.Equal(t, jobs.ItemResult{Name: "ns1/cm-b", Status: "done"}, last.Items[0])
	assert.Equal(t, "ns1/missing", last.Items[1].Name)
	assert.Equal(t, "failed", last.Items[1].Status)
	assert.NotEmpty(t, last.Items[1].Message)
	assert.Equal(t, jobs.ItemResult{Name: "ns1/cm-a", Status: "done"}, last.Items[2])
	assert.Len(t, reports[0].Items, 1, "progress is reported after every item")
}

func TestBulkDelete_AllSucceed_ReturnsNil(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", "cm-a", nil)))
	req := actions.BulkRequest{Action: "delete", Targets: []actions.Target{cmTarget("cm-a")}}
	assert.NoError(t, e.bulkHandler().Run(t.Context(), bulkJob(t, req), func(jobs.Progress) {}))
}

func TestBulk_StopsWhenTheContextEnds(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"cm-a", "cm-b"} {
		require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.ConfigMap("ns1", n, nil)))
	}
	ctx, cancel := context.WithCancel(t.Context())
	req := actions.BulkRequest{Action: "delete", Targets: []actions.Target{cmTarget("cm-a"), cmTarget("cm-b")}}
	err := e.bulkHandler().Run(ctx, bulkJob(t, req), func(p jobs.Progress) { cancel() })
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"delete:requested configmaps/cm-a", "delete:success configmaps/cm-a"}, e.audited(t))
}

func TestBulkRestart_RestartsEachWorkload(t *testing.T) {
	e := newEnv(t)
	dep2 := depT
	dep2.Name = "api"
	req := actions.BulkRequest{Action: "restart", Targets: []actions.Target{depT, dep2}}
	require.NoError(t, e.bulkHandler().Run(t.Context(), bulkJob(t, req), func(jobs.Progress) {}))
	require.Len(t, e.patch, 2)
	assert.Equal(t, types.StrategicMergePatchType, e.patch[0].PatchType)
	assert.Equal(t, []string{"restart:requested", "restart:success", "restart:requested", "restart:success"}, e.verbs(t))
}

func TestBulkLabel_MergePatchesAddedAndRemovedLabels(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.Pod("ns", "web-1", nil)))
	req := actions.BulkRequest{Action: "label", Targets: []actions.Target{podT}, Params: json.RawMessage(`{"add":{"env":"prod"},"remove":["old"]}`)}
	require.NoError(t, e.bulkHandler().Run(t.Context(), bulkJob(t, req), func(jobs.Progress) {}))
	p := e.lastPatch(t)
	assert.Equal(t, types.MergePatchType, p.PatchType)
	assert.JSONEq(t, `{"metadata":{"labels":{"env":"prod","old":null}}}`, string(p.Patch))
	assert.Equal(t, []string{"label:requested pods/web-1", "label:success pods/web-1"}, e.audited(t))
}

func TestBulkCordonAndUncordon_Nodes(t *testing.T) {
	for action, want := range map[string]string{"cordon": "true", "uncordon": "false"} {
		e := newEnv(t)
		req := actions.BulkRequest{Action: action, Targets: []actions.Target{nodeTarget("n1"), nodeTarget("n2")}}
		require.NoError(t, e.bulkHandler().Run(t.Context(), bulkJob(t, req), func(jobs.Progress) {}))
		require.Len(t, e.patch, 2)
		assert.JSONEq(t, `{"spec":{"unschedulable":`+want+`}}`, string(e.patch[1].Patch))
		assert.Equal(t, []string{action + ":requested nodes/n1", action + ":success nodes/n1", action + ":requested nodes/n2", action + ":success nodes/n2"}, e.audited(t))
	}
}

func TestBulkDrain_DrainsOneNodeAtATimeInTheGivenOrder(t *testing.T) {
	d := newDrainEnv(t, typedPod("ns", "on-1", "node-1"), typedPod("ns", "on-2", "node-2"))
	req := actions.BulkRequest{Action: "drain", Targets: []actions.Target{nodeTarget("node-2"), nodeTarget("node-1")}}
	var last jobs.Progress
	require.NoError(t, d.bulkHandler().Run(t.Context(), bulkJob(t, req), func(p jobs.Progress) { last = p }))
	assert.Equal(t, []string{"cordon node-2", "evict ns/on-2", "cordon node-1", "evict ns/on-1"}, d.log())
	assert.Equal(t, 2, last.Done)
	assert.Equal(t, []string{"node-2", "node-1"}, []string{last.Items[0].Name, last.Items[1].Name})
}

func TestBulk_BadParameters_Fail(t *testing.T) {
	e := newEnv(t)
	j := bulkJob(t, actions.BulkRequest{Action: "delete"})
	assert.ErrorIs(t, e.bulkHandler().Run(t.Context(), j, func(jobs.Progress) {}), apperr.ErrInvalid)
	j.Params = json.RawMessage(`nonsense`)
	assert.Error(t, e.bulkHandler().Run(t.Context(), j, func(jobs.Progress) {}))
}

func TestBulkRecover_ListsWhichItemsWereDone_AndOffersRetry(t *testing.T) {
	e := newEnv(t)
	j := bulkJob(t, actions.BulkRequest{Action: "delete", Targets: []actions.Target{cmTarget("a"), cmTarget("b"), cmTarget("c"), cmTarget("d"), cmTarget("e")}})
	j.Progress = jobs.Progress{Done: 3, Total: 5, Items: []jobs.ItemResult{
		{Name: "ns1/a", Status: "done"}, {Name: "ns1/b", Status: "failed", Message: "forbidden"}, {Name: "ns1/c", Status: "done"},
	}}
	status, msg, offers := e.bulkHandler().Recover(t.Context(), j)
	assert.Equal(t, jobs.Interrupted, status)
	assert.Contains(t, msg, "2 of 5")
	assert.Contains(t, msg, "ns1/a")
	assert.Contains(t, msg, "ns1/c")
	assert.NotContains(t, msg, "ns1/b")
	assert.Equal(t, []string{"retry"}, offers)
}

func TestBulkRecover_NothingDoneYet(t *testing.T) {
	e := newEnv(t)
	j := bulkJob(t, actions.BulkRequest{Action: "delete", Targets: []actions.Target{cmTarget("a")}})
	_, msg, _ := e.bulkHandler().Recover(t.Context(), j)
	assert.Contains(t, msg, "0 of 1")
}
