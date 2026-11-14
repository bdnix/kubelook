package execs

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

const (
	nodeShellPrefix    = "kubelook-node-shell-"
	nodeShellNamespace = "default"
	nodeShellContainer = "shell"
	managedByLabel     = "app.kubernetes.io/managed-by"
)

// NodeShellCommand enters the namespaces of the node's init process from the node shell pod.
var NodeShellCommand = []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "/bin/sh"}

// NodeShell is the pod a node shell runs in.
type NodeShell struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
}

func (g *Debugger) nodeShellEnabled() error {
	if !g.d.NodeShell.IsEnabled() {
		return fmt.Errorf("node shells are not enabled: %w", apperr.ErrNotFound)
	}
	return nil
}

// CreateNodeShell starts a privileged pod on the node and waits until it runs.
func (g *Debugger) CreateNodeShell(ctx context.Context, sess session.Session, clusterID, node string) (NodeShell, error) {
	if err := g.nodeShellEnabled(); err != nil {
		return NodeShell{}, err
	}
	if node == "" {
		return NodeShell{}, fmt.Errorf("a node is required: %w", apperr.ErrInvalid)
	}
	typed, err := g.d.Clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return NodeShell{}, err
	}
	suffix, err := g.suffix()
	if err != nil {
		return NodeShell{}, err
	}
	name := nodeShellPrefix + suffix
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: nodeShellNamespace, Labels: map[string]string{managedByLabel: "kubelook"}},
		Spec: corev1.PodSpec{
			NodeName: node, HostPID: true, HostNetwork: true, HostIPC: true, RestartPolicy: corev1.RestartPolicyNever,
			Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			Containers: []corev1.Container{{
				Name: nodeShellContainer, Image: g.d.NodeShell.Image, Command: []string{"sleep", "infinity"},
				Stdin: true, TTY: true, SecurityContext: &corev1.SecurityContext{Privileged: ptrTo(true)},
			}},
		},
	}
	p, err := g.d.Audit.Begin(ctx, audit.Event{
		Cluster: clusterID, Namespace: nodeShellNamespace, Verb: audit.VerbDebugContainer, ResourceKind: "pods", ResourceName: name,
		Details: map[string]any{"node": node, "image": g.d.NodeShell.Image, "profile": "node-shell"},
	})
	if err != nil {
		return NodeShell{}, apperr.ErrNotRecorded
	}
	err = g.runNodeShell(ctx, typed, pod)
	p.Done(context.WithoutCancel(ctx), err)
	if err != nil {
		return NodeShell{}, err
	}
	return NodeShell{Namespace: nodeShellNamespace, Pod: name, Container: nodeShellContainer}, nil
}

func (g *Debugger) runNodeShell(ctx context.Context, typed kubernetes.Interface, pod *corev1.Pod) error {
	if _, err := typed.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{FieldManager: kube.FieldManager}); err != nil {
		return kube.MapError(err)
	}
	err := waitContainerRunning(ctx, typed, g.d.Clock, pod.Namespace, pod.Name, nodeShellContainer, startTimeout)
	if err != nil {
		// A shell that never started must not stay behind.
		_ = typed.CoreV1().Pods(pod.Namespace).Delete(context.WithoutCancel(ctx), pod.Name, metav1.DeleteOptions{})
	}
	return err
}

// DeleteNodeShell removes a node shell pod. Only pods that Kubelook made are deleted.
func (g *Debugger) DeleteNodeShell(ctx context.Context, sess session.Session, clusterID, pod string) error {
	if err := g.nodeShellEnabled(); err != nil {
		return err
	}
	typed, err := g.d.Clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	pods := typed.CoreV1().Pods(nodeShellNamespace)
	existing, err := pods.Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return kube.MapError(err)
	}
	if !strings.HasPrefix(pod, nodeShellPrefix) || existing.Labels[managedByLabel] != "kubelook" {
		return fmt.Errorf("%q is not a node shell: %w", pod, apperr.ErrNotFound)
	}
	p, err := g.d.Audit.Begin(ctx, audit.Event{
		Cluster: clusterID, Namespace: nodeShellNamespace, Verb: audit.VerbDelete, ResourceKind: "pods", ResourceName: pod,
		Details: map[string]any{"profile": "node-shell"},
	})
	if err != nil {
		return apperr.ErrNotRecorded
	}
	err = kube.MapError(pods.Delete(ctx, pod, metav1.DeleteOptions{}))
	p.Done(context.WithoutCancel(ctx), err)
	return err
}
