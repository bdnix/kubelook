package helm

import (
	"context"
	"errors"
	"fmt"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/session"
)

// Service is what the Helm API needs: the reads, the audited reveal of values, the preview of a change and the
// start of the jobs.
type Service struct {
	Factory  DriverFactory
	Repos    *Repos
	Fetcher  *Fetcher
	Audit    *audit.Recorder
	TempRoot string
}

// JobDeps are the dependencies of the job handlers of this service.
func (s *Service) JobDeps() JobDeps {
	return JobDeps{Factory: s.Factory, Repos: s.Repos, Fetcher: s.Fetcher, Audit: s.Audit, TempRoot: s.TempRoot}
}

func (s *Service) withDriver(ctx context.Context, sess session.Session, cluster, namespace string, fn func(Driver, string) error) error {
	h := &handler{d: s.JobDeps()}
	return h.withWorkDir(func(dir string) error {
		drv, err := s.Factory.Driver(ctx, sess, cluster, namespace, dir)
		if err != nil {
			return err
		}
		return fn(drv, dir)
	})
}

// List returns the releases of a namespace ("" is all).
func (s *Service) List(ctx context.Context, sess session.Session, cluster, namespace string) ([]Release, error) {
	var out []Release
	err := s.withDriver(ctx, sess, cluster, namespace, func(d Driver, _ string) (err error) {
		out, err = d.List(ctx, namespace)
		return err
	})
	return out, err
}

// Detail returns a release with its history. The values are part of the Detail; the caller must not send them
// on without the reveal step.
func (s *Service) Detail(ctx context.Context, sess session.Session, cluster, namespace, name string, revision int) (*Detail, []Revision, error) {
	var (
		d    *Detail
		hist []Revision
	)
	err := s.withDriver(ctx, sess, cluster, namespace, func(drv Driver, _ string) (err error) {
		if d, err = drv.Get(ctx, namespace, name, revision); err != nil {
			return err
		}
		hist, err = drv.History(ctx, namespace, name)
		return err
	})
	return d, hist, err
}

// Reveal returns the values of a release. The reveal is audited, whether or not it succeeds.
func (s *Service) Reveal(ctx context.Context, sess session.Session, cluster, namespace, name string, revision int) (user, computed string, err error) {
	p, err := s.Audit.Begin(ctx, audit.Event{
		Cluster: cluster, Namespace: namespace, Verb: audit.VerbHelmValuesReveal, ResourceKind: "helm release", ResourceName: name,
		Details: map[string]any{"revision": revision},
	})
	if err != nil {
		return "", "", apperr.ErrNotRecorded
	}
	err = s.withDriver(ctx, sess, cluster, namespace, func(drv Driver, _ string) error {
		d, err := drv.Get(ctx, namespace, name, revision)
		if err != nil {
			return err
		}
		user, computed = d.UserValues, d.ComputedValues
		return nil
	})
	p.Done(context.WithoutCancel(ctx), err)
	return user, computed, err
}

// Preview renders what an install or an upgrade would apply, next to the manifest that is deployed now.
func (s *Service) Preview(ctx context.Context, sess session.Session, cluster, action string, req JobRequest) (current, next string, err error) {
	if action != ActionInstall && action != ActionUpgrade {
		return "", "", fmt.Errorf("only an install or an upgrade can be previewed: %w", apperr.ErrInvalid)
	}
	if err := req.Validate(action); err != nil {
		return "", "", err
	}
	h := &handler{d: s.JobDeps(), action: action}
	err = s.withDriver(ctx, sess, cluster, req.Namespace, func(drv Driver, dir string) error {
		if d, gerr := drv.Get(ctx, req.Namespace, req.Name, 0); gerr == nil {
			current = d.Manifest
		} else if !errors.Is(gerr, apperr.ErrNotFound) {
			return gerr
		}
		src, err := h.source(ctx, sess, req.Chart, dir)
		if err != nil {
			return err
		}
		next, err = drv.Render(ctx, UpgradeOptions{Namespace: req.Namespace, Name: req.Name, Chart: src, Values: req.Values})
		return err
	})
	return current, next, err
}

// Submit starts a Helm job. It records the revision the release is at, so recovery can tell whether the job
// changed anything. An action on a release that does not exist is refused, except an install.
func (s *Service) Submit(ctx context.Context, runner *jobs.Runner, sess session.Session, cluster, action string, req JobRequest) (string, error) {
	if !validAction(action) {
		return "", fmt.Errorf("%q is not a Helm action: %w", action, apperr.ErrInvalid)
	}
	if err := req.Validate(action); err != nil {
		return "", err
	}
	target := JobTarget{Name: req.Name, Namespace: req.Namespace, Action: action}
	if req.Chart != nil {
		target.Chart, target.ChartVersion = req.Chart.Name, req.Chart.Version
		if req.Chart.OCI != "" {
			target.Chart = req.Chart.OCI
		}
	}
	err := s.withDriver(ctx, sess, cluster, req.Namespace, func(drv Driver, _ string) error {
		rel, err := drv.Status(ctx, req.Namespace, req.Name)
		switch {
		case errors.Is(err, apperr.ErrNotFound):
			if action != ActionInstall {
				return err
			}
			return nil
		case err != nil:
			return err
		}
		target.StartRevision = rel.Revision
		return nil
	})
	if err != nil {
		return "", err
	}
	return runner.Submit(ctx, sess, cluster, JobType(action), target, req)
}

func validAction(a string) bool {
	for _, v := range actions {
		if v == a {
			return true
		}
	}
	return false
}
