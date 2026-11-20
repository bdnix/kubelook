package helm4

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"
	release "helm.sh/helm/v4/pkg/release/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/redact"
)

// operationTimeout is the fixed timeout of every Helm operation in v1.
const operationTimeout = 10 * time.Minute

// Locator finds a chart by reference and returns the path of the downloaded archive. It is the SDK's
// ChartPathOptions.LocateChart, called on an install action that carries the registry client; tests replace it.
type Locator func(inst *action.Install, ref string, settings *cli.EnvSettings) (string, error)

// WithLocator replaces the chart locator.
func (a *Adapter) WithLocator(l Locator) *Adapter {
	a.locate = l
	return a
}

func defaultLocate(inst *action.Install, ref string, settings *cli.EnvSettings) (string, error) {
	return inst.LocateChart(ref, settings)
}

// settings points every file Helm may write at the adapter's working directory.
func (a *Adapter) settings() *cli.EnvSettings {
	s := cli.New()
	if a.workDir != "" {
		s.RepositoryCache = filepath.Join(a.workDir, "cache")
		s.RepositoryConfig = filepath.Join(a.workDir, "repositories.yaml")
		s.RegistryConfig = filepath.Join(a.workDir, "registry.json")
		s.ContentCache = filepath.Join(a.workDir, "content")
	}
	return s
}

func (a *Adapter) loadChart(namespace string, src helm.ChartSource) (*chart.Chart, error) {
	path := src.Path
	if src.OCI != "" {
		located, err := a.fetchOCI(namespace, src)
		if err != nil {
			return nil, err
		}
		path = located
	}
	if path == "" {
		return nil, fmt.Errorf("no chart given: %w", apperr.ErrInvalid)
	}
	ch, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading the chart: %v: %w", err, apperr.ErrInvalid)
	}
	return ch, nil
}

// fetchOCI pulls the chart with the SDK. The reference goes to the SDK's locator unchanged; its files stay in
// the working directory.
func (a *Adapter) fetchOCI(namespace string, src helm.ChartSource) (string, error) {
	settings := a.settings()
	client, err := registry.NewClient(registry.ClientOptCredentialsFile(settings.RegistryConfig))
	if err != nil {
		return "", fmt.Errorf("creating the registry client: %w", err)
	}
	cfg, err := a.open(namespace)
	if err != nil {
		return "", err
	}
	inst := action.NewInstall(cfg)
	inst.SetRegistryClient(client)
	inst.Version = src.Version
	located, err := a.locate(inst, src.OCI, settings)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %v: %w", src.OCI, err, apperr.ErrInvalid)
	}
	return located, nil
}

func (a *Adapter) last(namespace, name string) (*release.Release, *action.Configuration, error) {
	cfg, err := a.open(namespace)
	if err != nil {
		return nil, nil, err
	}
	r, err := cfg.Releases.Last(name)
	if err != nil {
		return nil, cfg, notFound(namespace, name, err)
	}
	rel, err := releaseOf(r)
	return rel, cfg, err
}

// Status implements helm.Driver.
func (a *Adapter) Status(_ context.Context, namespace, name string) (*helm.Release, error) {
	rel, _, err := a.last(namespace, name)
	if err != nil {
		return nil, err
	}
	r := toRelease(rel)
	return &r, nil
}

// Install implements helm.Driver. A first install uses the SDK's default way of applying.
func (a *Adapter) Install(ctx context.Context, o helm.InstallOptions) (*helm.Release, error) {
	if _, _, err := a.last(o.Namespace, o.Name); err == nil {
		return nil, fmt.Errorf("release %s/%s exists: %w", o.Namespace, o.Name, apperr.ErrConflict)
	} else if !errors.Is(err, apperr.ErrNotFound) {
		return nil, err
	}
	ch, err := a.loadChart(o.Namespace, o.Chart)
	if err != nil {
		return nil, err
	}
	cfg, err := a.open(o.Namespace)
	if err != nil {
		return nil, err
	}
	inst := action.NewInstall(cfg)
	inst.ReleaseName = o.Name
	inst.Namespace = o.Namespace
	inst.Timeout = operationTimeout
	done, err := inst.RunWithContext(ctx, ch, o.Values)
	if err != nil {
		return nil, fmt.Errorf("installing %s/%s: %w", o.Namespace, o.Name, err)
	}
	return a.released(done)
}

func (a *Adapter) released(r any) (*helm.Release, error) {
	rel, err := releaseOf(r)
	if err != nil {
		return nil, err
	}
	out := toRelease(rel)
	return &out, nil
}

// Upgrade implements helm.Driver. It never forces server-side apply onto a release that uses client-side apply:
// "auto" follows the apply method the latest revision recorded, and a record without one (every release made by
// Helm 3) is client-side.
func (a *Adapter) Upgrade(ctx context.Context, o helm.UpgradeOptions) (*helm.Release, error) {
	if _, _, err := a.last(o.Namespace, o.Name); err != nil {
		return nil, err
	}
	ch, err := a.loadChart(o.Namespace, o.Chart)
	if err != nil {
		return nil, err
	}
	cfg, err := a.open(o.Namespace)
	if err != nil {
		return nil, err
	}
	up := action.NewUpgrade(cfg)
	up.Namespace = o.Namespace
	up.Timeout = operationTimeout
	up.ServerSideApply = applyFollowsRelease
	done, err := up.RunWithContext(ctx, o.Name, ch, o.Values)
	if err != nil {
		return nil, fmt.Errorf("upgrading %s/%s: %w", o.Namespace, o.Name, err)
	}
	return a.released(done)
}

// applyFollowsRelease is the SDK's value for "use what the release used before".
const applyFollowsRelease = "auto"

// Rollback implements helm.Driver; like Upgrade it keeps the apply method of the release.
func (a *Adapter) Rollback(_ context.Context, namespace, name string, revision int) error {
	if _, _, err := a.last(namespace, name); err != nil {
		return err
	}
	cfg, err := a.open(namespace)
	if err != nil {
		return err
	}
	rb := action.NewRollback(cfg)
	rb.Version = revision
	rb.Timeout = operationTimeout
	rb.ServerSideApply = applyFollowsRelease
	if err := rb.Run(name); err != nil {
		return fmt.Errorf("rolling back %s/%s to revision %d: %w", namespace, name, revision, notFoundOr(err))
	}
	return nil
}

// notFoundOr maps the SDK's "no such revision" and "release: not found" errors.
func notFoundOr(err error) error {
	if errors.Is(err, errReleaseGone) {
		return fmt.Errorf("%v: %w", err, apperr.ErrNotFound)
	}
	return err
}

// Uninstall implements helm.Driver.
func (a *Adapter) Uninstall(_ context.Context, namespace, name string) error {
	cfg, err := a.open(namespace)
	if err != nil {
		return err
	}
	u := action.NewUninstall(cfg)
	u.Timeout = operationTimeout
	if _, err := u.Run(name); err != nil {
		return notFound(namespace, name, err)
	}
	return nil
}

// Test implements helm.Driver.
func (a *Adapter) Test(_ context.Context, namespace, name string) (helm.TestResult, error) {
	cfg, err := a.open(namespace)
	if err != nil {
		return helm.TestResult{}, err
	}
	rt := action.NewReleaseTesting(cfg)
	rt.Namespace = namespace
	rt.Timeout = operationTimeout
	r, shutdown, runErr := rt.Run(name)
	if shutdown != nil {
		defer func() { _ = shutdown() }()
	}
	res := helm.TestResult{Hooks: []helm.HookResult{}}
	if rel, err := releaseOf(r); err == nil {
		for _, h := range rel.Hooks {
			for _, e := range h.Events {
				if e == release.HookTest {
					res.Hooks = append(res.Hooks, helm.HookResult{Name: h.Name, Phase: string(h.LastRun.Phase)})
				}
			}
		}
	}
	if runErr != nil {
		return res, fmt.Errorf("testing %s/%s: %w", namespace, name, notFoundOr(runErr))
	}
	return res, nil
}

// Render implements helm.Driver with the SDK's client-side dry run, which stores nothing.
func (a *Adapter) Render(ctx context.Context, o helm.UpgradeOptions) (string, error) {
	ch, err := a.loadChart(o.Namespace, o.Chart)
	if err != nil {
		return "", err
	}
	cfg, err := a.open(o.Namespace)
	if err != nil {
		return "", err
	}
	var rel any
	if _, _, lerr := a.last(o.Namespace, o.Name); lerr == nil {
		up := action.NewUpgrade(cfg)
		up.Namespace = o.Namespace
		up.DryRunStrategy = action.DryRunClient
		up.ServerSideApply = applyFollowsRelease
		rel, err = up.RunWithContext(ctx, o.Name, ch, o.Values)
	} else {
		inst := action.NewInstall(cfg)
		inst.ReleaseName = o.Name
		inst.Namespace = o.Namespace
		inst.DryRunStrategy = action.DryRunClient
		inst.Replace = true
		rel, err = inst.RunWithContext(ctx, ch, o.Values)
	}
	if err != nil {
		return "", fmt.Errorf("rendering %s/%s: %w", o.Namespace, o.Name, err)
	}
	r, err := releaseOf(rel)
	if err != nil {
		return "", err
	}
	return redact.Manifest(r.Manifest), nil
}
