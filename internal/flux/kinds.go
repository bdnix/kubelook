// Package flux knows the Flux kinds the console covers, how each is suspended and reconciled, and how to read
// the objects a Kustomization or a HelmRelease manages.
package flux

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// RequestedAtAnnotation is what every Flux controller watches to reconcile an object now.
const RequestedAtAnnotation = "reconcile.fluxcd.io/requestedAt"

const (
	valueDisabled = "disabled"
	valueEnabled  = "enabled"
)

// ErrCannotSuspend is returned for a kind that has no way to be suspended (FluxReport).
var ErrCannotSuspend = errors.New("this kind cannot be suspended")

type suspendVia int

const (
	viaSpec suspendVia = iota
	viaAnnotation
	viaNever
)

// Kind is one Flux kind and how it is suspended: by `spec.suspend`, by an annotation or not at all.
type Kind struct {
	Group, Kind string
	via         suspendVia
	annotation  string
}

const (
	opAnnotation  = "fluxcd.controlplane.io/reconcile"
	genAnnotation = "source.extensions.fluxcd.io/reconcile"
)

// Kinds are the kinds of the spec's Flux table. The annotation values were checked in the sources of the
// flux-operator and of source-watcher: both compare `disabled` without regard to case and treat `enabled` as on.
var Kinds = []Kind{
	{Group: "fluxcd.controlplane.io", Kind: "FluxInstance", via: viaAnnotation, annotation: opAnnotation},
	{Group: "fluxcd.controlplane.io", Kind: "FluxReport", via: viaNever},
	{Group: "fluxcd.controlplane.io", Kind: "ResourceSet", via: viaAnnotation, annotation: opAnnotation},
	{Group: "fluxcd.controlplane.io", Kind: "ResourceSetInputProvider", via: viaAnnotation, annotation: opAnnotation},
	{Group: "source.toolkit.fluxcd.io", Kind: "GitRepository"},
	{Group: "source.toolkit.fluxcd.io", Kind: "OCIRepository"},
	{Group: "source.toolkit.fluxcd.io", Kind: "HelmRepository"},
	{Group: "source.toolkit.fluxcd.io", Kind: "HelmChart"},
	{Group: "source.toolkit.fluxcd.io", Kind: "Bucket"},
	{Group: "source.toolkit.fluxcd.io", Kind: "ExternalArtifact"},
	{Group: "source.extensions.fluxcd.io", Kind: "ArtifactGenerator", via: viaAnnotation, annotation: genAnnotation},
	{Group: "kustomize.toolkit.fluxcd.io", Kind: "Kustomization"},
	{Group: "helm.toolkit.fluxcd.io", Kind: "HelmRelease"},
	{Group: "image.toolkit.fluxcd.io", Kind: "ImageRepository"},
	{Group: "image.toolkit.fluxcd.io", Kind: "ImagePolicy"},
	{Group: "image.toolkit.fluxcd.io", Kind: "ImageUpdateAutomation"},
	{Group: "notification.toolkit.fluxcd.io", Kind: "Provider"},
	{Group: "notification.toolkit.fluxcd.io", Kind: "Alert"},
	{Group: "notification.toolkit.fluxcd.io", Kind: "Receiver"},
}

// Find returns the Flux kind with the group and kind.
func Find(group, kind string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Group == group && k.Kind == kind {
			return k, true
		}
	}
	return Kind{}, false
}

// SuspendPatch is the merge patch that suspends (or resumes) an object of the kind.
func (k Kind) SuspendPatch(suspend bool) ([]byte, error) {
	switch k.via {
	case viaSpec:
		return json.Marshal(map[string]any{"spec": map[string]any{"suspend": suspend}})
	case viaAnnotation:
		value := valueEnabled
		if suspend {
			value = valueDisabled
		}
		return json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{k.annotation: value}}})
	}
	return nil, ErrCannotSuspend
}

// ReconcilePatch is the merge patch that asks for a reconciliation now.
func ReconcilePatch(now time.Time) []byte {
	b, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{RequestedAtAnnotation: now.UTC().Format(time.RFC3339Nano)}}})
	return b
}

// IsSuspended reports whether the object is suspended, from `spec.suspend` or from the kind's annotation.
func (k Kind) IsSuspended(obj map[string]any) bool {
	switch k.via {
	case viaSpec:
		spec, _ := obj["spec"].(map[string]any)
		suspended, _ := spec["suspend"].(bool)
		return suspended
	case viaAnnotation:
		meta, _ := obj["metadata"].(map[string]any)
		annotations, _ := meta["annotations"].(map[string]any)
		value, _ := annotations[k.annotation].(string)
		return strings.EqualFold(value, valueDisabled)
	}
	return false
}
