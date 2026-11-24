//go:build integration

package it

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

// AuditLine is one audit record as Kubelook writes it to standard output.
type AuditLine struct {
	Verb    string         `json:"verb"`
	Outcome string         `json:"outcome"`
	Cluster string         `json:"cluster"`
	Account map[string]any `json:"account"`
	Vault   map[string]any `json:"vault"`
	Details map[string]any `json:"details"`
	Raw     string         `json:"-"`
}

// podSelector selects the Kubelook pods of the release in the host cluster (not its migrate or cleanup jobs).
const podSelector = "app.kubernetes.io/name=kubelook,app.kubernetes.io/instance=kubelook,!app.kubernetes.io/component"

// Logs returns what the Kubelook pods have written so far (standard output and error), all replicas together.
func Logs(t testing.TB) string {
	t.Helper()
	return Kubectl(t, "host", "-n", "kubelook", "logs", "-l", podSelector, "--tail=-1", "--max-log-requests=10")
}

// AuditLines returns the audit records in the log of the Kubelook pods. Lines that are not audit JSON are skipped.
func AuditLines(t testing.TB) []AuditLine {
	t.Helper()
	var out []AuditLine
	sc := bufio.NewScanner(strings.NewReader(Logs(t)))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		raw := sc.Text()
		if !strings.Contains(raw, `"audit":true`) {
			continue
		}
		var l AuditLine
		if json.Unmarshal([]byte(raw), &l) == nil {
			l.Raw = raw
			out = append(out, l)
		}
	}
	return out
}

// DumpLogOnFailure prints the last lines of the Kubelook pods' log (without audit and request lines) when the test fails.
func DumpLogOnFailure(t testing.TB) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		var keep []string
		for _, line := range strings.Split(Logs(t), "\n") {
			if !strings.Contains(line, `"audit":true`) && !strings.Contains(line, `"msg":"request"`) {
				keep = append(keep, line)
			}
		}
		if len(keep) > 40 {
			keep = keep[len(keep)-40:]
		}
		t.Logf("last lines of the Kubelook log:\n%s", strings.Join(keep, "\n"))
	})
}
