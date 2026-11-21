package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/session"
)

// The Helm job types. Their names start with "helm", so they share one limit group (`helm.maxConcurrentJobs`).
const (
	ActionInstall   = "install"
	ActionUpgrade   = "upgrade"
	ActionRollback  = "rollback"
	ActionUninstall = "uninstall"
	ActionTest      = "test"
)

var actions = []string{ActionInstall, ActionUpgrade, ActionRollback, ActionUninstall, ActionTest}

// JobType is the job type of an action.
func JobType(action string) string { return "helm-" + action }

var auditVerbs = map[string]string{
	ActionInstall:   audit.VerbHelmInstall,
	ActionUpgrade:   audit.VerbHelmUpgrade,
	ActionRollback:  audit.VerbHelmRollback,
	ActionUninstall: audit.VerbHelmUninstall,
	ActionTest:      audit.VerbHelmTest,
}

// ChartRef names a chart: in a repository (by the name the user gave it) or by an OCI reference.
type ChartRef struct {
	Repo    string `json:"repo,omitempty"`
	OCI     string `json:"oci,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// JobRequest is the sealed parameters of a Helm job. The values may hold secrets: they are never audited and
// never leave the job's sealed parameters.
type JobRequest struct {
	Namespace string         `json:"namespace"`
	Name      string         `json:"name"`
	Chart     *ChartRef      `json:"chart,omitempty"`
	Values    map[string]any `json:"values,omitempty"`
	// Revision is the revision a rollback goes to.
	Revision int `json:"revision,omitempty"`
}

// JobTarget is what the job list shows, and what recovery needs: the revision the release was at when the job
// was submitted.
type JobTarget struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	Action        string `json:"action"`
	Chart         string `json:"chart,omitempty"`
	ChartVersion  string `json:"chartVersion,omitempty"`
	StartRevision int    `json:"startRevision"`
}

// Validate checks a request for an action.
func (r JobRequest) Validate(action string) error {
	if len(validation.IsDNS1123Label(r.Namespace)) > 0 {
		return fmt.Errorf("%q is not a namespace name: %w", r.Namespace, apperr.ErrInvalid)
	}
	if len(validation.IsDNS1123Subdomain(r.Name)) > 0 || len(r.Name) > 53 {
		return fmt.Errorf("%q is not a release name: %w", r.Name, apperr.ErrInvalid)
	}
	switch action {
	case ActionInstall, ActionUpgrade:
		return r.validateChart()
	case ActionRollback:
		if r.Revision < 1 {
			return fmt.Errorf("a rollback needs the revision to go back to: %w", apperr.ErrInvalid)
		}
	}
	return nil
}

func (r JobRequest) validateChart() error {
	c := r.Chart
	switch {
	case c == nil:
		return fmt.Errorf("no chart given: %w", apperr.ErrInvalid)
	case c.Repo != "" && c.OCI != "":
		return fmt.Errorf("give a repository or an OCI reference, not both: %w", apperr.ErrInvalid)
	case c.OCI != "":
		if !strings.HasPrefix(c.OCI, "oci://") {
			return fmt.Errorf("an OCI reference starts with oci://: %w", apperr.ErrInvalid)
		}
	case c.Repo != "" && c.Name != "":
	default:
		return fmt.Errorf("give a repository and a chart name, or an OCI reference: %w", apperr.ErrInvalid)
	}
	return nil
}

// DriverFactory makes the Helm driver of a cluster for the user of a job.
type DriverFactory interface {
	Driver(ctx context.Context, sess session.Session, clusterID, namespace, workDir string) (Driver, error)
}

// JobDeps are the dependencies of the Helm job handlers.
type JobDeps struct {
	Factory DriverFactory
	Repos   *Repos
	Fetcher *Fetcher
	Audit   *audit.Recorder
	// TempRoot is where each job makes its own working directory ("" is the system's temporary directory).
	TempRoot string
}

type handler struct {
	d      JobDeps
	action string
}

// NewJobHandlers returns one handler per Helm action.
func NewJobHandlers(d JobDeps) []jobs.Handler {
	out := make([]jobs.Handler, 0, len(actions))
	for _, a := range actions {
		out = append(out, &handler{d: d, action: a})
	}
	return out
}

func (h *handler) Type() string { return JobType(h.action) }

func decode(j *jobs.Job, action string) (JobRequest, error) {
	var req JobRequest
	if err := json.Unmarshal(j.Params, &req); err != nil {
		return req, fmt.Errorf("the job's parameters are not valid: %w", apperr.ErrInvalid)
	}
	return req, req.Validate(action)
}

// withWorkDir gives fn a fresh directory and removes it afterwards, whether fn succeeded or not.
func (h *handler) withWorkDir(fn func(dir string) error) error {
	dir, err := os.MkdirTemp(h.d.TempRoot, "kubelook-helm-*")
	if err != nil {
		return fmt.Errorf("creating the job's directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	return fn(dir)
}

// Run implements jobs.Handler. The action is recorded in the audit log before it starts, without the values.
func (h *handler) Run(ctx context.Context, j *jobs.Job, report func(jobs.Progress)) error {
	req, err := decode(j, h.action)
	if err != nil {
		return err
	}
	details := map[string]any{"job": j.ID}
	if req.Chart != nil {
		details["chart"] = req.Chart.Name
		details["version"] = req.Chart.Version
		if req.Chart.OCI != "" {
			details["chart"] = req.Chart.OCI
		}
	}
	if h.action == ActionRollback {
		details["revision"] = req.Revision
	}
	p, err := h.d.Audit.Begin(ctx, audit.Event{
		Cluster: j.Cluster, Namespace: req.Namespace, Verb: auditVerbs[h.action],
		ResourceKind: "helm release", ResourceName: req.Name, Details: details,
	})
	if err != nil {
		return apperr.ErrNotRecorded
	}
	err = h.withWorkDir(func(dir string) error {
		report(jobs.Progress{Text: fmt.Sprintf("Running helm %s for %s/%s", h.action, req.Namespace, req.Name)})
		return h.do(ctx, j, req, dir, p)
	})
	p.Done(context.WithoutCancel(ctx), err)
	return err
}

func (h *handler) source(ctx context.Context, sess session.Session, c *ChartRef, dir string) (ChartSource, error) {
	if c.OCI != "" {
		return ChartSource{OCI: c.OCI, Version: c.Version}, nil
	}
	repo, err := h.d.Repos.Resolve(ctx, sess, c.Repo)
	if err != nil {
		return ChartSource{}, err
	}
	path, err := h.d.Fetcher.Fetch(ctx, repo, c.Name, c.Version, dir)
	if err != nil {
		return ChartSource{}, err
	}
	return ChartSource{Path: path}, nil
}

func (h *handler) do(ctx context.Context, j *jobs.Job, req JobRequest, dir string, p *audit.Pending) error {
	sess := j.Session()
	drv, err := h.d.Factory.Driver(ctx, sess, j.Cluster, req.Namespace, dir)
	if err != nil {
		return err
	}
	switch h.action {
	case ActionInstall, ActionUpgrade:
		src, err := h.source(ctx, sess, req.Chart, dir)
		if err != nil {
			return err
		}
		var rel *Release
		if h.action == ActionInstall {
			rel, err = drv.Install(ctx, InstallOptions{Namespace: req.Namespace, Name: req.Name, Chart: src, Values: req.Values})
		} else {
			rel, err = drv.Upgrade(ctx, UpgradeOptions{Namespace: req.Namespace, Name: req.Name, Chart: src, Values: req.Values})
		}
		if err != nil {
			return err
		}
		p.Annotate("chartVersion", rel.ChartVersion)
		p.Annotate("revision", rel.Revision)
		return nil
	case ActionRollback:
		return drv.Rollback(ctx, req.Namespace, req.Name, req.Revision)
	case ActionUninstall:
		return drv.Uninstall(ctx, req.Namespace, req.Name)
	default:
		res, err := drv.Test(ctx, req.Namespace, req.Name)
		for _, hook := range res.Hooks {
			if hook.Phase == "Failed" {
				return fmt.Errorf("the test %s failed", hook.Name)
			}
		}
		return err
	}
}

// Observed is what recovery sees of a release.
type Observed struct {
	Exists       bool
	Revision     int
	Status       string
	ChartVersion string
	// ValuesMatch reports that the user values of the release are the ones the job was asked to apply.
	ValuesMatch bool
}

// Classify decides how a Helm job that lost its replica ended, from what the cluster shows. It never redoes the
// work. A release left in a pending state offers the way out of it.
func Classify(action string, startRevision int, wantVersion string, o Observed) (jobs.Status, string, []string) {
	retry := []string{jobs.OfferRetry}
	switch action {
	case ActionUninstall:
		if !o.Exists {
			return jobs.Succeeded, "The release is gone.", nil
		}
		return jobs.Interrupted, "The release still exists, so the uninstall did not finish.", retry
	case ActionTest:
		return jobs.Interrupted, "The result of a test run cannot be checked afterwards.", retry
	}
	if !o.Exists {
		return jobs.Interrupted, "The release does not exist, so the action did not take effect.", retry
	}
	switch o.Status {
	case "pending-install":
		return jobs.Interrupted, "The install was cut off and left the release half installed.", []string{jobs.OfferUninstall}
	case "pending-upgrade", "pending-rollback":
		return jobs.Interrupted, "The change was cut off and left the release half changed.", []string{jobs.OfferRollback}
	case "failed":
		return jobs.Failed, "The release is in a failed state.", nil
	}
	if o.Revision <= startRevision && action != ActionInstall {
		return jobs.Interrupted, "The release is unchanged from before the job started.", retry
	}
	switch action {
	case ActionRollback:
		return jobs.Succeeded, "The release was rolled back.", nil
	default:
		if o.Status == "deployed" && o.ChartVersion == wantVersion && o.ValuesMatch {
			return jobs.Succeeded, "The release is at the chart version and values of the job.", nil
		}
		return jobs.Interrupted, "The release does not match what the job was to apply.", retry
	}
}

// Recover implements jobs.Handler. It only looks.
func (h *handler) Recover(ctx context.Context, j *jobs.Job) (jobs.Status, string, []string) {
	req, err := decode(j, h.action)
	if err != nil {
		return jobs.Interrupted, "The job's parameters could not be read, so its result could not be checked.", nil
	}
	var target JobTarget
	_ = json.Unmarshal(j.Target, &target)
	var (
		status  jobs.Status
		message string
		offers  []string
	)
	err = h.withWorkDir(func(dir string) error {
		drv, err := h.d.Factory.Driver(ctx, j.Session(), j.Cluster, req.Namespace, dir)
		if err != nil {
			return err
		}
		obs := Observed{}
		rel, err := drv.Status(ctx, req.Namespace, req.Name)
		switch {
		case errors.Is(err, apperr.ErrNotFound):
		case err != nil:
			return err
		default:
			obs = Observed{Exists: true, Revision: rel.Revision, Status: rel.Status, ChartVersion: rel.ChartVersion}
			if d, derr := drv.Get(ctx, req.Namespace, req.Name, 0); derr == nil {
				obs.ValuesMatch = sameValues(d.UserValues, req.Values)
			}
		}
		status, message, offers = Classify(h.action, target.StartRevision, h.wantedVersion(ctx, j, req, dir), obs)
		return nil
	})
	if err != nil {
		return jobs.Interrupted, "The result of this job could not be checked.", nil
	}
	return status, message, offers
}

// wantedVersion is the chart version the job was to deploy: the one it named, or for an install or an upgrade
// without a version the newest the repository offers.
func (h *handler) wantedVersion(ctx context.Context, j *jobs.Job, req JobRequest, dir string) string {
	if req.Chart == nil {
		return ""
	}
	if req.Chart.Version != "" || req.Chart.OCI != "" {
		return req.Chart.Version
	}
	sess := j.Session()
	repo, err := h.d.Repos.Resolve(ctx, sess, req.Chart.Repo)
	if err != nil {
		return ""
	}
	path, err := h.d.Fetcher.Fetch(ctx, repo, req.Chart.Name, "", dir)
	if err != nil {
		return ""
	}
	return chartVersionOf(path)
}

func sameValues(current string, want map[string]any) bool {
	b, err := yaml.Marshal(want)
	if err != nil {
		return false
	}
	var a, c any
	if yaml.Unmarshal([]byte(current), &a) != nil || yaml.Unmarshal(b, &c) != nil {
		return false
	}
	ja, _ := json.Marshal(a)
	jc, _ := json.Marshal(c)
	return string(ja) == string(jc) || (isEmpty(a) && isEmpty(c))
}

func isEmpty(v any) bool {
	m, ok := v.(map[string]any)
	return v == nil || (ok && len(m) == 0)
}
