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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

const alphaKCCluster = "k:alpha-kc/kind-kubelook-it-alpha"

// alphaKubeconfig is the kubeconfig whose server address the Kubelook container can reach.
func alphaKubeconfig(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(it.Root(), ".it", "kubeconfig-alpha-internal.yaml"))
	require.NoError(t, err)
	return data
}

// tokenKubeconfig builds a kubeconfig that signs in with a service account token bound to ClusterRole "view".
func tokenKubeconfig(t *testing.T) []byte {
	t.Helper()
	ns := it.Namespace(t, "alpha")
	it.Kubectl(t, "alpha", "-n", ns, "create", "serviceaccount", "viewer")
	it.Kubectl(t, "alpha", "create", "clusterrolebinding", "it-viewer-"+ns, "--clusterrole=view", "--serviceaccount="+ns+":viewer")
	t.Cleanup(func() { it.Kubectl(t, "alpha", "delete", "clusterrolebinding", "it-viewer-"+ns, "--ignore-not-found") })
	token := it.Kubectl(t, "alpha", "-n", ns, "create", "token", "viewer", "--duration=1h")
	ca := it.Kubectl(t, "alpha", "config", "view", "--raw", "--minify", "-o", "jsonpath={.clusters[0].cluster.certificate-authority-data}")
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: alpha
    cluster:
      server: https://kubelook-it-alpha-control-plane:6443
      certificate-authority-data: %s
users:
  - name: viewer
    user:
      token: %s
contexts:
  - name: alpha-viewer
    context: {cluster: alpha, user: viewer}
current-context: alpha-viewer
`, ca, token))
}

func TestKubeconfig_Upload_OpensAndListsTheClusterWithTheFilesCredentials(t *testing.T) {
	c := signedIn(t, "kc-upload")
	c.UploadKubeconfig("alpha-kc", alphaKubeconfig(t))

	found := false
	for _, s := range clusters(t, c) {
		for _, e := range s.Clusters {
			found = found || e.ID == alphaKCCluster
		}
	}
	require.True(t, found, "the cluster of the uploaded file is listed as %s", alphaKCCluster)

	status, st, apiErr := open(t, c, alphaKCCluster, "")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "open", st.State)

	status, items := listItems(t, c, alphaKCCluster, "core", "v1", "pods", "demo")
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, strings.Join(names(items), ","), "logger-")
}

func TestKubeconfig_BearerTokenOfAViewer_ReadsButCannotWrite(t *testing.T) {
	c := signedIn(t, "kc-token")
	c.UploadKubeconfig("alpha-viewer", tokenKubeconfig(t))
	id := "k:alpha-viewer/alpha-viewer"

	status, _, apiErr := open(t, c, id, "")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	status, items := listItems(t, c, id, "core", "v1", "pods", "demo")
	require.Equal(t, http.StatusOK, status)
	assert.NotEmpty(t, items)

	pod := podOf(t, "alpha", "web")
	status, _ = c.Do(http.MethodDelete, "/api/v1/clusters/"+it.ClusterPath(id)+"/resources/core/v1/pods/"+pod+"?namespace=demo", nil, nil)
	assert.Equal(t, http.StatusForbidden, status, "view cannot delete pods")
}

func TestKubeconfig_ExecPluginsAndFilePaths_AreRejectedAndNothingIsStored(t *testing.T) {
	cases := map[string]string{
		"exec.yaml":     "exec credential plugins are not supported",
		"filepath.yaml": "file path references are not supported",
	}
	for file, message := range cases {
		t.Run(file, func(t *testing.T) {
			c := signedIn(t, "kc-reject-"+file)
			data, err := os.ReadFile(filepath.Join("testdata", "kubeconfigs", file))
			require.NoError(t, err)
			var apiErr it.APIError
			status := c.JSON(http.MethodPost, "/api/v1/credentials/kubeconfig", map[string]string{"label": "bad", "content": string(data)}, &apiErr)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Contains(t, apiErr.Error.Message, message)
			for _, s := range clusters(t, c) {
				assert.Empty(t, s.Clusters, "nothing was stored")
			}
		})
	}
}

func TestKubeconfig_Removal_AndAnotherUser_AnswerTheSameClusterUnavailable(t *testing.T) {
	c := signedIn(t, "kc-remove")
	c.UploadKubeconfig("alpha-kc", alphaKubeconfig(t))
	status, _, _ := open(t, c, alphaKCCluster, "")
	require.Equal(t, http.StatusOK, status)

	other := signedIn(t, "kc-remove-other")
	status, _, otherErr := open(t, other, alphaKCCluster, "")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "cluster_unavailable", otherErr.Error.Code, "another user's request for the ID looks like a cluster that does not exist")

	require.Equal(t, http.StatusNoContent, c.JSON(http.MethodDelete, "/api/v1/credentials/kubeconfig/alpha-kc", nil, nil))
	status, _, goneErr := open(t, c, alphaKCCluster, "")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "cluster_unavailable", goneErr.Error.Code)
	assert.Equal(t, otherErr.Error.Message, goneErr.Error.Message, "the same message for both")
}

func TestKubeconfig_KeepsWorkingWhileVaultIsDown_AndRestoresFromTheSealedCopy(t *testing.T) {
	c := signedIn(t, "kc-vault-down")
	sealed := c.UploadKubeconfig("alpha-kc", alphaKubeconfig(t))
	status, _, _ := open(t, c, alphaKCCluster, "")
	require.Equal(t, http.StatusOK, status)

	it.Docker(t, "pause", "kubelook-it-vault")
	t.Cleanup(func() { it.TryDocker("unpause", "kubelook-it-vault") })
	status, items := listItems(t, c, alphaKCCluster, "core", "v1", "pods", "demo")
	assert.Equal(t, http.StatusOK, status, "a kubeconfig cluster does not need Vault")
	assert.NotEmpty(t, items)
	it.Docker(t, "unpause", "kubelook-it-vault")

	// a new session of the same user restores the file from the sealed copy
	again := signedIn(t, "kc-vault-down")
	var res struct {
		Results []struct{ Status string } `json:"results"`
	}
	require.Equal(t, http.StatusOK, again.JSON(http.MethodPost, "/api/v1/credentials/restore", map[string]any{"items": []map[string]string{{"sealed": sealed}}}, &res))
	require.Len(t, res.Results, 1)
	assert.Equal(t, "ok", res.Results[0].Status)
	it.Eventually(t, 15*time.Second, func() bool {
		s, _, _ := open(t, again, alphaKCCluster, "")
		return s == http.StatusOK
	}, "the restored kubeconfig's cluster opens")
}

func TestKubeconfig_ServerOnALoopbackAddress_UploadsButOpeningIsRefused(t *testing.T) {
	c := signedIn(t, "kc-loopback")
	c.UploadKubeconfig("loop", []byte(`apiVersion: v1
kind: Config
clusters:
  - name: loop
    cluster:
      server: https://127.0.0.1:6443
      insecure-skip-tls-verify: true
users:
  - name: loop
    user:
      token: not-a-real-token
contexts:
  - name: loop
    context: {cluster: loop, user: loop}
current-context: loop
`))

	status, _, apiErr := open(t, c, "k:loop/loop", "")

	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, "unprocessable", apiErr.Error.Code)
	assert.Equal(t, "This address isn't allowed: Kubelook doesn't connect to local or link-local addresses from a kubeconfig.", apiErr.Error.Message)

	// the --internal kubeconfig of the kind clusters is on a private address and still works
	c.UploadKubeconfig("alpha-kc", alphaKubeconfig(t))
	status, st, apiErr := open(t, c, alphaKCCluster, "")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "open", st.State)
}
