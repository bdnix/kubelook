//go:build integration

package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

type snapshotFrame struct {
	Items []map[string]any `json:"items"`
}

func TestWatch_ConfigMaps_SnapshotThenAddedModifiedDeleted_OnBothKubernetesVersions(t *testing.T) {
	for _, cl := range []string{"alpha", "beta"} {
		t.Run(cl, func(t *testing.T) {
			c := devopsClient(t, "watch-"+cl)
			ns := it.Namespace(t, cl)
			w := c.WS()
			ch := w.Open("watch.open", watchConfigMaps("v:"+cl, ns))
			var snap snapshotFrame
			require.NoError(t, json.Unmarshal(ch.Next("snapshot", 30*time.Second), &snap))

			it.Kubectl(t, cl, "-n", ns, "create", "configmap", "w1", "--from-literal=a=1")
			added := waitEvent(t, ch, "ADDED", "w1", 10*time.Second)
			md, _ := added.Object["metadata"].(map[string]any)
			assert.NotContains(t, md, "managedFields", "managedFields never reach the browser")

			it.Kubectl(t, cl, "-n", ns, "patch", "configmap", "w1", "-p", `{"data":{"a":"2"}}`)
			waitEvent(t, ch, "MODIFIED", "w1", 10*time.Second)

			// a second channel with the same key starts from the current state
			second := w.Open("watch.open", watchConfigMaps("v:"+cl, ns))
			var snap2 snapshotFrame
			require.NoError(t, json.Unmarshal(second.Next("snapshot", 30*time.Second), &snap2))
			require.Len(t, snap2.Items, 1)
			assert.Equal(t, "w1", objName(snap2.Items[0]))

			it.Kubectl(t, cl, "-n", ns, "delete", "configmap", "w1")
			waitEvent(t, ch, "DELETED", "w1", 10*time.Second)
		})
	}
}

func TestWatch_HiddenTabIsPausedAfterTheIdleTimeAndResumesWithAFreshSnapshot(t *testing.T) {
	c := devopsClient(t, "watch-pause")
	ns := it.Namespace(t, "alpha")
	w := c.WS()
	ch := w.Open("watch.open", watchConfigMaps("v:alpha", ns))
	ch.Next("snapshot", 30*time.Second)

	w.Control("visibility", map[string]bool{"hidden": true})
	ch.Next("paused", 45*time.Second) // the default idle time is 30 s

	w.Control("visibility", map[string]bool{"hidden": false})
	again := w.Open("watch.open", watchConfigMaps("v:alpha", ns))
	again.Next("snapshot", 30*time.Second)
}
