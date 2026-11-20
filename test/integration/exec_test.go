//go:build integration

package integration

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

func stdin(s string) map[string]string {
	return map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(s))}
}

// readUntil collects "out" frames until their text contains want.
func readUntil(t *testing.T, ch *it.Channel, want string, within time.Duration) {
	t.Helper()
	var got strings.Builder
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		kind, raw := ch.NextOf(time.Until(deadline), "out", "error", "exit")
		if kind == "" {
			break
		}
		if kind != "out" {
			t.Fatalf("the shell sent %s instead of output: %s", kind, raw)
		}
		var o struct{ Data string }
		require.NoError(t, json.Unmarshal(raw, &o))
		b, err := base64.StdEncoding.DecodeString(o.Data)
		require.NoError(t, err)
		got.Write(b)
		if strings.Contains(got.String(), want) {
			return
		}
	}
	t.Fatalf("output %q never contained %q", got.String(), want)
}

// awaitShell waits for the shell's own "opened" frame, which names the shell that started. Input sent before it
// would stop Kubelook from trying the next shell when the first one is missing, as a real terminal never does.
func awaitShell(t *testing.T, ch *it.Channel) string {
	t.Helper()
	kind, raw := ch.NextOf(60*time.Second, "opened", "error", "exit")
	require.Equal(t, "opened", kind, "the shell did not start: %s", raw)
	var opened struct{ Shell string }
	require.NoError(t, json.Unmarshal(raw, &opened))
	return opened.Shell
}

func TestExec_Shell_FallsBackToShAndEchoes_OnBothKubernetesVersions(t *testing.T) {
	for _, cl := range []string{"alpha", "beta"} {
		t.Run(cl, func(t *testing.T) {
			c := devopsClient(t, "exec-"+cl)
			pod := podOf(t, cl, "shell")
			w := c.WS()
			ch := w.Open("exec.open", map[string]any{"cluster": "v:" + cl, "namespace": "demo", "pod": pod, "cols": 80, "rows": 24})
			// the mux's "opened" was consumed by Open; the shell's own "opened" names the shell it found
			assert.Equal(t, "/bin/sh", awaitShell(t, ch), "busybox has no bash, so the fallback to sh is used")
			ch.Send("exec.stdin", stdin("echo kubelook-$((6*7))\n"))
			readUntil(t, ch, "kubelook-42", 30*time.Second)
			ch.Send("exec.resize", map[string]int{"cols": 100, "rows": 30})
			ch.Send("exec.stdin", stdin("exit\n"))
			var exit struct{ Code int }
			require.NoError(t, json.Unmarshal(ch.Next("exit", 30*time.Second), &exit))
			assert.Equal(t, 0, exit.Code)
		})
	}
}

func TestExec_Audit_RecordsStartAndStopButNeverTheOutput(t *testing.T) {
	c := devopsClient(t, "exec-audit")
	pod := podOf(t, "alpha", "shell")
	ch := c.WS().Open("exec.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": pod, "cols": 80, "rows": 24})
	awaitShell(t, ch)
	ch.Send("exec.stdin", stdin("echo kubelook-$((6*7))\n"))
	readUntil(t, ch, "kubelook-42", 30*time.Second)
	ch.Send("exec.stdin", stdin("exit\n"))
	ch.Next("exit", 30*time.Second)

	it.Eventually(t, 20*time.Second, func() bool {
		var start, stop bool
		for _, l := range it.AuditLines(t, "kubelook-it-app") {
			if l.Account["email"] != "exec-audit@corp.example" {
				continue
			}
			start = start || l.Verb == "exec-start"
			stop = stop || l.Verb == "exec-stop"
			assert.NotContains(t, l.Raw, "kubelook-42")
		}
		return start && stop
	}, "exec-start and exec-stop are audited")
}
