package kube

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/redact"
	"github.com/bdnix/kubelook/internal/session"
)

// List limits.
const (
	DefaultLimit = 500
	MaxLimit     = 5000
)

// ListOptions narrow a list.
type ListOptions struct {
	LabelSelector string
	FieldSelector string
	Limit         int64 // 0 → DefaultLimit; above MaxLimit → MaxLimit
	Continue      string
}

// Resources reads any served kind. Every object it returns has passed through redact.Apply.
type Resources struct{ clients Clients }

// NewResources returns the generic reader.
func NewResources(c Clients) *Resources { return &Resources{clients: c} }

func (o ListOptions) normalized() metav1.ListOptions {
	limit := o.Limit
	switch {
	case limit <= 0:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}
	return metav1.ListOptions{LabelSelector: o.LabelSelector, FieldSelector: o.FieldSelector, Limit: limit, Continue: o.Continue}
}

// serves checks that the cluster serves the exact group, version and resource.
func (r *Resources) serves(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource) error {
	disc, err := r.clients.Discovery(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	if _, ok := disc.Find(gvr); !ok {
		return fmt.Errorf("the cluster does not serve %s: %w", gvr.String(), apperr.ErrNotFound)
	}
	return nil
}

// List lists a resource, in one namespace or (ns == "") across all of them.
func (r *Resources) List(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns string, o ListOptions) (*unstructured.UnstructuredList, error) {
	if err := r.serves(ctx, sess, clusterID, gvr); err != nil {
		return nil, err
	}
	dyn, err := r.clients.Dynamic(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	res := dyn.Resource(gvr)
	var list *unstructured.UnstructuredList
	if ns != "" {
		list, err = res.Namespace(ns).List(ctx, o.normalized())
	} else {
		list, err = res.List(ctx, o.normalized())
	}
	if err != nil {
		return nil, MapError(err)
	}
	list.Items = redact.List(list.Items)
	return list, nil
}

// Get reads one object. Objects that must never be shown answer as if they did not exist.
func (r *Resources) Get(ctx context.Context, sess session.Session, clusterID string, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	if err := r.serves(ctx, sess, clusterID, gvr); err != nil {
		return nil, err
	}
	dyn, err := r.clients.Dynamic(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	res := dyn.Resource(gvr)
	var obj *unstructured.Unstructured
	if ns != "" {
		obj, err = res.Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = res.Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, MapError(err)
	}
	if !redact.Apply(obj) {
		return nil, fmt.Errorf("%s %q not found: %w", gvr.Resource, name, apperr.ErrNotFound)
	}
	return obj, nil
}

// mapped is an API error that keeps the API server's message as the text and matches a sentinel.
type mapped struct {
	msg    string
	target error
}

func (m *mapped) Error() string        { return m.msg }
func (m *mapped) Is(target error) bool { return target == m.target }
func (m *mapped) Unwrap() error        { return nil }

// MapError turns a Kubernetes API error into an apperr sentinel that keeps the server's message.
// Errors it does not know are returned unchanged.
func MapError(err error) error {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return err
	}
	msg := status.Status().Message
	if msg == "" {
		msg = err.Error()
	}
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return &mapped{msg, apperr.ErrForbidden}
	case apierrors.IsNotFound(err):
		return &mapped{msg, apperr.ErrNotFound}
	case apierrors.IsConflict(err), apierrors.IsAlreadyExists(err):
		return &mapped{msg, apperr.ErrConflict}
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		return &mapped{msg, apperr.ErrInvalid}
	case apierrors.IsTooManyRequests(err), apierrors.IsServiceUnavailable(err), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return &mapped{msg, apperr.ErrBusy}
	case apierrors.IsRequestEntityTooLargeError(err):
		return &mapped{msg, apperr.ErrTooLarge}
	}
	return err
}

// Mapped returns an error whose text is msg and that matches target with errors.Is.
func Mapped(msg string, target error) error { return &mapped{msg, target} }
