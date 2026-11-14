package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/vault"
)

// Source and cluster states, as the cluster list reports them.
const (
	SourceVault      = "vault"
	SourceKubeconfig = "kubeconfig"

	SourceOK          = "ok"
	SourceUnreachable = "unreachable"
	SourceExpired     = "expired"

	StateClosed       = "closed"
	StateDisconnected = "disconnected"
)

// Entry is one cluster in the list.
type Entry struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Roles        []string `json:"roles,omitempty"`
	SelectedRole string   `json:"selectedRole,omitempty"`
	State        string   `json:"state"`
	Server       string   `json:"server,omitempty"`
}

// Source is a group of clusters that come from one credential.
type Source struct {
	Type     string  `json:"type"`
	Label    string  `json:"label"`
	Status   string  `json:"status"`
	Clusters []Entry `json:"clusters"`
}

// Ref identifies a cluster of the session precisely enough to connect to it (Step 3.1).
type Ref struct {
	ID      string
	Source  string
	Mount   string   // vault
	Roles   []string // vault
	Label   string   // kubeconfig
	Context string   // kubeconfig
}

// RegistryDeps are the dependencies of Registry.
type RegistryDeps struct {
	Credentials *Credentials
	Vault       vault.API
	Sessions    *store.Sessions
	Cache       *DiscoveryCache
	Config      *config.Config
	Log         *slog.Logger
}

// Registry lists and resolves the clusters of a session.
type Registry struct{ d RegistryDeps }

// NewRegistry returns the cluster registry.
func NewRegistry(d RegistryDeps) *Registry { return &Registry{d: d} }

// List returns the session's sources: the Vault source first, then kubeconfig sources by label.
// It never fetches cluster configs or credentials; those are fetched when a cluster is opened.
func (r *Registry) List(ctx context.Context, sess session.Session) ([]Source, error) {
	sources := []Source{}
	vs, err := r.vaultSource(ctx, sess)
	if err != nil {
		return nil, err
	}
	if vs != nil {
		sources = append(sources, *vs)
	}
	files, err := r.d.Credentials.loadKubeconfigs(ctx, sess)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		src := Source{Type: SourceKubeconfig, Label: f.Label, Status: SourceOK, Clusters: make([]Entry, len(f.Contexts))}
		for i, cx := range f.Contexts {
			src.Clusters[i] = Entry{ID: cx.ID, Name: cx.Context, State: StateClosed, Server: cx.Server}
		}
		sources = append(sources, src)
	}
	return sources, nil
}

// vaultSource returns the Vault source, or nil when the session has no Vault token.
func (r *Registry) vaultSource(ctx context.Context, sess session.Session) (*Source, error) {
	clusters, status, found, err := r.vaultClusters(ctx, sess)
	if err != nil || !found {
		return nil, err
	}
	state := StateClosed
	if status == SourceExpired {
		state = StateDisconnected
	}
	src := &Source{Type: SourceVault, Label: "Vault", Status: status, Clusters: make([]Entry, len(clusters))}
	for i, cl := range clusters {
		src.Clusters[i] = Entry{ID: vaultClusterPrefix + cl.Name, Name: cl.Name, Roles: cl.Roles, State: state}
	}
	return src, nil
}

// vaultClusters returns the clusters the session's Vault token reaches and the status of that source.
// Discovery is cached for 60 s; when Vault cannot be asked, the last known clusters are returned.
func (r *Registry) vaultClusters(ctx context.Context, sess session.Session) (clusters []vault.Cluster, status string, found bool, err error) {
	vc, expires, err := r.d.Credentials.loadVaultCred(ctx, sess)
	if errors.Is(err, errNoVaultToken) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	last, _ := r.d.Cache.Last(sess.Hash)
	if !expires.IsZero() && !r.d.Credentials.d.Clock.Now().Before(expires) {
		return last, SourceExpired, true, nil
	}
	if fresh, ok := r.d.Cache.Fresh(sess.Hash); ok {
		return fresh, SourceOK, true, nil
	}
	cfg := r.d.Config.Vault
	discovered, err := vault.Discover(ctx, r.d.Vault, vc.Token, cfg.KubernetesMountPrefix, cfg.CandidateRoles)
	switch {
	case errors.Is(err, vault.ErrPermissionDenied):
		return last, SourceExpired, true, nil
	case errors.Is(err, vault.ErrUnreachable):
		return last, SourceUnreachable, true, nil
	case err != nil:
		return nil, "", false, fmt.Errorf("discovering clusters: %w", err)
	}
	r.d.Cache.Put(sess.Hash, discovered)
	return discovered, SourceOK, true, nil
}

// Resolve finds a cluster of this session by ID. Anything that is not in the session, including
// another user's kubeconfig cluster, answers apperr.ErrClusterUnavailable.
func (r *Registry) Resolve(ctx context.Context, sess session.Session, id string) (Ref, error) {
	switch {
	case strings.HasPrefix(id, vaultClusterPrefix):
		return r.resolveVault(ctx, sess, id)
	case strings.HasPrefix(id, kubeconfigClusterPrefix):
		return r.resolveKubeconfig(ctx, sess, id)
	}
	return Ref{}, apperr.ErrClusterUnavailable
}

func (r *Registry) resolveVault(ctx context.Context, sess session.Session, id string) (Ref, error) {
	clusters, _, found, err := r.vaultClusters(ctx, sess)
	if err != nil {
		return Ref{}, err
	}
	if found {
		for _, cl := range clusters {
			if vaultClusterPrefix+cl.Name == id {
				return Ref{ID: id, Source: SourceVault, Mount: cl.Mount, Roles: cl.Roles}, nil
			}
		}
	}
	return Ref{}, apperr.ErrClusterUnavailable
}

func (r *Registry) resolveKubeconfig(ctx context.Context, sess session.Session, id string) (Ref, error) {
	files, err := r.d.Credentials.loadKubeconfigs(ctx, sess)
	if err != nil {
		return Ref{}, err
	}
	for _, f := range files {
		for _, cx := range f.Contexts {
			if cx.ID == id {
				return Ref{ID: id, Source: SourceKubeconfig, Label: f.Label, Context: cx.Context}, nil
			}
		}
	}
	return Ref{}, apperr.ErrClusterUnavailable
}
