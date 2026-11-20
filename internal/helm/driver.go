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

// Driver reads (and from Step 5.5 changes) the releases of one cluster under the user's credentials.
type Driver interface {
	// List returns the latest revision of each release, sorted by namespace and name. An empty namespace means all.
	List(ctx context.Context, namespace string) ([]Release, error)
	// Get returns a release; revision 0 is the latest. A missing release is apperr.ErrNotFound.
	Get(ctx context.Context, namespace, name string, revision int) (*Detail, error)
	// History returns the revisions of a release, newest first.
	History(ctx context.Context, namespace, name string) ([]Revision, error)
}
