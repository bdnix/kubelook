//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

const chartRepoURL = "http://kubelook-it-fakeidp:8300/charts"

func helmBase(cluster string) string { return "/api/v1/clusters/v:" + cluster + "/helm" }

// addChartRepo adds the test chart repository to the session under the name "it".
func addChartRepo(t *testing.T, c *it.Client, cluster string) {
	t.Helper()
	var apiErr it.APIError
	status := c.JSON(http.MethodPost, helmBase(cluster)+"/repositories", map[string]string{"name": "it", "url": chartRepoURL}, &apiErr)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
}

type jobResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// helmJob starts a Helm job and waits until it has ended.
func helmJob(t *testing.T, c *it.Client, cluster, action string, req map[string]any) jobResult {
	t.Helper()
	body := map[string]any{"action": action}
	for k, v := range req {
		body[k] = v
	}
	var started struct {
		JobID string `json:"jobId"`
	}
	status := c.JSON(http.MethodPost, helmBase(cluster)+"/jobs", body, &started)
	require.Equal(t, http.StatusOK, status, "starting %s", action)
	require.NotEmpty(t, started.JobID)

	var job jobResult
	it.Eventually(t, 3*time.Minute, func() bool {
		require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/jobs/"+started.JobID, nil, &job))
		return job.Status != "queued" && job.Status != "running"
	}, "the "+action+" job ends")
	return job
}

type releaseRow struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
	ApplyMethod  string `json:"applyMethod"`
}

func listReleases(t *testing.T, c *it.Client, cluster, ns string) []releaseRow {
	t.Helper()
	var out struct {
		Releases []releaseRow `json:"releases"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, helmBase(cluster)+"/releases?namespace="+ns, nil, &out))
	return out.Releases
}

type releaseDetail struct {
	releaseRow
	Manifest     string `json:"manifest"`
	Notes        string `json:"notes"`
	ValuesHidden bool   `json:"valuesHidden"`
}

func releaseDetailOf(t *testing.T, c *it.Client, cluster, ns, name string) (int, releaseDetail) {
	t.Helper()
	var d releaseDetail
	status := c.JSON(http.MethodGet, fmt.Sprintf("%s/releases/%s/%s", helmBase(cluster), ns, name), nil, &d)
	return status, d
}

func revealValues(t *testing.T, c *it.Client, cluster, ns, name string) string {
	t.Helper()
	var out struct {
		UserValues string `json:"userValues"`
	}
	status := c.JSON(http.MethodPost, fmt.Sprintf("%s/releases/%s/%s/values/reveal", helmBase(cluster), ns, name), map[string]int{"revision": 0}, &out)
	require.Equal(t, http.StatusOK, status)
	return out.UserValues
}

func demoChart() map[string]string {
	return map[string]string{"repo": "it", "name": "demo", "version": "0.1.0"}
}

func jobRequest(ns, name string, extra map[string]any) map[string]any {
	req := map[string]any{"namespace": ns, "name": name}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

func kubeconfigOf(cluster string) string {
	return filepath.Join(it.Root(), ".it", "kubeconfig-"+cluster+"-host.yaml")
}

// cli runs a Helm CLI (bin/helm is Helm 4, bin/helm3 is Helm 3) against a cluster and returns its output and error.
func cli(binary, cluster string, args ...string) (string, error) {
	all := append([]string{"--kubeconfig", kubeconfigOf(cluster)}, args...)
	cmd := exec.CommandContext(context.Background(), filepath.Join(it.Root(), "bin", binary), all...) //nolint:gosec // G204: test helper running the pinned tools
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func mustCLI(t *testing.T, binary, cluster string, args ...string) string {
	t.Helper()
	out, err := cli(binary, cluster, args...)
	require.NoError(t, err, "%s %s: %s", binary, strings.Join(args, " "), out)
	return out
}

type cliRelease struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Status   string `json:"status"`
}

// cliReleases is `helm list -n <ns> -o json` of the given CLI.
func cliReleases(t *testing.T, binary, cluster, ns string) []cliRelease {
	t.Helper()
	var rows []cliRelease
	require.NoError(t, json.Unmarshal([]byte(mustCLI(t, binary, cluster, "list", "-n", ns, "-o", "json")), &rows))
	return rows
}

func assertCLISees(t *testing.T, binary, cluster, ns, name string, revision int, status string) {
	t.Helper()
	for _, r := range cliReleases(t, binary, cluster, ns) {
		if r.Name == name {
			assert.Equal(t, strconv.Itoa(revision), r.Revision, "%s: revision of %s", binary, name)
			assert.Equal(t, status, r.Status, "%s: status of %s", binary, name)
			return
		}
	}
	t.Fatalf("%s does not list %s in %s", binary, name, ns)
}

func greetingIn(t *testing.T, cluster, ns, release string) string {
	t.Helper()
	return it.Kubectl(t, cluster, "-n", ns, "get", "configmap", release+"-demo", "-o", "jsonpath={.data.greeting}")
}

func TestHelm_InstallUpgradeRollbackTestUninstall_AsJobs(t *testing.T) {
	c := devopsClient(t, "helm-lifecycle")
	addChartRepo(t, c, "alpha")
	ns := it.Namespace(t, "alpha")

	job := helmJob(t, c, "alpha", "install", jobRequest(ns, "web", map[string]any{"chart": demoChart(), "values": map[string]any{"greeting": "one"}}))
	require.Equal(t, "succeeded", job.Status, job.Message)

	rows := listReleases(t, c, "alpha", ns)
	require.Len(t, rows, 1)
	assert.Equal(t, "web", rows[0].Name)
	assert.Equal(t, 1, rows[0].Revision)
	assert.Equal(t, "deployed", rows[0].Status)
	assert.Equal(t, "0.1.0", rows[0].ChartVersion)
	assertCLISees(t, "helm", "alpha", ns, "web", 1, "deployed")

	status, detail := releaseDetailOf(t, c, "alpha", ns, "web")
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, detail.Notes, "web says one")
	assert.Contains(t, detail.Manifest, "kind: Secret")
	assert.NotContains(t, detail.Manifest, "s3cr3t-demo-value", "the values of Secrets are masked in the manifest")
	assert.True(t, detail.ValuesHidden, "values come only from the reveal call")
	assert.Contains(t, revealValues(t, c, "alpha", ns, "web"), "one")

	job = helmJob(t, c, "alpha", "upgrade", jobRequest(ns, "web", map[string]any{"chart": demoChart(), "values": map[string]any{"greeting": "two"}}))
	require.Equal(t, "succeeded", job.Status, job.Message)
	assert.Equal(t, "two", greetingIn(t, "alpha", ns, "web"))
	assertCLISees(t, "helm", "alpha", ns, "web", 2, "deployed")

	job = helmJob(t, c, "alpha", "rollback", jobRequest(ns, "web", map[string]any{"revision": 1}))
	require.Equal(t, "succeeded", job.Status, job.Message)
	assert.Equal(t, "one", greetingIn(t, "alpha", ns, "web"))
	assertCLISees(t, "helm", "alpha", ns, "web", 3, "deployed")

	job = helmJob(t, c, "alpha", "test", jobRequest(ns, "web", nil))
	assert.Equal(t, "succeeded", job.Status, job.Message)

	job = helmJob(t, c, "alpha", "uninstall", jobRequest(ns, "web", nil))
	require.Equal(t, "succeeded", job.Status, job.Message)
	assert.Empty(t, cliReleases(t, "helm", "alpha", ns))
	status, _ = releaseDetailOf(t, c, "alpha", ns, "web")
	assert.Equal(t, http.StatusNotFound, status)
}

func TestHelm_ChartLargerThanTheLimitFailsTheJob(t *testing.T) {
	c := devopsClient(t, "helm-too-large")
	addChartRepo(t, c, "alpha")
	ns := it.Namespace(t, "alpha")

	big := map[string]string{"repo": "it", "name": "demo-big", "version": "0.1.0"}
	job := helmJob(t, c, "alpha", "install", jobRequest(ns, "big", map[string]any{"chart": big}))
	assert.Equal(t, "failed", job.Status)
	assert.Contains(t, job.Message, "larger than 1048576 bytes")
	assert.Empty(t, listReleases(t, c, "alpha", ns), "nothing was installed")
}

// findings collects what the other direction (Helm 3 on a release that Kubelook changed) did. It is recorded, not required.
type findings struct{ lines []string }

func (f *findings) record(format string, args ...any) {
	f.lines = append(f.lines, fmt.Sprintf(format, args...))
}

func (f *findings) write(t *testing.T) {
	t.Helper()
	dir := filepath.Join(it.Root(), ".it", "findings")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	file, err := os.OpenFile(filepath.Join(dir, "helm-cross-version.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: a fixed path under .it
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	_, err = file.WriteString(strings.Join(f.lines, "\n") + "\n")
	require.NoError(t, err)
}

func (f *findings) try(binary, cluster string, args ...string) {
	out, err := cli(binary, cluster, args...)
	outcome := "ok"
	if err != nil {
		outcome = "FAILED"
	}
	f.record("%s %s: %s\n    %s", binary, strings.Join(args, " "), outcome, strings.ReplaceAll(out, "\n", "\n    "))
}

func TestHelm_ReleasesMadeByHelm3_AreManagedThroughKubelookAndStayManageableByHelm3(t *testing.T) {
	c := devopsClient(t, "helm-legacy")
	for _, cluster := range []string{"alpha", "beta"} {
		t.Run(cluster, func(t *testing.T) {
			addChartRepo(t, c, cluster)
			ns := it.Namespace(t, cluster)
			packaged := filepath.Join(it.Root(), ".it", "charts", "demo-0.1.0.tgz")
			f := &findings{}
			f.record("== %s: namespace %s, Helm 4 CLI %s, Helm 3 CLI %s", cluster, ns,
				firstLine(mustCLI(t, "helm", cluster, "version", "--short")), firstLine(mustCLI(t, "helm3", cluster, "version", "--short")))
			t.Cleanup(func() { f.write(t) })

			// 1. Helm 3 creates revision 1.
			mustCLI(t, "helm3", cluster, "install", "legacy", packaged, "-n", ns, "--set", "greeting=one")
			assertCLISees(t, "helm3", cluster, ns, "legacy", 1, "deployed")

			// 2. Kubelook lists it and shows notes, the masked manifest and, after the reveal, the values.
			rows := listReleases(t, c, cluster, ns)
			require.Len(t, rows, 1)
			assert.Equal(t, releaseRow{Name: "legacy", Namespace: ns, Revision: 1, Status: "deployed", Chart: "demo", ChartVersion: "0.1.0", ApplyMethod: rows[0].ApplyMethod}, rows[0])
			status, detail := releaseDetailOf(t, c, cluster, ns, "legacy")
			require.Equal(t, http.StatusOK, status)
			assert.Contains(t, detail.Notes, "legacy says one")
			assert.NotContains(t, detail.Manifest, "s3cr3t-demo-value")
			assert.Contains(t, revealValues(t, c, cluster, ns, "legacy"), "one")

			// 3. Upgrade through Kubelook: revision 2, and the release stays on client-side apply.
			job := helmJob(t, c, cluster, "upgrade", jobRequest(ns, "legacy", map[string]any{"chart": demoChart(), "values": map[string]any{"greeting": "two"}}))
			require.Equal(t, "succeeded", job.Status, job.Message)
			assert.Equal(t, "two", greetingIn(t, cluster, ns, "legacy"))
			_, detail = releaseDetailOf(t, c, cluster, ns, "legacy")
			assert.Equal(t, 2, detail.Revision)
			assert.Equal(t, "csa", detail.ApplyMethod)
			managed := it.Kubectl(t, cluster, "-n", ns, "get", "configmap", "legacy-demo", "--show-managed-fields", "-o", "json")
			var cm struct {
				Metadata struct {
					ManagedFields []struct {
						Manager   string `json:"manager"`
						Operation string `json:"operation"`
					} `json:"managedFields"`
				} `json:"metadata"`
			}
			require.NoError(t, json.Unmarshal([]byte(managed), &cm))
			require.NotEmpty(t, cm.Metadata.ManagedFields)
			for _, mf := range cm.Metadata.ManagedFields {
				assert.NotEqual(t, "Apply", mf.Operation, "no field moved to a server-side-apply manager (%s)", mf.Manager)
			}

			// 4. Rollback through Kubelook to revision 1 is revision 3.
			job = helmJob(t, c, cluster, "rollback", jobRequest(ns, "legacy", map[string]any{"revision": 1}))
			require.Equal(t, "succeeded", job.Status, job.Message)
			assert.Equal(t, "one", greetingIn(t, cluster, ns, "legacy"))

			// 5. Helm 3 still manages it.
			assertCLISees(t, "helm3", cluster, ns, "legacy", 3, "deployed")
			var history []map[string]any
			require.NoError(t, json.Unmarshal([]byte(mustCLI(t, "helm3", cluster, "history", "legacy", "-n", ns, "-o", "json")), &history))
			assert.Len(t, history, 3)
			var values map[string]any
			require.NoError(t, json.Unmarshal([]byte(mustCLI(t, "helm3", cluster, "get", "values", "legacy", "-n", ns, "-o", "json")), &values))
			assert.Equal(t, "one", values["greeting"])

			// The other direction is recorded, not required.
			f.try("helm3", cluster, "upgrade", "legacy", packaged, "-n", ns, "--set", "greeting=three")
			f.try("helm3", cluster, "rollback", "legacy", "1", "-n", ns)
			job = helmJob(t, c, cluster, "install", jobRequest(ns, "modern", map[string]any{"chart": demoChart(), "values": map[string]any{"greeting": "one"}}))
			require.Equal(t, "succeeded", job.Status, job.Message)
			out, err := cli("helm3", cluster, "list", "-n", ns)
			require.NoError(t, err, "helm3 list must work on a release installed through Kubelook: %s", out)
			f.record("helm3 list -n %s:\n    %s", ns, strings.ReplaceAll(out, "\n", "\n    "))
			f.try("helm3", cluster, "upgrade", "modern", packaged, "-n", ns, "--set", "greeting=three")
			f.try("helm3", cluster, "rollback", "modern", "1", "-n", ns)

			// 6. Uninstall through Kubelook.
			job = helmJob(t, c, cluster, "uninstall", jobRequest(ns, "legacy", nil))
			require.Equal(t, "succeeded", job.Status, job.Message)
			for _, r := range cliReleases(t, "helm3", cluster, ns) {
				assert.NotEqual(t, "legacy", r.Name, "helm3 no longer lists the uninstalled release")
			}
		})
	}
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }
