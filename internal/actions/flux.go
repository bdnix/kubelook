package actions

import (
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/flux"
)

// fluxKinds are the kinds the Flux actions apply to.
func fluxKinds() []schema.GroupKind {
	out := make([]schema.GroupKind, 0, len(flux.Kinds))
	for _, k := range flux.Kinds {
		out = append(out, gk(k.Group, k.Kind))
	}
	return out
}

// fluxKind is the Flux kind of the call's object; the service has already checked that it is one.
func (c *call) fluxKind() (flux.Kind, error) {
	disc, err := c.svc.d.Clients.Discovery(c.ctx, c.sess, c.cluster)
	if err != nil {
		return flux.Kind{}, err
	}
	entry, ok := disc.Find(c.gvr)
	if !ok {
		return flux.Kind{}, fmt.Errorf("the cluster does not serve %s: %w", c.gvr.String(), apperr.ErrNotFound)
	}
	k, ok := flux.Find(c.t.Group, entry.Kind)
	if !ok {
		return flux.Kind{}, fmt.Errorf("%s is not a Flux kind: %w", entry.Kind, apperr.ErrInvalid)
	}
	return k, nil
}

var errResumeFirst = &apperr.APIError{Status: http.StatusConflict, Code: "conflict", Message: "Resume the object before reconciling."}

func validateFluxReconcile(c *call) error {
	k, err := c.fluxKind()
	if err != nil {
		return err
	}
	obj, err := c.get()
	if err != nil {
		return err
	}
	if k.IsSuspended(obj.Object) {
		return errResumeFirst
	}
	return nil
}

func runFluxReconcile(c *call) error {
	return c.patch(types.MergePatchType, flux.ReconcilePatch(c.svc.d.Clock.Now()))
}

func fluxSuspendOrResume(suspend bool) (validate, run func(*call) error) {
	patch := func(c *call) ([]byte, error) {
		k, err := c.fluxKind()
		if err != nil {
			return nil, err
		}
		body, err := k.SuspendPatch(suspend)
		if err != nil {
			return nil, fmt.Errorf("a %s cannot be suspended: %w", k.Kind, apperr.ErrInvalid)
		}
		return body, nil
	}
	validate = func(c *call) error {
		_, err := patch(c)
		return err
	}
	run = func(c *call) error {
		body, err := patch(c)
		if err != nil {
			return err
		}
		return c.patch(types.MergePatchType, body)
	}
	return validate, run
}

func registerFlux(h map[string]handler) {
	kinds := fluxKinds()
	h["fluxReconcile"] = handler{kinds: kinds, verb: audit.VerbFluxReconcile, validate: validateFluxReconcile, run: runFluxReconcile}
	suspendValidate, suspendRun := fluxSuspendOrResume(true)
	h["fluxSuspend"] = handler{kinds: kinds, verb: audit.VerbFluxSuspend, validate: suspendValidate, run: suspendRun}
	resumeValidate, resumeRun := fluxSuspendOrResume(false)
	h["fluxResume"] = handler{kinds: kinds, verb: audit.VerbFluxResume, validate: resumeValidate, run: resumeRun}
}
