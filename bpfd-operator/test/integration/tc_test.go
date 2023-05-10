//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tcCounterBytecodeYaml      = "../../../examples/go-tc-counter/kubernetes-deployment/go-tc-counter-bytecode.yaml"
	tcGoCounterUserspaceYaml   = "../../../examples/go-tc-counter/kubernetes-deployment/go-tc-counter.yaml"
	tcGoCounterUserspaceNs     = "go-tc-counter"
	tcCounterUserspaceDsName   = "go-tc-counter-ds"
)

func TestTcGoCounter(t *testing.T) {
	t.Log("deploying tc counter bpf program")
	require.NoError(t, clusters.ApplyManifestByURL(ctx, env.Cluster(), tcCounterBytecodeYaml))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up tc counter bpfd program")
		return clusters.DeleteManifestByURL(ctx, env.Cluster(), tcCounterBytecodeYaml)
	})

	t.Log("deploying go tc counter userspace daemon")
	require.NoError(t, clusters.ApplyManifestByURL(ctx, env.Cluster(), tcGoCounterUserspaceYaml))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up tc counter userspace daemon")
		return clusters.DeleteManifestByURL(ctx, env.Cluster(), tcGoCounterUserspaceYaml)
	})

	t.Log("waiting for go tc counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(tcGoCounterUserspaceNs).Get(ctx, tcCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	}, time.Minute, time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(tcGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-tc-counter"})
	require.NoError(t, err)
	gotcCounterPod := pods.Items[0]

	req := env.Cluster().Client().CoreV1().Pods(tcGoCounterUserspaceNs).GetLogs(gotcCounterPod.Name, &corev1.PodLogOptions{})

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
	}, 30*time.Second, time.Second)
}
