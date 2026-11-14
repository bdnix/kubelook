package kube

import (
	"context"
	"encoding/base64"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/session"
)

var secretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// SecretData reads the decoded values of a Secret with the user's own credentials. It is the only place that
// returns real Secret values; every caller must have audited the request first. Helm's release records answer
// as if they did not exist.
func (r *Resources) SecretData(ctx context.Context, sess session.Session, clusterID, ns, name string) (map[string][]byte, error) {
	dyn, err := r.clients.Dynamic(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	obj, err := dyn.Resource(secretsGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, MapError(err)
	}
	if hidden(obj) {
		return nil, notFound(secretsGVR, name)
	}
	raw, _, _ := unstructured.NestedMap(obj.Object, "data")
	out := make(map[string][]byte, len(raw))
	for k, v := range raw {
		s, _ := v.(string)
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("the value of %q is not valid base64: %w", k, apperr.ErrInvalid)
		}
		out[k] = b
	}
	return out, nil
}
