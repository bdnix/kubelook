package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/kube"
)

// MaxBulkTargets is the most objects one bulk job works on.
const MaxBulkTargets = 500

// maxListedItems bounds the names a recovery message lists.
const maxListedItems = 20

var bulkActions = map[string]bool{"delete": true, "restart": true, "label": true, "cordon": true, "uncordon": true, "drain": true}

// BulkRequest is what a bulk job is made from: one action on many objects.
type BulkRequest struct {
	Action  string          `json:"action"`
	Targets []Target        `json:"targets"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type labelParams struct {
	Add    map[string]string `json:"add"`
	Remove []string          `json:"remove"`
}

func decodeLabels(raw json.RawMessage) (labelParams, error) {
	var p labelParams
	if len(raw) == 0 {
		return p, fmt.Errorf("label needs the labels to add or remove: %w", apperr.ErrInvalid)
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("the label parameters are not valid: %w", apperr.ErrInvalid)
	}
	if len(p.Add) == 0 && len(p.Remove) == 0 {
		return p, fmt.Errorf("label needs the labels to add or remove: %w", apperr.ErrInvalid)
	}
	for k, v := range p.Add {
		if len(validation.IsQualifiedName(k)) > 0 || len(validation.IsValidLabelValue(v)) > 0 {
			return p, fmt.Errorf("%q=%q is not a valid label: %w", k, v, apperr.ErrInvalid)
		}
	}
	for _, k := range p.Remove {
		if len(validation.IsQualifiedName(k)) > 0 {
			return p, fmt.Errorf("%q is not a valid label key: %w", k, apperr.ErrInvalid)
		}
	}
	return p, nil
}

func isNode(t Target) bool { return (t.Group == "" || t.Group == "core") && t.Resource == "nodes" }

// ValidateBulk checks a request before a job is made from it.
func ValidateBulk(r BulkRequest) error {
	if !bulkActions[r.Action] {
		return fmt.Errorf("unknown bulk action %q: %w", r.Action, apperr.ErrInvalid)
	}
	if len(r.Targets) == 0 {
		return fmt.Errorf("select at least one object: %w", apperr.ErrInvalid)
	}
	if len(r.Targets) > MaxBulkTargets {
		return fmt.Errorf("at most %d objects can be changed at once: %w", MaxBulkTargets, apperr.ErrInvalid)
	}
	for _, t := range r.Targets {
		if t.Version == "" || t.Resource == "" || t.Name == "" {
			return fmt.Errorf("every target needs a version, a resource and a name: %w", apperr.ErrInvalid)
		}
		if (r.Action == "cordon" || r.Action == "uncordon" || r.Action == "drain") && !isNode(t) {
			return fmt.Errorf("%s applies to nodes only: %w", r.Action, apperr.ErrInvalid)
		}
	}
	if r.Action == "label" {
		if _, err := decodeLabels(r.Params); err != nil {
			return err
		}
	}
	return nil
}

func itemName(t Target) string {
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "/" + t.Name
}

type bulkHandler struct {
	d   Deps
	svc *Service
	res *kube.Resources
	dr  *Drainer
}

// NewBulkHandler returns the job handler of type "bulk": params are a BulkRequest.
func NewBulkHandler(d Deps) jobs.Handler {
	return &bulkHandler{d: d, svc: New(d), res: kube.NewResources(d.Clients), dr: NewDrainer(d)}
}

func (h *bulkHandler) Type() string { return "bulk" }

func decodeBulk(j *jobs.Job) (BulkRequest, error) {
	var r BulkRequest
	if err := json.Unmarshal(j.Params, &r); err != nil {
		return r, fmt.Errorf("the bulk job's parameters are not valid: %w", apperr.ErrInvalid)
	}
	return r, ValidateBulk(r)
}

// Run works through the targets one at a time in the given order. An item that fails is recorded and the job
// goes on; each item is audited on its own.
func (h *bulkHandler) Run(ctx context.Context, j *jobs.Job, report func(jobs.Progress)) error {
	req, err := decodeBulk(j)
	if err != nil {
		return err
	}
	prog := jobs.Progress{Total: len(req.Targets)}
	failed := 0
	for i, t := range req.Targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		itemErr := h.item(ctx, j, req, t)
		if itemErr != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		item := jobs.ItemResult{Name: itemName(t), Status: "done"}
		if itemErr != nil {
			failed++
			item.Status, item.Message = "failed", itemErr.Error()
		}
		prog.Done = i + 1
		prog.Text = fmt.Sprintf("Processed %d of %d items", prog.Done, prog.Total)
		prog.Items = append(prog.Items, item)
		report(prog)
	}
	if failed > 0 {
		// the UI builds the follow-up job from the items that failed; repeating the whole job would redo the rest
		return jobs.WithOffers(fmt.Errorf("%d of %d items failed", failed, len(req.Targets)))
	}
	return nil
}

func (h *bulkHandler) item(ctx context.Context, j *jobs.Job, req BulkRequest, t Target) error {
	if t.Group == "core" {
		t.Group = ""
	}
	sess := j.Session()
	gvr := schema.GroupVersionResource{Group: t.Group, Version: t.Version, Resource: t.Resource}
	ev := audit.Event{Cluster: j.Cluster, Namespace: t.Namespace, ResourceKind: t.Resource, ResourceName: t.Name, Details: map[string]any{"job": j.ID}}
	switch req.Action {
	case "delete":
		ev.Verb = audit.VerbDelete
		return h.audited(ctx, ev, func() error {
			return h.res.Delete(ctx, sess, j.Cluster, gvr, t.Namespace, t.Name, metav1.DeletePropagationBackground)
		})
	case "label":
		p, _ := decodeLabels(req.Params)
		body, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": labelPatch(p)}})
		if err != nil {
			return err
		}
		ev.Verb = audit.VerbLabel
		return h.audited(ctx, ev, func() error {
			_, err := h.res.Patch(ctx, sess, j.Cluster, gvr, t.Namespace, t.Name, types.MergePatchType, body, false)
			return err
		})
	case "drain":
		return h.dr.Drain(ctx, sess, j.Cluster, t.Name, func(jobs.Progress) {})
	default: // restart, cordon, uncordon: the audited actions of the single-object API
		return h.svc.Do(ctx, sess, j.Cluster, Request{Action: req.Action, Target: t})
	}
}

func labelPatch(p labelParams) map[string]any {
	out := map[string]any{}
	for k, v := range p.Add {
		out[k] = v
	}
	for _, k := range p.Remove {
		out[k] = nil
	}
	return out
}

func (h *bulkHandler) audited(ctx context.Context, ev audit.Event, fn func() error) error {
	p, err := h.d.Audit.Begin(ctx, ev)
	if err != nil {
		return apperr.ErrNotRecorded
	}
	err = fn()
	p.Done(context.WithoutCancel(ctx), err)
	return err
}

// Recover says which items were done, from the progress saved by the dead replica. The job is never
// continued automatically.
func (h *bulkHandler) Recover(_ context.Context, j *jobs.Job) (jobs.Status, string, []string) {
	total := j.Progress.Total
	if req, err := decodeBulk(j); err == nil {
		total = len(req.Targets)
	}
	var done []string
	for _, it := range j.Progress.Items {
		if it.Status == "done" {
			done = append(done, it.Name)
		}
	}
	msg := fmt.Sprintf("The server stopped part-way: %d of %d items were done", len(done), total)
	switch {
	case len(done) == 0:
		msg += "."
	case len(done) <= maxListedItems:
		msg += " (" + strings.Join(done, ", ") + ")."
	default:
		msg += fmt.Sprintf(" (%s and %d more).", strings.Join(done[:maxListedItems], ", "), len(done)-maxListedItems)
	}
	return jobs.Interrupted, msg, []string{jobs.OfferRetry}
}
