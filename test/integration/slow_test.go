//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// TestSlow_ConnectionsOpenWhenTheCredentialExpires answers the spec's "open watch or shell after credential
// expiry". It runs only in the nightly workflow (KUBELOOK_IT_SLOW=1), takes up to twelve minutes, and records
// what it saw in .it/findings/credential-expiry.txt without asserting it: the container re-mints a credential
// only at the last instant (vault.remintBefore: 1ns, the nearest to "never" the configuration allows, since 0
// means "use the default"), and a request that finds the credential expired may re-mint it instead of failing.
func TestSlow_ConnectionsOpenWhenTheCredentialExpires(t *testing.T) {
	if os.Getenv("KUBELOOK_IT_SLOW") != "1" {
		t.Skip("slow: set KUBELOOK_IT_SLOW=1 (the nightly workflow does)")
	}
	it.Slow(t)
	c := it.NewClient(t)
	c.Base = "http://localhost:18083"
	final := c.SignInGoogle(it.Account{Sub: "slow-1", Email: "slow-1@corp.example", HD: "corp.example"})
	require.Empty(t, final.Query().Get("signin_error"), "%s", final)
	status, _, apiErr := addToken(t, c, "ALICE_TOKEN")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	status, st, apiErr := open(t, c, "v:alpha", "")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	require.NotNil(t, st.CredentialExpiresAt)

	ns := it.Namespace(t, "alpha")
	w := c.WS()
	watch := w.Open("watch.open", watchConfigMaps("v:alpha", ns))
	watch.Next("snapshot", 30*time.Second)
	shell := w.Open("exec.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": podOf(t, "alpha", "shell"), "cols": 80, "rows": 24})
	awaitShell(t, shell)

	var lines []string
	record := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	t.Cleanup(func() {
		dir := filepath.Join(it.Root(), ".it", "findings")
		_ = os.MkdirAll(dir, 0o750)
		_ = os.WriteFile(filepath.Join(dir, "credential-expiry.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	})

	// wait for the first credential's expiry plus a minute, polling with a list request
	expired := time.Until(*st.CredentialExpiresAt) + time.Minute
	record("credential issued with %s left; waiting up to %s for a request to be refused", time.Until(*st.CredentialExpiresAt).Round(time.Second), expired.Round(time.Second))
	deadline := time.Now().Add(min(expired, 12*time.Minute))
	refused := false
	for time.Now().Before(deadline) {
		if s, _ := listItems(t, c, "v:alpha", "core", "v1", "pods", "demo"); s == http.StatusUnauthorized {
			refused = true
			break
		}
		time.Sleep(15 * time.Second) //nolint:forbidigo // real time on purpose: this test waits for a real credential to expire
	}
	record("a list request was refused with 401 after the expiry: %v", refused)

	it.Kubectl(t, "alpha", "-n", ns, "create", "configmap", "after-expiry", "--from-literal=a=1")
	kind, _ := watch.NextOf(30*time.Second, "event", "error", "closed")
	record("the open watch after the expiry: first frame %q", kind)
	shell.Send("exec.stdin", stdin("echo after-expiry-$((6*7))\n"))
	kind, _ = shell.NextOf(30*time.Second, "out", "error", "exit", "closed")
	record("the open shell after the expiry: first frame %q", kind)
	t.Log(strings.Join(lines, "\n"))
}
