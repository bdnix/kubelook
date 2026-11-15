package logs

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

type kubeResolver struct{ clients kube.Clients }

// NewKubeResolver finds containers through the user's own clients.
func NewKubeResolver(c kube.Clients) Resolver { return kubeResolver{c} }

func (k kubeResolver) Resolve(ctx context.Context, sess session.Session, clusterID, ns string, spec SourceSpec) ([]Target, error) {
	typed, err := k.clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	switch {
	case spec.Pod != "":
		pod, err := typed.CoreV1().Pods(ns).Get(ctx, spec.Pod, metav1.GetOptions{})
		if err != nil {
			return nil, kube.MapError(err)
		}
		return podTargets(pod, true), nil
	case spec.Selector != "":
		return k.bySelector(ctx, sess, clusterID, ns, spec.Selector)
	case spec.Workload != nil:
		return k.byWorkload(ctx, sess, clusterID, ns, *spec.Workload)
	}
	return nil, fmt.Errorf("sources must name a pod, a selector or a workload: %w", apperr.ErrInvalid)
}

// podTargets lists a pod's containers; init containers come first and only when asked for.
func podTargets(pod *corev1.Pod, withInit bool) []Target {
	var out []Target
	if withInit {
		for _, c := range pod.Spec.InitContainers {
			out = append(out, Target{Pod: pod.Name, Container: c.Name})
		}
	}
	for _, c := range pod.Spec.Containers {
		out = append(out, Target{Pod: pod.Name, Container: c.Name})
	}
	return out
}

// bySelector lists the pods that have logs (not Pending) with their containers, sorted by pod name.
func (k kubeResolver) bySelector(ctx context.Context, sess session.Session, clusterID, ns, selector string) ([]Target, error) {
	if _, err := labels.Parse(selector); err != nil {
		return nil, fmt.Errorf("the label selector is not valid: %w", apperr.ErrInvalid)
	}
	typed, err := k.clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := typed.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, kube.MapError(err)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
	var out []Target
	for i := range list.Items {
		if list.Items[i].Status.Phase == corev1.PodPending {
			continue
		}
		out = append(out, podTargets(&list.Items[i], false)...)
	}
	return out, nil
}

// byWorkload follows the pods the workload's own selector matches.
func (k kubeResolver) byWorkload(ctx context.Context, sess session.Session, clusterID, ns string, w WorkloadRef) ([]Target, error) {
	disc, err := k.clients.Discovery(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	gvr, err := disc.ResolveKind(w.Group, w.Kind)
	if err != nil {
		return nil, err
	}
	dyn, err := k.clients.Dynamic(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	obj, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, w.Name, metav1.GetOptions{})
	if err != nil {
		return nil, kube.MapError(err)
	}
	raw, found, _ := unstructured.NestedMap(obj.Object, "spec", "selector")
	if !found {
		return nil, fmt.Errorf("%s %s has no pod selector: %w", w.Kind, w.Name, apperr.ErrInvalid)
	}
	var sel metav1.LabelSelector
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &sel); err != nil {
		return nil, fmt.Errorf("%s %s has a selector that cannot be read: %w", w.Kind, w.Name, apperr.ErrInvalid)
	}
	parsed, err := metav1.LabelSelectorAsSelector(&sel)
	if err != nil || parsed.Empty() {
		return nil, fmt.Errorf("%s %s has no usable pod selector: %w", w.Kind, w.Name, apperr.ErrInvalid)
	}
	return k.bySelector(ctx, sess, clusterID, ns, parsed.String())
}
