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

	bpfdHelpers "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/helpers"
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

func TestXdpPassPrivate(t *testing.T) {
	t.Log("deploying secret for privated xdp bytecode image in the bpfd namespace")
	// Generated from
	/*
		kubectl create secret -n bpfd docker-registry regcred --docker-server=quay.io --docker-username=bpfd-bytecode+bpfdcreds --docker-password=JOGZ3FA6A9L2297JAT4FFN6CJU87LKTIY6X1ZGKWJ0W0XLKY0KPT5YKTBBEAGSF5
	*/
	xdpPassPrivateSecretYAML := `---
---
apiVersion: v1
kind: Secret
metadata:
  name: regcred
  namespace: bpfd
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: eyJhdXRocyI6eyJxdWF5LmlvIjp7InVzZXJuYW1lIjoiYnBmZC1ieXRlY29kZSticGZkY3JlZHMiLCJwYXNzd29yZCI6IkpPR1ozRkE2QTlMMjI5N0pBVDRGRk42Q0pVODdMS1RJWTZYMVpHS1dKMFcwWExLWTBLUFQ1WUtUQkJFQUdTRjUiLCJhdXRoIjoiWW5CbVpDMWllWFJsWTI5a1pTdGljR1prWTNKbFpITTZTazlIV2pOR1FUWkJPVXd5TWprM1NrRlVORVpHVGpaRFNsVTROMHhMVkVsWk5sZ3hXa2RMVjBvd1Z6QllURXRaTUV0UVZEVlpTMVJDUWtWQlIxTkdOUT09In19fQ==
`

	require.NoError(t, clusters.ApplyManifestByYAML(ctx, env.Cluster(), xdpPassPrivateSecretYAML))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up xdp pass private secret")
		return clusters.DeleteManifestByYAML(ctx, env.Cluster(), xdpPassPrivateSecretYAML)
	})

	xdpPassPrivateBpfProgramConfigYAML := `---
---
apiVersion: bpfd.io/v1alpha1
kind: XdpProgram
metadata:
  labels:
    app.kubernetes.io/name: xdpprogram
  name: xdp-pass-private-all-nodes
spec:
  sectionname: pass
  # Select all nodes
  nodeselector: {}
  interfaceselector:
    interface: eth0
  priority: 0
  bytecode:
    image:
      imagepullsecret: regcred
      url: quay.io/bpfd-bytecode/xdp_pass_private:latest
`

	t.Log("deploying private xdp pass bpf program")
	require.NoError(t, clusters.ApplyManifestByYAML(ctx, env.Cluster(), xdpPassPrivateBpfProgramConfigYAML))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up xdp pass private bpfd program")
		return clusters.DeleteManifestByYAML(ctx, env.Cluster(), xdpPassPrivateBpfProgramConfigYAML)
	})

	// Make sure the bpfProgram was successfully deployed
	require.NoError(t, bpfdHelpers.WaitForBpfProgConfLoad(bpfdClient, "xdp-pass-private-all-nodes", time.Duration(time.Second*10), bpfdHelpers.Xdp))
	t.Log("private xdp pass bpf program successfully deployed")

}

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
	}, 30*time.Second, time.Second)
}
