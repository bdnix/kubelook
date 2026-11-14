// Package actions runs the named operations of the UI (scale, restart, rollback, …) against a cluster with
// the user's own credentials. Every action is audited; adding one is adding a row to the table.
package actions

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// Target is the object an action works on. Group "core" (or empty) is the core group.
type Target struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Request is one action asked for.
type Request struct {
	Action string          `json:"action"`
	Target Target          `json:"target"`
	Params json.RawMessage `json:"params"`
}

// Deps are the dependencies of the service.
type Deps struct {
	Clients kube.Clients
	Clock   clock.Clock
	Audit   *audit.Recorder
}

// Service runs actions.
type Service struct {
	d        Deps
	handlers map[string]handler
}

// call is what a handler works with.
type call struct {
	ctx     context.Context
	svc     *Service
	sess    session.Session
	cluster string
	gvr     schema.GroupVersionResource
	t       Target
	params  json.RawMessage
	dyn     dynamic.Interface
	typed   kubernetes.Interface
}

// handler is one row of the table.
type handler struct {
	kinds []schema.GroupKind
	// verb is the audit verb.
	verb string
	// validate checks the parameters (and may read the object); an error means nothing is audited or done.
	validate func(c *call) error
	run      func(c *call) error
}

// New returns the service with every action registered.
func New(d Deps) *Service {
	s := &Service{d: d, handlers: map[string]handler{}}
	registerWorkloads(s.handlers)
	return s
}

func gk(group, kind string) schema.GroupKind { return schema.GroupKind{Group: group, Kind: kind} }

func (c *call) decode(v any) error {
	if len(c.params) == 0 {
		return fmt.Errorf("this action needs parameters: %w", apperr.ErrInvalid)
	}
	if err := json.Unmarshal(c.params, v); err != nil {
		return fmt.Errorf("the parameters are not valid: %w", apperr.ErrInvalid)
	}
	return nil
}

// Do runs the action. The audit record "requested" is written before anything is changed; when it cannot be
// written nothing is done.
func (s *Service) Do(ctx context.Context, sess session.Session, clusterID string, r Request) error {
	h, ok := s.handlers[r.Action]
	if !ok {
		return fmt.Errorf("unknown action %q: %w", r.Action, apperr.ErrInvalid)
	}
	if r.Target.Group == "core" {
		r.Target.Group = ""
	}
	t := r.Target
	if t.Version == "" || t.Resource == "" || t.Name == "" {
		return fmt.Errorf("the target needs a version, a resource and a name: %w", apperr.ErrInvalid)
	}
	gvr := schema.GroupVersionResource{Group: t.Group, Version: t.Version, Resource: t.Resource}
	disc, err := s.d.Clients.Discovery(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	entry, found := disc.Find(gvr)
	if !found {
		return fmt.Errorf("the cluster does not serve %s: %w", gvr.String(), apperr.ErrNotFound)
	}
	if !kindAllowed(h.kinds, schema.GroupKind{Group: t.Group, Kind: entry.Kind}) {
		return fmt.Errorf("%s does not apply to %s: %w", r.Action, entry.Kind, apperr.ErrInvalid)
	}
	dyn, err := s.d.Clients.Dynamic(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	typed, err := s.d.Clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	c := &call{ctx: ctx, svc: s, sess: sess, cluster: clusterID, gvr: gvr, t: t, params: r.Params, dyn: dyn, typed: typed}
	if h.validate != nil {
		if err := h.validate(c); err != nil {
			return err
		}
	}
	p, err := s.d.Audit.Begin(ctx, audit.Event{
		Cluster: clusterID, Namespace: t.Namespace, Verb: h.verb, ResourceKind: t.Resource, ResourceName: t.Name,
		Details: map[string]any{"action": r.Action},
	})
	if err != nil {
		return apperr.ErrNotRecorded
	}
	err = h.run(c)
	p.Done(context.WithoutCancel(ctx), err)
	return err
}

func kindAllowed(kinds []schema.GroupKind, k schema.GroupKind) bool {
	for _, a := range kinds {
		if a == k {
			return true
		}
	}
	return false
}
