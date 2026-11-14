package actions

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/kube"
)

// maxTaintRetries is how often a taint change is repeated after another writer changed the node meanwhile.
const maxTaintRetries = 3

var nodeKinds = []schema.GroupKind{gk("", "Node")}

func registerNodes(h map[string]handler) {
	h["cordon"] = handler{kinds: nodeKinds, verb: audit.VerbCordon, run: mergePatch(`{"spec":{"unschedulable":true}}`)}
	h["uncordon"] = handler{kinds: nodeKinds, verb: audit.VerbUncordon, run: mergePatch(`{"spec":{"unschedulable":false}}`)}
	h["taintAdd"] = handler{kinds: nodeKinds, verb: audit.VerbTaint, validate: validateTaintAdd, run: runTaintAdd}
	h["taintRemove"] = handler{kinds: nodeKinds, verb: audit.VerbTaint, validate: validateTaintRemove, run: runTaintRemove}
}

type taintParams struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

var taintEffects = map[string]bool{"NoSchedule": true, "PreferNoSchedule": true, "NoExecute": true}

func (p taintParams) validate(withValue bool) error {
	if p.Key == "" || len(validation.IsQualifiedName(p.Key)) > 0 {
		return fmt.Errorf("the taint key %q is not valid: %w", p.Key, apperr.ErrInvalid)
	}
	if !taintEffects[p.Effect] {
		return fmt.Errorf("the effect must be NoSchedule, PreferNoSchedule or NoExecute: %w", apperr.ErrInvalid)
	}
	if withValue && len(validation.IsValidLabelValue(p.Value)) > 0 {
		return fmt.Errorf("the taint value %q is not valid: %w", p.Value, apperr.ErrInvalid)
	}
	return nil
}

func (p taintParams) matches(taint any) bool {
	m, ok := taint.(map[string]any)
	return ok && m["key"] == p.Key && m["effect"] == p.Effect
}

func nodeTaints(n *unstructured.Unstructured) []any {
	list, _, _ := unstructured.NestedSlice(n.Object, "spec", "taints")
	return list
}

func hasTaint(list []any, p taintParams) bool {
	for _, t := range list {
		if p.matches(t) {
			return true
		}
	}
	return false
}

var errDuplicateTaint = fmt.Errorf("the node already has this taint: %w", apperr.ErrConflict)

func validateTaintAdd(c *call) error {
	var p taintParams
	if err := c.decode(&p); err != nil {
		return err
	}
	if err := p.validate(true); err != nil {
		return err
	}
	n, err := c.get()
	if err != nil {
		return err
	}
	if hasTaint(nodeTaints(n), p) {
		return errDuplicateTaint
	}
	return nil
}

func validateTaintRemove(c *call) error {
	var p taintParams
	if err := c.decode(&p); err != nil {
		return err
	}
	if err := p.validate(false); err != nil {
		return err
	}
	n, err := c.get()
	if err != nil {
		return err
	}
	if !hasTaint(nodeTaints(n), p) {
		return fmt.Errorf("the node has no such taint: %w", apperr.ErrNotFound)
	}
	return nil
}

func runTaintAdd(c *call) error {
	var p taintParams
	_ = c.decode(&p)
	taint := map[string]any{"key": p.Key, "effect": p.Effect}
	if p.Value != "" {
		taint["value"] = p.Value
	}
	return c.updateTaints(func(list []any) ([]any, error) {
		if hasTaint(list, p) {
			return nil, errDuplicateTaint
		}
		return append(list, taint), nil
	})
}

func runTaintRemove(c *call) error {
	var p taintParams
	_ = c.decode(&p)
	return c.updateTaints(func(list []any) ([]any, error) {
		out := make([]any, 0, len(list))
		for _, t := range list {
			if !p.matches(t) {
				out = append(out, t)
			}
		}
		if len(out) == len(list) {
			return nil, fmt.Errorf("the node has no such taint: %w", apperr.ErrNotFound)
		}
		return out, nil
	})
}

// updateTaints reads the node, changes its taints and writes it back with the resourceVersion it read, so a
// concurrent change is a conflict. A conflict is retried with a fresh read, up to maxTaintRetries times.
func (c *call) updateTaints(change func([]any) ([]any, error)) error {
	for range maxTaintRetries + 1 {
		n, err := c.get()
		if err != nil {
			return err
		}
		list, err := change(nodeTaints(n))
		if err != nil {
			return err
		}
		if len(list) == 0 {
			unstructured.RemoveNestedField(n.Object, "spec", "taints")
		} else if err := unstructured.SetNestedSlice(n.Object, list, "spec", "taints"); err != nil {
			return fmt.Errorf("setting the taints: %w", err)
		}
		_, err = c.dyn.Resource(c.gvr).Update(c.ctx, n, metav1.UpdateOptions{FieldManager: kube.FieldManager})
		if err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			return kube.MapError(err)
		}
	}
	return kube.Mapped("The node kept changing while the taint was being saved. Try again.", apperr.ErrConflict)
}
