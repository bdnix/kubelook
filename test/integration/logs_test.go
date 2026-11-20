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

type linesFrame struct {
	Lines []struct {
		Text string `json:"text"`
	} `json:"lines"`
}

func TestLogs_FollowDeliversLines_OnBothKubernetesVersions(t *testing.T) {
	for _, cl := range []string{"alpha", "beta"} {
		t.Run(cl, func(t *testing.T) {
			c := devopsClient(t, "logs-follow-"+cl)
			pod := podOf(t, cl, "logger")
			w := c.WS()
			ch := w.Open("logs.open", map[string]any{"cluster": "v:" + cl, "namespace": "demo", "pod": pod, "follow": true})
			var lines linesFrame
			require.NoError(t, json.Unmarshal(ch.Next("lines", 30*time.Second), &lines))
			require.NotEmpty(t, lines.Lines)
			assert.Contains(t, lines.Lines[0].Text, `"msg":"tick"`)
		})
	}
}

func TestLogs_TailLines_ReturnsExactlyThatManyThenEOF_AndTheAuditHasNoLogText(t *testing.T) {
	c := devopsClient(t, "logs-tail")
	pod := podOf(t, "alpha", "logger")
	w := c.WS()
	ch := w.Open("logs.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": pod, "follow": false, "tailLines": 5})
	total := 0
	for total < 5 {
		var lines linesFrame
		require.NoError(t, json.Unmarshal(ch.Next("lines", 30*time.Second), &lines))
		total += len(lines.Lines)
	}
	assert.Equal(t, 5, total)
	ch.Next("eof", 30*time.Second)
	ch.Close("logs.close")

	it.Eventually(t, 20*time.Second, func() bool {
		var open, closed bool
		for _, l := range it.AuditLines(t, "kubelook-it-app") {
			if l.Account["email"] != "logs-tail@corp.example" {
				continue
			}
			open = open || l.Verb == "log-view-open"
			closed = closed || l.Verb == "log-view-close"
			assert.NotContains(t, l.Raw, `"msg":"tick"`, "the audit never holds log text")
		}
		return open && closed
	}, "log-view-open and log-view-close are audited")
}
