//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/loadimage"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/certmanager"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/redhat-et/bpfd/bpfd-operator/internal"
)

var (
	ctx    context.Context
	cancel context.CancelFunc
	env    environments.Environment

	bpfdImage = os.Getenv("BPFD_IMG")
	bpfdAgentImage  = os.Getenv("BPFD_AGENT_IMG")
	bpfdOperatorImage  = os.Getenv("BPFD_OPERATOR_IMG")
	certmanagerVersionStr = os.Getenv("CERTMANAGER_VERSION")

	existingCluster      = os.Getenv("USE_EXISTING_KIND_CLUSTER")
	keepTestCluster      = func() bool { return os.Getenv("TEST_KEEP_CLUSTER") == "true" || existingCluster != "" }()
	// TODO (astoycos) add this back after fixing bpfd-operator deletion issues
	//keepKustomizeDeploys = func() bool { return os.Getenv("TEST_KEEP_KUSTOMIZE_DEPLOYS") == "true" }()

	cleanup = []func(context.Context) error{}
)

const (
	bpfdKustomize = "../../config/default"
)

func TestMain(m *testing.M) {
	// check that we have the bpfd, bpfd-agent, and bpfd-operator images to use for the tests.
	// generally the runner of the tests should have built these from the latest
	// changes prior to the tests and fed them to the test suite.
	if bpfdImage == "" || bpfdAgentImage == "" || bpfdOperatorImage == ""  {
		exitOnErr(fmt.Errorf("BPFD_IMG, BPFD_AGENT_IMG, and BPFD_OPERATOR_IMG must be provided"))
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	if existingCluster != "" {
		fmt.Printf("INFO: existing kind cluster %s was provided\n", existingCluster)

		// if an existing cluster was provided, build a test env out of that instead
		cluster, err := kind.NewFromExisting(existingCluster)
		exitOnErr(err)
		env, err = environments.NewBuilder().WithExistingCluster(cluster).Build(ctx)
		exitOnErr(err)
	} else {
		fmt.Println("INFO: creating a new kind cluster")

		// to use the provided bpfd, bpfd-agent, and bpfd-operator images we will need to add
		// them as images to load in the test cluster via an addon.
		loadImages, err := loadimage.NewBuilder().WithImage(bpfdImage)
		exitOnErr(err)
		loadImages, err = loadImages.WithImage(bpfdAgentImage)
		exitOnErr(err)
		loadImages, err = loadImages.WithImage(bpfdOperatorImage)
		exitOnErr(err)

		certManagerBuilder := certmanager.NewBuilder()

		if len(certmanagerVersionStr) != 0 { 
			fmt.Printf("INFO: a specific version of certmanager was requested: %s\n", certmanagerVersionStr)
			certmanagerVersion, err := semver.ParseTolerant(certmanagerVersionStr)
			exitOnErr(err)
			certManagerBuilder.WithVersion(certmanagerVersion)
		}

		// create the testing environment and cluster
		env, err = environments.NewBuilder().WithAddons(certManagerBuilder.Build(),loadImages.Build()).Build(ctx)
		exitOnErr(err)

		if !keepTestCluster {
			addCleanup(func(context.Context) error {
				cleanupLog("cleaning up test environment and cluster %s\n", env.Cluster().Name())
				return env.Cleanup(ctx)
			})
		}

		fmt.Printf("INFO: new kind cluster %s was created\n", env.Cluster().Name())

		// deploy the BPFD Operator and revelevant CRDs
		fmt.Println("INFO: deploying bpfd operator to test cluster")
		exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), bpfdKustomize))
		// TODO (astoycos) add this back after fixing bpfd-operator deletion issues
		// if !keepKustomizeDeploys {
		// 	addCleanup(func(context.Context) error {
		// 		cleanupLog("cleaning up bpfd operator")
		// 		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), bpfdKustomize)
		// 	})
		// }
	}

	exitOnErr(waitForBpfdReadiness(ctx, env))

	exit := m.Run()

	exitOnErr(runCleanup())

	os.Exit(exit)
}

func exitOnErr(err error) {
	if err == nil {
		return
	}

	if cleanupErr := runCleanup(); cleanupErr != nil {
		err = fmt.Errorf("%s; %w", err, cleanupErr)
	}

	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func addCleanup(job func(context.Context) error) {
	// prepend so that cleanup runs in reverse order
	cleanup = append([]func(context.Context) error{job}, cleanup...)
}

func cleanupLog(msg string, args ...any) {
	fmt.Printf(fmt.Sprintf("INFO: %s\n", msg), args...)
}

func runCleanup() (cleanupErr error) {
	if len(cleanup) < 1 {
		return
	}

	fmt.Println("INFO: running cleanup jobs")
	for _, job := range cleanup {
		if err := job(ctx); err != nil {
			cleanupErr = fmt.Errorf("%s; %w", err, cleanupErr)
		}
	}
	cleanup = nil
	return
}

func waitForBpfdReadiness(ctx context.Context, env environments.Environment) error {
	for {
		time.Sleep(2 * time.Second)
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context completed while waiting for components: %w", err)
			}
			return fmt.Errorf("context completed while waiting for components")
		default:
			fmt.Println("INFO: waiting for bpfd")
			var controlplaneReady, dataplaneReady bool

			controlplane, err := env.Cluster().Client().AppsV1().Deployments(internal.BpfdNs).Get(ctx, internal.BpfdOperatorName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) { 
					fmt.Println("INFO: bpfd-operator dep not found yet")
					continue
				}
				return err
			}
			if controlplane.Status.AvailableReplicas > 0 {
				controlplaneReady = true
			}

			dataplane, err := env.Cluster().Client().AppsV1().DaemonSets(internal.BpfdNs).Get(ctx, internal.BpfdDsName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) { 
					fmt.Println("INFO: bpfd daemon not found yet")
					continue
				}
				return err
			}
			if dataplane.Status.NumberAvailable > 0 {
				dataplaneReady = true
			}

			if controlplaneReady && dataplaneReady {
				fmt.Println("INFO: bpfd-operator is ready")
				return nil
			}
		}
	}
}
