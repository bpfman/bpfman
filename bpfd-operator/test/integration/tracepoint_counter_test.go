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
	tracepointCounterBytecodeYaml      = "../../../examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter-bytecode.yaml"
	tracepointGoCounterUserspaceYaml   = "../../../examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter.yaml"
	tracepointGoCounterUserspaceNs     = "go-tracepoint-counter"
	tracepointGoCounterUserspaceDsName = "go-tracepoint-counter-ds"
)

func TestTracepointGoCounter(t *testing.T) {
	t.Log("deploying tracepoint counter bpf program")
	require.NoError(t, clusters.ApplyManifestByURL(ctx, env.Cluster(), tracepointCounterBytecodeYaml))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up tracepoint counter bpfd program")
		return clusters.DeleteManifestByURL(ctx, env.Cluster(), tracepointCounterBytecodeYaml)
	})

	t.Log("deploying go tracepoint counter userspace daemon")
	require.NoError(t, clusters.ApplyManifestByURL(ctx, env.Cluster(), tracepointGoCounterUserspaceYaml))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up tracepoint counter userspace daemon")
		return clusters.DeleteManifestByURL(ctx, env.Cluster(), tracepointGoCounterUserspaceYaml)
	})

	t.Log("waiting for go tracepoint counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(tracepointGoCounterUserspaceNs).Get(ctx, tracepointGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	}, time.Minute, time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(tracepointGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-tracepoint-counter"})
	require.NoError(t, err)
	goTracepointCounterPod := pods.Items[0]

	want := regexp.MustCompile(`([0-9]+) SIGUSR1 signals seen`)
	req := env.Cluster().Client().CoreV1().Pods(tracepointGoCounterUserspaceNs).GetLogs(goTracepointCounterPod.Name, &corev1.PodLogOptions{})
	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())

		matches := want.FindAllStringSubmatch(output.String(), -1)
		if len(matches) == 1 && len(matches[0]) == 2 {
			count, err := strconv.Atoi(matches[0][1])
			require.NoError(t, err)
			if count > 0 {
				t.Logf("counted %d SIGUSR1 signals so far, BPF program is functioning", count)
				return true
			}
		}
		return false
	}, 30*time.Second, time.Second)
}
