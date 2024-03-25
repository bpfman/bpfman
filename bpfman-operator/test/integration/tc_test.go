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
	tcGoCounterKustomize       = "../../../examples/config/default/go-tc-counter"
	tcGoCounterUserspaceNs     = "go-tc-counter"
	tcGoCounterUserspaceDsName = "go-tc-counter-ds"
)

func TestTcGoCounter(t *testing.T) {
	t.Log("deploying tc counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), tcGoCounterKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up tc counter program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), tcGoCounterKustomize)
	})

	t.Log("waiting for go tc counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(tcGoCounterUserspaceNs).Get(ctx, tcGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	}, 
	// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
	5 * time.Minute, 10 * time.Second)

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
