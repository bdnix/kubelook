//go:build integration

package integration

import (
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
	status, data := c.Do(http.MethodPost, "/api/v1/clusters/"+id+"/open", mustJSON(t, in), nil)
	unmarshal(data, &st)
	unmarshal(data, &apiErr)
	return status, st, apiErr
}

// listItems lists a resource kind and returns the objects.
func listItems(t *testing.T, c *it.Client, id, group, version, resource, namespace string) (int, []map[string]any) {
	t.Helper()
	path := "/api/v1/clusters/" + id + "/resources/" + group + "/" + version + "/" + resource
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
