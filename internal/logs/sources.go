package logs

import (
	"context"
	"fmt"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/session"
)

// MaxStreams is the most containers one logs channel follows.
const MaxStreams = 20

// WorkloadRef names a workload whose pods are followed.
type WorkloadRef struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
}

// SourceSpec says which containers a channel follows: one container, every container of a pod, the pods
// matching a label selector, or the pods of a workload. The set is fixed when the channel opens.
type SourceSpec struct {
	Pod           string       `json:"pod"`
	Container     string       `json:"container"`
	AllContainers bool         `json:"allContainers"`
	Selector      string       `json:"selector"`
	Workload      *WorkloadRef `json:"workload"`
}

// Target is one container of one pod.
type Target struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
}

// Resolver turns a SourceSpec that names more than one container into the containers.
type Resolver interface {
	Resolve(ctx context.Context, sess session.Session, clusterID, namespace string, spec SourceSpec) ([]Target, error)
}

func (s SourceSpec) validate() error {
	n := 0
	if s.Pod != "" {
		n++
	}
	if s.Selector != "" {
		n++
	}
	if s.Workload != nil {
		n++
		if s.Workload.Kind == "" || s.Workload.Name == "" {
			return fmt.Errorf("a workload needs a kind and a name: %w", apperr.ErrInvalid)
		}
	}
	if n != 1 {
		return fmt.Errorf("sources must name exactly one of pod, selector or workload: %w", apperr.ErrInvalid)
	}
	if s.AllContainers && s.Pod == "" {
		return fmt.Errorf("allContainers goes with a pod: %w", apperr.ErrInvalid)
	}
	if s.Container != "" && (s.AllContainers || s.Pod == "") {
		return fmt.Errorf("a container goes with a pod and not with allContainers: %w", apperr.ErrInvalid)
	}
	if strings.ContainsAny(s.Selector, "\n\r") {
		return fmt.Errorf("the selector is not valid: %w", apperr.ErrInvalid)
	}
	return nil
}

// targets resolves the request's sources: at least one, at most MaxStreams.
func targets(ctx context.Context, d Deps, sess session.Session, req openRequest) ([]Target, error) {
	spec := SourceSpec{Pod: req.Pod, Container: req.Container}
	if req.Sources != nil {
		spec = *req.Sources
	}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	var out []Target
	if spec.Pod != "" && !spec.AllContainers {
		out = []Target{{Pod: spec.Pod, Container: spec.Container}}
	} else {
		if d.Resolver == nil {
			return nil, fmt.Errorf("following several containers is not available: %w", apperr.ErrInvalid)
		}
		var err error
		if out, err = d.Resolver.Resolve(ctx, sess, req.Cluster, req.Namespace, spec); err != nil {
			return nil, err
		}
	}
	switch {
	case len(out) == 0:
		return nil, fmt.Errorf("no containers match: %w", apperr.ErrNotFound)
	case len(out) > MaxStreams:
		return nil, fmt.Errorf("this would follow %d containers; at most %d can be followed at once: %w", len(out), MaxStreams, apperr.ErrInvalid)
	}
	return out, nil
}
