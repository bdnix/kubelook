package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// evictionRetry is how long to wait before asking again after a PodDisruptionBudget refused an eviction.
const evictionRetry = 5 * time.Second

const mirrorPodAnnotation = "kubernetes.io/config.mirror"

// podFilter says whether a drain evicts the pod and, if not, why. It follows `kubectl drain
// --ignore-daemonsets --delete-emptydir-data`: pods with emptyDir volumes are evicted too.
func podFilter(p *corev1.Pod) (evict bool, reason string) {
	for _, o := range p.OwnerReferences {
		if o.Kind == "DaemonSet" {
			return false, "managed by a DaemonSet"
		}
	}
	if _, ok := p.Annotations[mirrorPodAnnotation]; ok {
		return false, "a mirror pod of a static pod"
	}
	if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
		return false, "already finished"
	}
	return true, ""
}

// Drainer drains nodes with the user's own credentials. It is built on the typed client and the clock
// only; nothing from kubectl is used.
type Drainer struct{ d Deps }

// NewDrainer returns a Drainer.
func NewDrainer(d Deps) *Drainer { return &Drainer{d: d} }

// Drain cordons the node, evicts its pods and waits until each is gone. An eviction that a
// PodDisruptionBudget refuses is retried every five seconds until the context ends. The drain is audited as
// one action; report receives "Evicted n of m pods".
func (dr *Drainer) Drain(ctx context.Context, sess session.Session, clusterID, node string, report func(jobs.Progress)) error {
	typed, err := dr.d.Clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	if _, err := typed.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{}); err != nil {
		return kube.MapError(err)
	}
	p, err := dr.d.Audit.Begin(ctx, audit.Event{Cluster: clusterID, Verb: audit.VerbDrain, ResourceKind: "nodes", ResourceName: node})
	if err != nil {
		return apperr.ErrNotRecorded
	}
	err = dr.drain(ctx, typed, node, report)
	p.Done(context.WithoutCancel(ctx), err)
	return err
}

func (dr *Drainer) drain(ctx context.Context, typed kubernetes.Interface, node string, report func(jobs.Progress)) error {
	if _, err := typed.CoreV1().Nodes().Patch(ctx, node, types.MergePatchType, []byte(`{"spec":{"unschedulable":true}}`), metav1.PatchOptions{FieldManager: kube.FieldManager}); err != nil {
		return kube.MapError(err)
	}
	victims, err := evictable(ctx, typed, node)
	if err != nil {
		return err
	}
	if len(victims) == 0 {
		report(jobs.Progress{Text: "No pods to evict"})
		return nil
	}
	total := len(victims)
	var (
		mu   sync.Mutex
		done int
	)
	// reports are made one at a time, so a newer count is never overtaken by an older one
	progress := func() {
		mu.Lock()
		defer mu.Unlock()
		done++
		report(jobs.Progress{Done: done, Total: total, Text: fmt.Sprintf("Evicted %d of %d pods", done, total)})
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	for i := range victims {
		pod := &victims[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := dr.evictAndWait(ctx, typed, pod); err != nil {
				cancel(err)
				return
			}
			progress()
		}()
	}
	wg.Wait()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return ctx.Err()
}

// evictable lists the pods a drain of the node evicts, sorted by namespace and name.
func evictable(ctx context.Context, typed kubernetes.Interface, node string) ([]corev1.Pod, error) {
	list, err := typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String()})
	if err != nil {
		return nil, kube.MapError(err)
	}
	var out []corev1.Pod
	for i := range list.Items {
		p := &list.Items[i]
		if p.Spec.NodeName != node {
			continue
		}
		if ok, _ := podFilter(p); ok {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (dr *Drainer) evictAndWait(ctx context.Context, typed kubernetes.Interface, pod *corev1.Pod) error {
	// Watch from the version listed, so a pod that vanishes before the watch starts is still seen.
	w, err := typed.CoreV1().Pods(pod.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", pod.Name).String(), ResourceVersion: pod.ResourceVersion,
	})
	if err != nil {
		return kube.MapError(err)
	}
	if err := dr.evict(ctx, typed, pod); err != nil {
		w.Stop()
		return err
	}
	return waitGone(ctx, typed, w, pod)
}

func (dr *Drainer) evict(ctx context.Context, typed kubernetes.Interface, pod *corev1.Pod) error {
	for {
		err := typed.CoreV1().Pods(pod.Namespace).EvictV1(ctx, &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}})
		switch {
		case err == nil, apierrors.IsNotFound(err):
			return nil
		case !apierrors.IsTooManyRequests(err):
			return kube.MapError(err)
		}
		// a PodDisruptionBudget refuses for now: ask again later
		t := dr.d.Clock.NewTimer(evictionRetry)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C():
		}
	}
}

// waitGone returns when the pod (identified by its UID, so a replacement with the same name counts as
// gone) is deleted. It owns w and stops it. A watch that ends early is replaced after checking once
// whether the pod is still there.
func waitGone(ctx context.Context, typed kubernetes.Interface, w watch.Interface, pod *corev1.Pod) error {
	defer func() { w.Stop() }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.ResultChan():
			if !ok {
				nw, gone, err := rewatch(ctx, typed, pod)
				if err != nil || gone {
					return err
				}
				w = nw
				continue
			}
			p, isPod := ev.Object.(*corev1.Pod)
			if !isPod || p.Name != pod.Name {
				continue
			}
			if p.UID != pod.UID || ev.Type == watch.Deleted {
				return nil
			}
		}
	}
}

// rewatch checks whether the pod is still there and, if so, watches it again from the version just read.
func rewatch(ctx context.Context, typed kubernetes.Interface, pod *corev1.Pod) (w watch.Interface, gone bool, err error) {
	cur, err := typed.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) || (err == nil && cur.UID != pod.UID) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, kube.MapError(err)
	}
	w, err = typed.CoreV1().Pods(pod.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", pod.Name).String(), ResourceVersion: cur.ResourceVersion,
	})
	if err != nil {
		return nil, false, kube.MapError(err)
	}
	return w, false, nil
}

type drainParams struct {
	Node string `json:"node"`
}

type drainHandler struct {
	d  Deps
	dr *Drainer
}

// NewDrainHandler returns the job handler of type "drain": params {"node": name}.
func NewDrainHandler(d Deps) jobs.Handler { return &drainHandler{d: d, dr: NewDrainer(d)} }

func (h *drainHandler) Type() string { return "drain" }

func decodeDrain(j *jobs.Job) (drainParams, error) {
	var p drainParams
	if err := json.Unmarshal(j.Params, &p); err != nil || p.Node == "" {
		return p, fmt.Errorf("the drain job has no node: %w", apperr.ErrInvalid)
	}
	return p, nil
}

func (h *drainHandler) Run(ctx context.Context, j *jobs.Job, report func(jobs.Progress)) error {
	p, err := decodeDrain(j)
	if err != nil {
		return err
	}
	return h.dr.Drain(ctx, j.Session(), j.Cluster, p.Node, report)
}

// Recover looks at the node: pods still to evict mean the drain stopped part-way.
func (h *drainHandler) Recover(ctx context.Context, j *jobs.Job) (jobs.Status, string, []string) {
	p, err := decodeDrain(j)
	if err != nil {
		return jobs.Interrupted, "The drain could not be checked: its parameters could not be read.", []string{jobs.OfferRetry}
	}
	typed, err := h.d.Clients.Typed(ctx, j.Session(), j.Cluster)
	if err != nil {
		return jobs.Interrupted, "The drain could not be checked: the cluster could not be reached.", []string{jobs.OfferRetry}
	}
	left, err := evictable(ctx, typed, p.Node)
	if err != nil {
		return jobs.Interrupted, "The drain could not be checked: the node's pods could not be listed.", []string{jobs.OfferRetry}
	}
	switch n := len(left); n {
	case 0:
		return jobs.Succeeded, "The node is drained.", nil
	case 1:
		return jobs.Interrupted, "Drain stopped part-way: 1 pod remains on the node.", []string{jobs.OfferRetry}
	default:
		return jobs.Interrupted, fmt.Sprintf("Drain stopped part-way: %d pods remain on the node.", n), []string{jobs.OfferRetry}
	}
}
