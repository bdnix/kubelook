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

// Logs returns everything the Kubelook container wrote so far (standard output and error).
func Logs(t testing.TB, container string) string {
	t.Helper()
	return Docker(t, "logs", container)
}

// AuditLines returns the audit records in the container's log. Lines that are not audit JSON are skipped.
func AuditLines(t testing.TB, container string) []AuditLine {
	t.Helper()
	var out []AuditLine
	sc := bufio.NewScanner(strings.NewReader(Logs(t, container)))
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

// DumpLogOnFailure prints the last lines of the container's log (without audit lines) when the test fails.
func DumpLogOnFailure(t testing.TB, container string) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		var keep []string
		for _, line := range strings.Split(Docker(t, "logs", "--tail", "400", container), "\n") {
			if !strings.Contains(line, `"audit":true`) && !strings.Contains(line, `"msg":"request"`) {
				keep = append(keep, line)
			}
		}
		if len(keep) > 40 {
			keep = keep[len(keep)-40:]
		}
		t.Logf("last lines of the %s log:\n%s", container, strings.Join(keep, "\n"))
	})
}
