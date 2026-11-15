package metrics

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

const metricsGroup = "metrics.k8s.io"

// usage reads a quantity pair; text that is not a quantity counts as zero.
func usage(m map[string]any) (cpuMillicores, memoryBytes int64) {
	if s, ok := m["cpu"].(string); ok {
		if q, err := resource.ParseQuantity(s); err == nil {
			cpuMillicores = q.MilliValue()
		}
	}
	if s, ok := m["memory"].(string); ok {
		if q, err := resource.ParseQuantity(s); err == nil {
			memoryBytes = q.Value()
		}
	}
	return cpuMillicores, memoryBytes
}

// list reads the PodMetrics or NodeMetrics objects; metrics-server not being served is not an error.
func (s *Service) list(ctx context.Context, sess session.Session, cluster, kind, namespace string) (*unstructured.UnstructuredList, bool, error) {
	disc, err := s.d.Clients.Discovery(ctx, sess, cluster)
	if err != nil {
		return nil, false, err
	}
	gvr, err := disc.ResolveKind(metricsGroup, kind)
	if err != nil {
		return nil, false, nil //nolint:nilerr // "not served" means "unavailable", not a failure
	}
	dyn, err := s.d.Clients.Dynamic(ctx, sess, cluster)
	if err != nil {
		return nil, false, err
	}
	list, err := dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, false, kube.MapError(err)
	}
	return list, true, nil
}

// Pods returns the usage of the pods, summed over their containers; namespace "" is all namespaces.
func (s *Service) Pods(ctx context.Context, sess session.Session, cluster, namespace string) (Usage, error) {
	list, ok, err := s.list(ctx, sess, cluster, "PodMetrics", namespace)
	if err != nil || !ok {
		return Usage{Items: []Item{}}, err
	}
	items := make([]Item, 0, len(list.Items))
	for i := range list.Items {
		o := &list.Items[i]
		it := Item{Namespace: o.GetNamespace(), Name: o.GetName()}
		cs, _, _ := unstructured.NestedSlice(o.Object, "containers")
		for _, c := range cs {
			m, _ := c.(map[string]any)
			u, _ := m["usage"].(map[string]any)
			cpu, mem := usage(u)
			name, _ := m["name"].(string)
			it.CPUMillicores += cpu
			it.MemoryBytes += mem
			it.Containers = append(it.Containers, Container{Name: name, CPUMillicores: cpu, MemoryBytes: mem})
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
	return Usage{Available: true, Items: items}, nil
}

// Nodes returns the usage of the nodes.
func (s *Service) Nodes(ctx context.Context, sess session.Session, cluster string) (Usage, error) {
	list, ok, err := s.list(ctx, sess, cluster, "NodeMetrics", "")
	if err != nil || !ok {
		return Usage{Items: []Item{}}, err
	}
	items := make([]Item, 0, len(list.Items))
	for i := range list.Items {
		o := &list.Items[i]
		u, _, _ := unstructured.NestedMap(o.Object, "usage")
		cpu, mem := usage(u)
		items = append(items, Item{Name: o.GetName(), CPUMillicores: cpu, MemoryBytes: mem})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return Usage{Available: true, Items: items}, nil
}
