//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
	"strings"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	xdpCounterBytecodeYaml      = "../../../examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter-bytecode.yaml"
	xdpGoCounterUserspaceYaml   = "../../../examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter.yaml"
	xdpGoCounterUserspaceNs     = "go-xdp-counter"
	xdpGoCounterUserspaceDsName = "go-xdp-counter-ds"
)

func TestXdpGoCounter(t *testing.T) {
	t.Log("deploying xdp counter bpf program")
	require.NoError(t, clusters.ApplyManifestByURL(ctx, env.Cluster(), xdpCounterBytecodeYaml))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up xdp counter bpfd program")
		return clusters.DeleteManifestByURL(ctx, env.Cluster(), xdpCounterBytecodeYaml)
	})

	t.Log("deploying go xdp counter userspace daemon")
	require.NoError(t, clusters.ApplyManifestByURL(ctx, env.Cluster(), xdpGoCounterUserspaceYaml))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up xdp counter userspace daemon")
		return clusters.DeleteManifestByURL(ctx, env.Cluster(), xdpGoCounterUserspaceYaml)
	})

	t.Log("waiting for go xdp counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(xdpGoCounterUserspaceNs).Get(ctx, xdpGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	}, time.Minute, time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(xdpGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-xdp-counter"})
	require.NoError(t, err)
	goXdpCounterPod := pods.Items[0]

	req := env.Cluster().Client().CoreV1().Pods(xdpGoCounterUserspaceNs).GetLogs(goXdpCounterPod.Name, &corev1.PodLogOptions{})

	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())
		if strings.Contains(output.String(), "packets received") && strings.Contains(output.String(), "bytes received") { 
			return true
		}
		return false
	}, 30 * time.Second, time.Second)
}
