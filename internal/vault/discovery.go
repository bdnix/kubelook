package vault

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ErrNoCandidateRoles is returned when discovery is asked to run without any candidate role.
var ErrNoCandidateRoles = errors.New("vault: no candidate roles are configured")

// kubernetesMountType is the type of Vault's Kubernetes secrets engine.
const kubernetesMountType = "kubernetes"

// Cluster is a Kubernetes mount a token can open.
type Cluster struct {
	Name  string   `json:"name"`  // mount path below the prefix, e.g. "prod" or "eu/prod"
	Mount string   `json:"mount"` // full mount path without trailing slash, e.g. "kubernetes/prod"
	Roles []string `json:"roles"` // allowed roles in preference order; Roles[0] is the default
}

// Discover computes exactly the clusters the token can open: Kubernetes mounts under prefix
// where the token may read the config and use `update` on the creds of at least one candidate
// role. It makes one Mounts call and one Capabilities call (which the client splits at 100 paths).
func Discover(ctx context.Context, api API, token, prefix string, candidateRoles []string) ([]Cluster, error) {
	if len(candidateRoles) == 0 {
		return nil, ErrNoCandidateRoles
	}
	mounts, err := api.Mounts(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("listing vault mounts: %w", err)
	}
	var found []Cluster
	var paths []string
	for path, typ := range mounts {
		if typ != kubernetesMountType || !strings.HasPrefix(path, prefix) {
			continue
		}
		mount := strings.TrimSuffix(path, "/")
		name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
		if name == "" {
			continue
		}
		found = append(found, Cluster{Name: name, Mount: mount})
		paths = append(paths, mount+"/config")
		for _, r := range candidateRoles {
			paths = append(paths, mount+"/creds/"+r)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	caps, err := api.Capabilities(ctx, token, paths)
	if err != nil {
		return nil, fmt.Errorf("checking vault capabilities: %w", err)
	}
	var clusters []Cluster
	for _, c := range found {
		if !can(caps[c.Mount+"/config"], "read") {
			continue
		}
		for _, r := range candidateRoles {
			if can(caps[c.Mount+"/creds/"+r], "update") {
				c.Roles = append(c.Roles, r)
			}
		}
		if len(c.Roles) > 0 {
			clusters = append(clusters, c)
		}
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Name < clusters[j].Name })
	return clusters, nil
}

// can reports whether the capabilities include want: root allows everything, deny nothing.
func can(caps []string, want string) bool {
	if slices.Contains(caps, "root") {
		return true
	}
	if slices.Contains(caps, "deny") {
		return false
	}
	return slices.Contains(caps, want)
}
