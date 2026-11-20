// Package helm is what Kubelook asks of Helm: list releases, read one, and (from Step 5.5) change them.
// The Driver interface keeps the Helm SDK behind one adapter.
package helm

import (
	"context"
	"time"
)

// Release is a release at one revision.
type Release struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	Chart        string    `json:"chart"`
	ChartVersion string    `json:"chartVersion"`
	AppVersion   string    `json:"appVersion"`
	Updated      time.Time `json:"updated"`
	// ApplyMethod is "csa" or "ssa". Records written by Helm 3 have none and are client-side apply.
	ApplyMethod string `json:"applyMethod"`
}

// Hook is a hook of a release chart.
type Hook struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Events []string `json:"events"`
}

// Detail is a release with its values, manifest, notes and hooks.
type Detail struct {
	Release
	UserValues     string `json:"userValues"`
	ComputedValues string `json:"computedValues"`
	// Manifest has the values of Secrets masked.
	Manifest string `json:"manifest"`
	Notes    string `json:"notes"`
	Hooks    []Hook `json:"hooks"`
}

// Revision is one line of a release's history.
type Revision struct {
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	ChartVersion string    `json:"chartVersion"`
	Description  string    `json:"description"`
	Updated      time.Time `json:"updated"`
}

// ChartSource says where a chart comes from: a file on disk (an archive downloaded from an HTTP repository,
// already inside the job's temporary directory) or an OCI reference the SDK pulls.
type ChartSource struct {
	// Path is a chart archive or directory.
	Path string `json:"-"`
	// OCI is an `oci://` reference; Version is its tag.
	OCI     string `json:"oci,omitempty"`
	Version string `json:"version,omitempty"`
}

// InstallOptions are the inputs of an install.
type InstallOptions struct {
	Namespace string
	Name      string
	Chart     ChartSource
	Values    map[string]any
}

// UpgradeOptions are the inputs of an upgrade, and of a preview of an install or an upgrade.
type UpgradeOptions struct {
	Namespace string
	Name      string
	Chart     ChartSource
	Values    map[string]any
}

// HookResult is the outcome of one test hook.
type HookResult struct {
	Name  string `json:"name"`
	Phase string `json:"phase"`
}

// TestResult is the outcome of `helm test`.
type TestResult struct {
	Hooks []HookResult `json:"hooks"`
}

// Driver reads and changes the releases of one cluster under the user's credentials.
type Driver interface {
	// List returns the latest revision of each release, sorted by namespace and name. An empty namespace means all.
	List(ctx context.Context, namespace string) ([]Release, error)
	// Get returns a release; revision 0 is the latest. A missing release is apperr.ErrNotFound.
	Get(ctx context.Context, namespace, name string, revision int) (*Detail, error)
	// History returns the revisions of a release, newest first.
	History(ctx context.Context, namespace, name string) ([]Revision, error)

	// Install fails with apperr.ErrConflict when the name is taken.
	Install(ctx context.Context, o InstallOptions) (*Release, error)
	// Upgrade fails with apperr.ErrNotFound when the release does not exist.
	Upgrade(ctx context.Context, o UpgradeOptions) (*Release, error)
	Rollback(ctx context.Context, namespace, name string, revision int) error
	Uninstall(ctx context.Context, namespace, name string) error
	Test(ctx context.Context, namespace, name string) (TestResult, error)
	// Render returns the manifest an install (or, when the release exists, an upgrade) would apply, with the values
	// of Secrets masked. Nothing is stored and nothing reaches the cluster.
	Render(ctx context.Context, o UpgradeOptions) (manifest string, err error)
	// Status is the latest revision; apperr.ErrNotFound when the release is absent.
	Status(ctx context.Context, namespace, name string) (*Release, error)
}
