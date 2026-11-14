package execs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

const (
	debugPrefix  = "kubelook-debug-"
	startTimeout = 60 * time.Second
	letters      = "abcdefghijklmnopqrstuvwxyz"
)

// DebuggerDeps are the dependencies of the debug toolkit.
type DebuggerDeps struct {
	Clients   kube.Clients
	Clock     clock.Clock
	Audit     *audit.Recorder
	Rand      io.Reader // random letters for names; crypto/rand.Reader in production
	Debug     config.Debug
	NodeShell config.NodeShell
}

// Debugger adds debug containers to pods and runs node shell pods.
type Debugger struct{ d DebuggerDeps }

// NewDebugger returns the debug toolkit.
func NewDebugger(d DebuggerDeps) *Debugger { return &Debugger{d: d} }

// PodRequest asks for a debug container in a pod.
type PodRequest struct {
	Namespace, Pod, Target, Profile string
}

// PodResult is the debug container to open a shell in.
type PodResult struct {
	Container string `json:"container"`
	Reused    bool   `json:"reused"`
}

// securityContext of every built-in profile (the names of `kubectl debug`).
func profileContext(profile string) (*corev1.SecurityContext, bool) {
	switch profile {
	case "", "general", "baseline":
		return nil, true
	case "restricted":
		return &corev1.SecurityContext{
			RunAsNonRoot: ptrTo(true), AllowPrivilegeEscalation: ptrTo(false),
			Capabilities:   &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}, true
	case "netadmin":
		return &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN", "NET_RAW"}}}, true
	case "sysadmin":
		return &corev1.SecurityContext{Privileged: ptrTo(true)}, true
	}
	return nil, false
}

// resolve returns the built-in profile and whether the target container is set. A configured profile name
// picks both; any other name must be a built-in profile.
func (g *Debugger) resolve(name string) (profile string, withTarget bool, err error) {
	for _, p := range g.d.Debug.Profiles {
		if p.Name == name {
			name = p.Profile
			if _, ok := profileContext(name); !ok {
				return "", false, fmt.Errorf("the configured profile %q uses the unknown profile %q: %w", p.Name, p.Profile, apperr.ErrInvalid)
			}
			return name, p.ShareProcessNamespace, nil
		}
	}
	if _, ok := profileContext(name); !ok {
		return "", false, fmt.Errorf("unknown debug profile %q: %w", name, apperr.ErrInvalid)
	}
	return name, true, nil
}

func (g *Debugger) suffix() (string, error) {
	b := make([]byte, 5)
	if _, err := io.ReadFull(g.d.Rand, b); err != nil {
		return "", fmt.Errorf("generating a name: %w", err)
	}
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b), nil
}

func containerNames(pod *corev1.Pod) map[string]bool {
	out := map[string]bool{}
	for _, c := range pod.Spec.Containers {
		out[c.Name] = true
	}
	for _, c := range pod.Spec.InitContainers {
		out[c.Name] = true
	}
	return out
}

func status(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for _, list := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for i := range list {
			if list[i].Name == name {
				return &list[i]
			}
		}
	}
	return nil
}

// reusable finds a running debug container of ours.
func reusable(pod *corev1.Pod) (string, bool) {
	for _, e := range pod.Spec.EphemeralContainers {
		if !strings.HasPrefix(e.Name, debugPrefix) {
			continue
		}
		if s := status(pod, e.Name); s != nil && s.State.Running != nil {
			return e.Name, true
		}
	}
	return "", false
}

// AddContainer adds a debug container, or reuses a running one, and waits until it runs.
func (g *Debugger) AddContainer(ctx context.Context, sess session.Session, clusterID string, r PodRequest) (PodResult, error) {
	profile, withTarget, err := g.resolve(r.Profile)
	if err != nil {
		return PodResult{}, err
	}
	typed, err := g.d.Clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return PodResult{}, err
	}
	pods := typed.CoreV1().Pods(r.Namespace)
	pod, err := pods.Get(ctx, r.Pod, metav1.GetOptions{})
	if err != nil {
		return PodResult{}, kube.MapError(err)
	}
	if name, ok := reusable(pod); ok {
		return PodResult{Container: name, Reused: true}, nil
	}
	names := containerNames(pod)
	target := r.Target
	if target == "" && len(pod.Spec.Containers) == 1 {
		target = pod.Spec.Containers[0].Name
	}
	if withTarget && !names[target] {
		return PodResult{}, fmt.Errorf("the pod has no container named %q to target: %w", target, apperr.ErrInvalid)
	}
	suffix, err := g.suffix()
	if err != nil {
		return PodResult{}, err
	}
	name := debugPrefix + suffix
	sc, _ := profileContext(profile)
	ec := corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
		Name: name, Image: g.d.Debug.Image, Command: []string{g.d.Debug.Shell}, Stdin: true, TTY: true, SecurityContext: sc,
	}}
	if withTarget {
		ec.TargetContainerName = target
	}
	p, err := g.d.Audit.Begin(ctx, audit.Event{
		Cluster: clusterID, Namespace: r.Namespace, Verb: audit.VerbDebugContainer, ResourceKind: "pods", ResourceName: r.Pod,
		Details: map[string]any{"container": name, "image": g.d.Debug.Image, "profile": profileLabel(r.Profile, profile)},
	})
	if err != nil {
		return PodResult{}, apperr.ErrNotRecorded
	}
	err = g.start(ctx, pods, pod, ec, name, typed)
	p.Done(context.WithoutCancel(ctx), err)
	if err != nil {
		return PodResult{}, err
	}
	return PodResult{Container: name}, nil
}

func profileLabel(asked, resolved string) string {
	if asked == "" {
		return "general"
	}
	if asked != resolved {
		return asked + " (" + resolved + ")"
	}
	return resolved
}

type podUpdater interface {
	UpdateEphemeralContainers(ctx context.Context, podName string, pod *corev1.Pod, opts metav1.UpdateOptions) (*corev1.Pod, error)
}

func (g *Debugger) start(ctx context.Context, pods podUpdater, pod *corev1.Pod, ec corev1.EphemeralContainer, name string, typed kubernetes.Interface) error {
	updated := pod.DeepCopy()
	updated.Spec.EphemeralContainers = append(updated.Spec.EphemeralContainers, ec)
	if _, err := pods.UpdateEphemeralContainers(ctx, pod.Name, updated, metav1.UpdateOptions{FieldManager: kube.FieldManager}); err != nil {
		return kube.MapError(err)
	}
	return waitContainerRunning(ctx, typed, g.d.Clock, pod.Namespace, pod.Name, name, startTimeout)
}

var errStartTimeout = func(reason string) error {
	msg := "The debug container did not start"
	if reason != "" {
		msg += ": " + reason
	}
	return &apperr.APIError{Status: http.StatusGatewayTimeout, Code: "timeout", Message: msg}
}

// waitContainerRunning watches the pod until the container runs. After `timeout` on the clock it gives up and
// names what the container is waiting for.
func waitContainerRunning(ctx context.Context, typed kubernetes.Interface, clk clock.Clock, ns, pod, container string, timeout time.Duration) error {
	current, err := typed.CoreV1().Pods(ns).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return kube.MapError(err)
	}
	reason := waitingReason(current, container)
	if s := status(current, container); s != nil && s.State.Running != nil {
		return nil
	}
	timer := clk.NewTimer(timeout)
	defer timer.Stop()
	w, err := typed.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + pod, ResourceVersion: current.ResourceVersion})
	if err != nil {
		return kube.MapError(err)
	}
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C():
			return errStartTimeout(reason)
		case ev, ok := <-w.ResultChan():
			if !ok {
				return errStartTimeout(reason)
			}
			if ev.Type == watch.Deleted {
				return fmt.Errorf("the pod was deleted: %w", apperr.ErrNotFound)
			}
			p, isPod := ev.Object.(*corev1.Pod)
			if !isPod {
				continue
			}
			if s := status(p, container); s != nil && s.State.Running != nil {
				return nil
			}
			if r := waitingReason(p, container); r != "" {
				reason = r
			}
		}
	}
}

func waitingReason(pod *corev1.Pod, container string) string {
	if s := status(pod, container); s != nil && s.State.Waiting != nil {
		return s.State.Waiting.Reason
	}
	return ""
}

func ptrTo[T any](v T) *T { return &v }
