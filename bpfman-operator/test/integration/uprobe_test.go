//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	uprobeGoCounterKustomize       = "../../../examples/config/default/go-uprobe-counter"
	uprobeGoCounterUserspaceNs     = "go-uprobe-counter"
	uprobeGoCounterUserspaceDsName = "go-uprobe-counter-ds"
	targetKustomize                = "../../../examples/config/default/go-target"
	targetUserspaceNs              = "go-target"
	targetUserspaceDsName          = "go-target-ds"
)

func TestUprobeGoCounter(t *testing.T) {
	t.Log("deploying target for uprobe counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), targetKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up target program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), targetKustomize)
	})

	t.Log("waiting for go target userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(targetUserspaceNs).Get(ctx, targetUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
	// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
	5*time.Minute, 10*time.Second)

	t.Log("deploying uprobe counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), uprobeGoCounterKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up uprobe counter program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), uprobeGoCounterKustomize)
	})

	t.Log("waiting for go uprobe counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(uprobeGoCounterUserspaceNs).Get(ctx, uprobeGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(uprobeGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-uprobe-counter"})
	require.NoError(t, err)
	goUprobeCounterPod := pods.Items[0]

	want := regexp.MustCompile(`Uprobe count: ([0-9]+)`)
	req := env.Cluster().Client().CoreV1().Pods(uprobeGoCounterUserspaceNs).GetLogs(goUprobeCounterPod.Name, &corev1.PodLogOptions{})
	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())

		matches := want.FindAllStringSubmatch(output.String(), -1)
		if len(matches) >= 1 && len(matches[0]) >= 2 {
			count, err := strconv.Atoi(matches[0][1])
			require.NoError(t, err)
			if count > 0 {
				t.Logf("counted %d uprobe executions so far, BPF program is functioning", count)
				return true
			}
		}
		return false
	}, 30*time.Second, time.Second)
}
