//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// signedIn returns a client signed in as a fresh Google account, so tests never share a session.
func signedIn(t *testing.T, sub string) *it.Client {
	t.Helper()
	c := it.NewClient(t)
	final := c.SignInGoogle(it.Account{Sub: sub, Email: sub + "@corp.example", HD: "corp.example"})
	require.Empty(t, final.Query().Get("signin_error"), "signing in as %s: %s", sub, final)
	return c
}

type clusterEntry struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Roles        []string `json:"roles"`
	SelectedRole string   `json:"selectedRole"`
	State        string   `json:"state"`
}

type clusterSource struct {
	Type     string         `json:"type"`
	Label    string         `json:"label"`
	Status   string         `json:"status"`
	Clusters []clusterEntry `json:"clusters"`
}

// clusters returns GET /api/v1/clusters.
func clusters(t *testing.T, c *it.Client) []clusterSource {
	t.Helper()
	var out struct {
		Sources []clusterSource `json:"sources"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/clusters", nil, &out))
	return out.Sources
}

// vaultClusters returns the clusters of the Vault source, sorted by ID.
func vaultClusters(t *testing.T, c *it.Client) []clusterEntry {
	t.Helper()
	var all []clusterEntry
	for _, s := range clusters(t, c) {
		if s.Type == "vault" {
			all = append(all, s.Clusters...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all
}

type openState struct {
	State               string     `json:"state"`
	Server              string     `json:"server"`
	Role                string     `json:"role"`
	CredentialExpiresAt *time.Time `json:"credentialExpiresAt"`
}

// open opens a cluster (an empty role picks the default) and returns the status and body.
func open(t *testing.T, c *it.Client, id, role string) (int, openState, it.APIError) {
	t.Helper()
	var st openState
	var apiErr it.APIError
	var in any
	if role != "" {
		in = map[string]string{"role": role}
	}
	status, data := c.Do(http.MethodPost, "/api/v1/clusters/"+it.ClusterPath(id)+"/open", mustJSON(t, in), nil)
	unmarshal(data, &st)
	unmarshal(data, &apiErr)
	return status, st, apiErr
}

// listItems lists a resource kind and returns the objects.
func listItems(t *testing.T, c *it.Client, id, group, version, resource, namespace string) (int, []map[string]any) {
	t.Helper()
	path := "/api/v1/clusters/" + it.ClusterPath(id) + "/resources/" + group + "/" + version + "/" + resource
	if namespace != "" {
		path += "?namespace=" + namespace
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	status := c.JSON(http.MethodGet, path, nil, &out)
	return status, out.Items
}

func names(items []map[string]any) []string {
	var out []string
	for _, o := range items {
		if md, ok := o["metadata"].(map[string]any); ok {
			if n, ok := md["name"].(string); ok {
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// devopsClient returns a signed-in client whose Vault token (bob's) opens both clusters with devops-role.
func devopsClient(t *testing.T, sub string) *it.Client {
	t.Helper()
	c := signedIn(t, sub)
	it.DumpLogOnFailure(t, "kubelook-it-app")
	addToken(t, c, "BOB_TOKEN")
	for _, id := range []string{"v:alpha", "v:beta"} {
		status, _, apiErr := open(t, c, id, "devops-role")
		require.Equal(t, http.StatusOK, status, "opening %s: %+v", id, apiErr)
	}
	return c
}

// frameObject reads the "object" of an event frame or the items of a snapshot frame.
type eventFrame struct {
	Type   string         `json:"type"`
	Object map[string]any `json:"object"`
}

func objName(o map[string]any) string {
	md, _ := o["metadata"].(map[string]any)
	n, _ := md["name"].(string)
	return n
}

// waitEvent returns the next event frame of the given type for the named object.
func waitEvent(t *testing.T, ch *it.Channel, typ, name string, within time.Duration) eventFrame {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		kind, raw := ch.NextOf(time.Until(deadline), "event")
		if kind == "" {
			break
		}
		var ev eventFrame
		require.NoError(t, json.Unmarshal(raw, &ev))
		if ev.Type == typ && objName(ev.Object) == name {
			return ev
		}
	}
	t.Fatalf("no %s event for %s within %s", typ, name, within)
	return eventFrame{}
}

func watchConfigMaps(cluster, ns string) map[string]any {
	return map[string]any{"cluster": cluster, "group": "core", "version": "v1", "resource": "configmaps", "namespace": ns}
}
