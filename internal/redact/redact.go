// Package redact removes what a browser must never receive from Kubernetes objects: Secret values,
// last-applied configuration of Secrets, managed fields and Helm's release records.
package redact

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/bdnix/kubelook/internal/apperr"
)

// Mask replaces every Secret data value. It contains "*", which is not in the base64 alphabet,
// so it can never equal a real value.
const Mask = "********"

// ErrConflict is returned by Unmask when a masked key no longer exists in the live Secret.
var ErrConflict = fmt.Errorf("a Secret key that you left masked was removed in the cluster meanwhile: %w", apperr.ErrConflict)

//nolint:gosec // G101 matches the word "Secret" in a constant name; these are labels, not credentials
const (
	helmSecretType   = "helm.sh/release.v1"
	helmReleasePrfx  = "sh.helm.release.v1."
	lastAppliedKey   = "kubectl.kubernetes.io/last-applied-configuration"
	unparseableDoc   = "# unparseable document removed"
	secretAPIVersion = "v1"
)

func isSecret(obj *unstructured.Unstructured) bool {
	return obj.GetAPIVersion() == secretAPIVersion && obj.GetKind() == "Secret"
}

func isHelmRelease(obj *unstructured.Unstructured) bool {
	switch {
	case isSecret(obj):
		t, _, _ := unstructured.NestedString(obj.Object, "type")
		return t == helmSecretType || strings.HasPrefix(obj.GetName(), helmReleasePrfx)
	case obj.GetAPIVersion() == secretAPIVersion && obj.GetKind() == "ConfigMap":
		return strings.HasPrefix(obj.GetName(), helmReleasePrfx) && obj.GetLabels()["owner"] == "helm"
	}
	return false
}

// Apply redacts obj in place. It returns false when the object must not be shown at all
// (Helm's release records, which hold every value of a release).
func Apply(obj *unstructured.Unstructured) (keep bool) {
	if isHelmRelease(obj) {
		return false
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	if !isSecret(obj) {
		return true
	}
	if data, ok, _ := unstructured.NestedMap(obj.Object, "data"); ok {
		for k := range data {
			data[k] = Mask
		}
		_ = unstructured.SetNestedMap(obj.Object, data, "data")
	} else {
		delete(obj.Object, "data") // absent, or not a map: nothing a browser may see
	}
	delete(obj.Object, "stringData")
	if ann := obj.GetAnnotations(); ann != nil {
		if _, ok := ann[lastAppliedKey]; ok {
			delete(ann, lastAppliedKey)
			obj.SetAnnotations(ann)
			if len(ann) == 0 {
				unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")
			}
		}
	}
	return true
}

// List redacts every item and drops the ones that must not be shown. The input slice is reused.
func List(items []unstructured.Unstructured) []unstructured.Unstructured {
	out := items[:0]
	for i := range items {
		if Apply(&items[i]) {
			out = append(out, items[i])
		}
	}
	return out
}

var docSeparator = regexp.MustCompile(`(?m)^---[ \t]*$`)

// Manifest redacts a multi-document YAML manifest (a Helm release manifest): Secret documents
// have their values masked, every other document is returned byte for byte, and a document that
// cannot be parsed is replaced by a comment.
func Manifest(text string) string {
	seps := docSeparator.FindAllString(text, -1)
	docs := docSeparator.Split(text, -1)
	var b strings.Builder
	for i, doc := range docs {
		b.WriteString(redactDoc(doc))
		if i < len(seps) {
			b.WriteString(seps[i])
		}
	}
	return b.String()
}

func redactDoc(doc string) string {
	if onlyComments(doc) {
		return doc
	}
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
		return "\n" + unparseableDoc + "\n"
	}
	u := &unstructured.Unstructured{Object: obj}
	if obj == nil || !isSecret(u) {
		return doc
	}
	if !Apply(u) {
		return "\n" + unparseableDoc + "\n"
	}
	out, err := yaml.Marshal(u.Object)
	if err != nil {
		return "\n" + unparseableDoc + "\n"
	}
	return "\n" + string(out)
}

func onlyComments(doc string) bool {
	for _, line := range strings.Split(doc, "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
			return false
		}
	}
	return true
}

// Unmask prepares an edited Secret for saving: every data value that is still Mask takes the live
// object's value for that key. The edited object is not changed; objects that are not Secrets are
// returned unchanged.
func Unmask(edited, live *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if !isSecret(edited) {
		return edited, nil
	}
	out := edited.DeepCopy()
	data, ok, _ := unstructured.NestedMap(out.Object, "data")
	if !ok {
		return out, nil
	}
	liveData, _, _ := unstructured.NestedMap(live.Object, "data")
	for k, v := range data {
		if s, isString := v.(string); isString && s == Mask {
			lv, exists := liveData[k]
			if !exists {
				return nil, fmt.Errorf("key %q: %w", k, ErrConflict)
			}
			data[k] = lv
		}
	}
	if err := unstructured.SetNestedMap(out.Object, data, "data"); err != nil {
		return nil, fmt.Errorf("restoring Secret values: %w", err)
	}
	return out, nil
}
