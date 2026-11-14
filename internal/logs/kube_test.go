package logs_test

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func newFakeClients() *fakekube.Clients {
	fk := fakekube.New()
	_, _ = fk.Kube.CoreV1().Pods("ns").Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "ns"}}, metav1.CreateOptions{})
	return fk
}
