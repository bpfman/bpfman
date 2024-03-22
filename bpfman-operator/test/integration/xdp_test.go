//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	bpfmanHelpers "github.com/bpfman/bpfman/bpfman-operator/pkg/helpers"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	xdpGoCounterKustomize       = "../../../examples/config/default/go-xdp-counter"
	xdpGoCounterUserspaceNs     = "go-xdp-counter"
	xdpGoCounterUserspaceDsName = "go-xdp-counter-ds"
)

var xdpPassPrivateImageCreds = os.Getenv("XDP_PASS_PRIVATE_IMAGE_CREDS")

func TestXdpPassPrivate(t *testing.T) {
	t.Log("deploying secret for privated xdp bytecode image in the bpfman namespace")
	// Generated from
	/*
		kubectl create secret -n bpfman docker-registry regcred --docker-server=quay.io --docker-username=<USERNAME> --docker-password=<CREDS>
	*/

	if xdpPassPrivateImageCreds == "" {
		t.Log("XDP_PASS_PRIVATE_IMAGE_CREDS must be provided to run this test, skipping")
		t.SkipNow()
	}

	xdpPassPrivateJson, err := base64.StdEncoding.DecodeString(xdpPassPrivateImageCreds)
	require.NoError(t, err)

	xdpPassPrivateSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regcred",
			Namespace: "default",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{".dockerconfigjson": []byte(xdpPassPrivateJson)},
	}

	yamlData, err := yaml.Marshal(xdpPassPrivateSecret)
	require.NoError(t, err)

	require.NoError(t, clusters.ApplyManifestByYAML(ctx, env.Cluster(), string(yamlData)))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up xdp pass private secret")
		return clusters.DeleteManifestByYAML(ctx, env.Cluster(), string(yamlData))
	})

	xdpPassPrivateXdpProgramYAML := `---
---
apiVersion: bpfman.io/v1alpha1
kind: XdpProgram
metadata:
  labels:
    app.kubernetes.io/name: xdpprogram
  name: xdp-pass-private-all-nodes
spec:
  bpffunctionname: pass
  # Select all nodes
  nodeselector: {}
  interfaceselector:
    interfaces:
    - eth0
  priority: 0
  bytecode:
    image:
      imagepullsecret: 
        name: regcred
        namespace: default
      url: quay.io/bpfman-bytecode/xdp_pass_private:latest
`

	t.Log("deploying private xdp pass bpf program")
	require.NoError(t, clusters.ApplyManifestByYAML(ctx, env.Cluster(), xdpPassPrivateXdpProgramYAML))
	addCleanup(func(ctx context.Context) error {
		cleanupLog("cleaning up xdp pass private bpfman program")
		return clusters.DeleteManifestByYAML(ctx, env.Cluster(), xdpPassPrivateXdpProgramYAML)
	})

	// Make sure the bpfProgram was successfully deployed
	require.NoError(t, bpfmanHelpers.WaitForBpfProgConfLoad(bpfmanClient, "xdp-pass-private-all-nodes", time.Duration(time.Second*10), bpfmanHelpers.Xdp))
	t.Log("private xdp pass bpf program successfully deployed")

}

func TestXdpGoCounter(t *testing.T) {
	t.Log("deploying xdp counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), xdpGoCounterKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up xdp counter program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), xdpGoCounterKustomize)
	})

	t.Log("waiting for go xdp counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(xdpGoCounterUserspaceNs).Get(ctx, xdpGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
	// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
	5 * time.Minute, 10 * time.Second)

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
