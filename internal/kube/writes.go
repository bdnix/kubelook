package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/redact"
	"github.com/bdnix/kubelook/internal/session"
)

// FieldManager names Kubelook in managedFields.
const FieldManager = "kubelook"

const staleMessage = "The object changed since you loaded it."

// mapWriteError maps the API error of a mutating call. Validation and admission failures become
// ErrUnprocessable with the server's text; a conflict gets the fixed text the UI expects.
func mapWriteError(err error, dryRun bool) error {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return err
	}
	msg := status.Status().Message
	switch {
	case dryRun && strings.Contains(msg, "does not support dry run"):
		return &mapped{msg, apperr.ErrDryRunUnsupported}
	case apierrors.IsConflict(err):
		return &mapped{staleMessage, apperr.ErrConflict}
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		if msg == "" {
			msg = err.Error()
		}
		return &mapped{msg, apperr.ErrUnprocessable}
	}
	return MapError(err)
}

func dryRunList(dryRun bool) []string {
	if dryRun {
		return []string{metav1.DryRunAll}
	}
	return nil
}

func nsClient(dyn dynamic.Interface, gvr schema.GroupVersionResource, ns string) dynamic.ResourceInterface {
	if ns != "" {
		return dyn.Resource(gvr).Namespace(ns)
	}
	return dyn.Resource(gvr)
}

// holdsRecords reports whether the resource can hold Helm's release records, which are never written.
func holdsRecords(gvr schema.GroupVersionResource) bool {
	return gvr.Group == "" && (gvr.Resource == "secrets" || gvr.Resource == "configmaps")
}

func hidden(obj *unstructured.Unstructured) bool { return !redact.Apply(obj.DeepCopy()) }

func notFound(gvr schema.GroupVersionResource, name string) error {
	return fmt.Errorf("%s %q not found: %w", gvr.Resource, name, apperr.ErrNotFound)
}

// guard answers ErrNotFound for objects that must stay invisible (Helm release records).
func guard(ctx context.Context, res dynamic.ResourceInterface, gvr schema.GroupVersionResource, name string) error {
	if !holdsRecords(gvr) {
		return nil
	}
	live, err := res.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return MapError(err)
	}
	if hidden(live) {
		return notFound(gvr, name)
	}
	return nil
}

func (r *Resources) client(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns string) (dynamic.ResourceInterface, error) {
	if err := r.serves(ctx, sess, clusterID, gvr); err != nil {
		return nil, err
	}
	dyn, err := r.clients.Dynamic(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	return nsClient(dyn, gvr, ns), nil
}

func redacted(obj *unstructured.Unstructured, gvr schema.GroupVersionResource) (*unstructured.Unstructured, error) {
	if obj == nil {
		return nil, nil //nolint:nilnil // a dry run of some kinds returns no object
	}
	if !redact.Apply(obj) {
		return nil, notFound(gvr, obj.GetName())
	}
	return obj, nil
}

// Create creates the object; with dryRun the server validates and returns what it would store.
func (r *Resources) Create(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	if hidden(obj) {
		return nil, notFound(gvr, obj.GetName())
	}
	res, err := r.client(ctx, sess, clusterID, gvr, ns)
	if err != nil {
		return nil, err
	}
	out, err := res.Create(ctx, obj, metav1.CreateOptions{FieldManager: FieldManager, DryRun: dryRunList(dryRun)})
	if err != nil {
		return nil, mapWriteError(err, dryRun)
	}
	return redacted(out, gvr)
}

// Update replaces the object. The edited object must carry the resourceVersion it was loaded with.
// Masked Secret values are restored from a fresh read of the live object. The live object is returned
// (redacted) only for a dry run, next to the result.
func (r *Resources) Update(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, dryRun bool) (live, result *unstructured.Unstructured, err error) {
	if obj.GetResourceVersion() == "" {
		return nil, nil, fmt.Errorf("resourceVersion is required: %w", apperr.ErrInvalid)
	}
	res, err := r.client(ctx, sess, clusterID, gvr, ns)
	if err != nil {
		return nil, nil, err
	}
	toSend := obj
	if dryRun || holdsRecords(gvr) {
		raw, gerr := res.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if gerr != nil {
			return nil, nil, MapError(gerr)
		}
		if hidden(raw) {
			return nil, nil, notFound(gvr, obj.GetName())
		}
		if toSend, err = redact.Unmask(obj, raw); err != nil {
			return nil, nil, err
		}
		if dryRun {
			live = raw
			redact.Apply(live)
		}
	}
	out, err := res.Update(ctx, toSend, metav1.UpdateOptions{FieldManager: FieldManager, DryRun: dryRunList(dryRun)})
	if err != nil {
		return nil, nil, mapWriteError(err, dryRun)
	}
	result, err = redacted(out, gvr)
	return live, result, err
}

// Patch applies a merge or strategic-merge patch.
func (r *Resources) Patch(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns, name string, pt types.PatchType, data []byte, dryRun bool) (*unstructured.Unstructured, error) {
	res, err := r.client(ctx, sess, clusterID, gvr, ns)
	if err != nil {
		return nil, err
	}
	if gvr.Group == "" && gvr.Resource == "secrets" && strings.Contains(string(data), `"`+redact.Mask+`"`) {
		return nil, fmt.Errorf("the patch contains masked Secret values: %w", apperr.ErrInvalid)
	}
	if err := guard(ctx, res, gvr, name); err != nil {
		return nil, err
	}
	out, err := res.Patch(ctx, name, pt, data, metav1.PatchOptions{FieldManager: FieldManager, DryRun: dryRunList(dryRun)})
	if err != nil {
		return nil, mapWriteError(err, dryRun)
	}
	return redacted(out, gvr)
}

// Delete deletes the object. An empty policy means Background.
func (r *Resources) Delete(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns, name string, policy metav1.DeletionPropagation) error {
	switch policy {
	case "":
		policy = metav1.DeletePropagationBackground
	case metav1.DeletePropagationBackground, metav1.DeletePropagationForeground, metav1.DeletePropagationOrphan:
	default:
		return fmt.Errorf("propagationPolicy must be Background, Foreground or Orphan: %w", apperr.ErrInvalid)
	}
	res, err := r.client(ctx, sess, clusterID, gvr, ns)
	if err != nil {
		return err
	}
	if err := guard(ctx, res, gvr, name); err != nil {
		return err
	}
	if err := res.Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy}); err != nil {
		return mapWriteError(err, false)
	}
	return nil
}
