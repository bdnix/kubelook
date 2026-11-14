package actions

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/kube"
)

const restartedAt = "kubectl.kubernetes.io/restartedAt"

var (
	deployments  = []schema.GroupKind{gk("apps", "Deployment")}
	scalable     = []schema.GroupKind{gk("apps", "Deployment"), gk("apps", "StatefulSet"), gk("apps", "ReplicaSet")}
	restartable  = []schema.GroupKind{gk("apps", "Deployment"), gk("apps", "StatefulSet"), gk("apps", "DaemonSet")}
	cronJobs     = []schema.GroupKind{gk("batch", "CronJob")}
	pods         = []schema.GroupKind{gk("", "Pod")}
	claims       = []schema.GroupKind{gk("", "PersistentVolumeClaim")}
	jobsResource = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	rsResource   = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	scResource   = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
)

func registerWorkloads(h map[string]handler) {
	h["scale"] = handler{kinds: scalable, verb: audit.VerbScale, validate: validateScale, run: runScale}
	h["restart"] = handler{kinds: restartable, verb: audit.VerbRestart, run: runRestart}
	h["pause"] = handler{kinds: deployments, verb: audit.VerbPatch, run: mergePatch(`{"spec":{"paused":true}}`)}
	h["resume"] = handler{kinds: deployments, verb: audit.VerbPatch, run: mergePatch(`{"spec":{"paused":false}}`)}
	h["rollback"] = handler{kinds: deployments, verb: audit.VerbRollback, validate: validateRollback, run: runRollback}
	h["setImage"] = handler{kinds: restartable, verb: audit.VerbPatch, validate: validateSetImage, run: runSetImage}
	h["cronjobTrigger"] = handler{kinds: cronJobs, verb: audit.VerbCreate, run: runTrigger}
	h["cronjobSuspend"] = handler{kinds: cronJobs, verb: audit.VerbPatch, run: mergePatch(`{"spec":{"suspend":true}}`)}
	h["cronjobResume"] = handler{kinds: cronJobs, verb: audit.VerbPatch, run: mergePatch(`{"spec":{"suspend":false}}`)}
	h["evict"] = handler{kinds: pods, verb: audit.VerbEvict, run: runEvict}
	h["expandPVC"] = handler{kinds: claims, verb: audit.VerbPatch, validate: validateExpand, run: runExpand}
}

func (c *call) patch(pt types.PatchType, body []byte, sub ...string) error {
	_, err := c.dyn.Resource(c.gvr).Namespace(c.t.Namespace).Patch(c.ctx, c.t.Name, pt, body, metav1.PatchOptions{FieldManager: kube.FieldManager}, sub...)
	if err != nil {
		return kube.MapError(err)
	}
	return nil
}

func (c *call) get() (*unstructured.Unstructured, error) {
	o, err := c.dyn.Resource(c.gvr).Namespace(c.t.Namespace).Get(c.ctx, c.t.Name, metav1.GetOptions{})
	if err != nil {
		return nil, kube.MapError(err)
	}
	return o, nil
}

func mergePatch(body string) func(*call) error {
	return func(c *call) error { return c.patch(types.MergePatchType, []byte(body)) }
}

type scaleParams struct {
	Replicas *int `json:"replicas"`
}

func validateScale(c *call) error {
	var p scaleParams
	if err := c.decode(&p); err != nil {
		return err
	}
	if p.Replicas == nil || *p.Replicas < 0 {
		return fmt.Errorf("replicas must be zero or more: %w", apperr.ErrInvalid)
	}
	return nil
}

func runScale(c *call) error {
	var p scaleParams
	_ = c.decode(&p) // validated
	return c.patch(types.MergePatchType, []byte(`{"spec":{"replicas":`+strconv.Itoa(*p.Replicas)+`}}`), "scale")
}

func runRestart(c *call) error {
	now := c.svc.d.Clock.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{restartedAt: now}}}}})
	return c.patch(types.StrategicMergePatchType, body)
}

type rollbackParams struct {
	Revision int `json:"revision"`
}

func validateRollback(c *call) error {
	var p rollbackParams
	if err := c.decode(&p); err != nil {
		return err
	}
	if p.Revision <= 0 {
		return fmt.Errorf("revision must be a positive number: %w", apperr.ErrInvalid)
	}
	return nil
}

func runRollback(c *call) error {
	var p rollbackParams
	_ = c.decode(&p)
	dep, err := c.get()
	if err != nil {
		return err
	}
	sets, err := c.dyn.Resource(rsResource).Namespace(c.t.Namespace).List(c.ctx, metav1.ListOptions{})
	if err != nil {
		return kube.MapError(err)
	}
	want := strconv.Itoa(p.Revision)
	for i := range sets.Items {
		rs := &sets.Items[i]
		if rs.GetAnnotations()["deployment.kubernetes.io/revision"] != want || !ownedBy(rs, dep) {
			continue
		}
		tpl, found, _ := unstructured.NestedMap(rs.Object, "spec", "template")
		if !found {
			break
		}
		unstructured.RemoveNestedField(tpl, "metadata", "labels", "pod-template-hash")
		body, _ := json.Marshal([]map[string]any{{"op": "replace", "path": "/spec/template", "value": tpl}})
		return c.patch(types.JSONPatchType, body)
	}
	return fmt.Errorf("revision %d of %s was not found: %w", p.Revision, c.t.Name, apperr.ErrNotFound)
}

func ownedBy(child, owner *unstructured.Unstructured) bool {
	for _, r := range child.GetOwnerReferences() {
		if r.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

type imageParams struct {
	Container string `json:"container"`
	Image     string `json:"image"`
	Init      bool   `json:"init"`
}

func (p imageParams) field() string {
	if p.Init {
		return "initContainers"
	}
	return "containers"
}

func validateSetImage(c *call) error {
	var p imageParams
	if err := c.decode(&p); err != nil {
		return err
	}
	if p.Container == "" || p.Image == "" || strings.ContainsAny(p.Image, " \t\r\n") {
		return fmt.Errorf("a container and an image without whitespace are required: %w", apperr.ErrInvalid)
	}
	o, err := c.get()
	if err != nil {
		return err
	}
	list, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", p.field())
	for _, item := range list {
		if m, ok := item.(map[string]any); ok && m["name"] == p.Container {
			return nil
		}
	}
	return fmt.Errorf("%s has no %s named %q: %w", c.t.Name, strings.TrimSuffix(p.field(), "s"), p.Container, apperr.ErrInvalid)
}

func runSetImage(c *call) error {
	var p imageParams
	_ = c.decode(&p)
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
		p.field(): []map[string]string{{"name": p.Container, "image": p.Image}},
	}}}})
	return c.patch(types.StrategicMergePatchType, body)
}

func runTrigger(c *call) error {
	cj, err := c.get()
	if err != nil {
		return err
	}
	tpl, _, _ := unstructured.NestedMap(cj.Object, "spec", "jobTemplate")
	spec, _ := tpl["spec"].(map[string]any)
	labels, _, _ := unstructured.NestedStringMap(tpl, "metadata", "labels")
	job := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "batch/v1", "kind": "Job", "spec": spec}}
	job.SetName(fmt.Sprintf("%s-manual-%d", c.t.Name, c.svc.d.Clock.Now().Unix()))
	job.SetNamespace(c.t.Namespace)
	job.SetLabels(labels)
	job.SetAnnotations(map[string]string{"cronjob.kubernetes.io/instantiate": "manual"})
	yes := true
	job.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: cj.GetName(), UID: cj.GetUID(), Controller: &yes, BlockOwnerDeletion: &yes}})
	if _, err := c.dyn.Resource(jobsResource).Namespace(c.t.Namespace).Create(c.ctx, job, metav1.CreateOptions{FieldManager: kube.FieldManager}); err != nil {
		return kube.MapError(err)
	}
	return nil
}

func runEvict(c *call) error {
	err := c.typed.CoreV1().Pods(c.t.Namespace).EvictV1(c.ctx, &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: c.t.Name, Namespace: c.t.Namespace}})
	if apierrors.IsTooManyRequests(err) {
		return kube.Mapped("Cannot evict: a PodDisruptionBudget would be violated.", apperr.ErrConflict)
	}
	if err != nil {
		return kube.MapError(err)
	}
	return nil
}

type expandParams struct {
	Storage string `json:"storage"`
}

func validateExpand(c *call) error {
	var p expandParams
	if err := c.decode(&p); err != nil {
		return err
	}
	size, err := resource.ParseQuantity(p.Storage)
	if err != nil {
		return fmt.Errorf("%q is not a size: %w", p.Storage, apperr.ErrInvalid)
	}
	pvc, err := c.get()
	if err != nil {
		return err
	}
	current, _, _ := unstructured.NestedString(pvc.Object, "spec", "resources", "requests", "storage")
	if cur, err := resource.ParseQuantity(current); err == nil && size.Cmp(cur) <= 0 {
		return fmt.Errorf("the new size must be larger than %s: %w", current, apperr.ErrUnprocessable)
	}
	className, _, _ := unstructured.NestedString(pvc.Object, "spec", "storageClassName")
	class, err := c.dyn.Resource(scResource).Get(c.ctx, className, metav1.GetOptions{})
	if err != nil {
		return kube.MapError(err)
	}
	if allowed, _, _ := unstructured.NestedBool(class.Object, "allowVolumeExpansion"); !allowed {
		return fmt.Errorf("the storage class %q does not allow volume expansion: %w", className, apperr.ErrUnprocessable)
	}
	return nil
}

func runExpand(c *call) error {
	var p expandParams
	_ = c.decode(&p)
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"resources": map[string]any{"requests": map[string]string{"storage": p.Storage}}}})
	return c.patch(types.MergePatchType, body)
}
