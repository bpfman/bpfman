/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helpers

import (
	"context"
	"fmt"
	"time"

	//bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanclientset "github.com/bpfman/bpfman/bpfman-operator/pkg/client/clientset/versioned"
	//"k8s.io/apimachinery/pkg/api/errors"
	//"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/labels"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
)

// Must match the internal bpfman-api mappings
type ProgramType int32

const (
	Kprobe     ProgramType = 2
	Tc         ProgramType = 3
	Tracepoint ProgramType = 5
	Xdp        ProgramType = 6
)

func (p ProgramType) Uint32() *uint32 {
	progTypeInt := uint32(p)
	return &progTypeInt
}

func FromString(p string) (*ProgramType, error) {
	var programType ProgramType
	switch p {
	case "kprobe":
		programType = Kprobe
	case "tc":
		programType = Tc
	case "xdp":
		programType = Xdp
	case "tracepoint":
		programType = Tracepoint
	default:
		return nil, fmt.Errorf("unknown program type: %s", p)
	}

	return &programType, nil
}

func (p ProgramType) String() string {
	switch p {
	case Kprobe:
		return "kprobe"
	case Tc:
		return "tc"
	case Xdp:
		return "xdp"
	case Tracepoint:
		return "tracepoint"
	default:
		return ""
	}
}

type TcProgramDirection int32

const (
	Ingress TcProgramDirection = 1
	Egress  TcProgramDirection = 2
)

func (t TcProgramDirection) String() string {
	switch t {
	case Ingress:
		return "ingress"
	case Egress:
		return "egress"
	default:
		return ""
	}
}

var log = ctrl.Log.WithName("bpfman-helpers")

// getk8sConfig gets a kubernetes config automatically detecting if it should
// be the in or out of cluster config. If this step fails panic.
func getk8sConfigOrDie() *rest.Config {
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeConfig :=
			clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			panic(err)
		}

		log.Info("Program running from outside of the cluster, picking config from --kubeconfig flag")
	} else {
		log.Info("Program running inside the cluster, picking the in-cluster configuration")
	}

	return config
}

// GetClientOrDie gets the bpfman Kubernetes Client dynamically switching between in cluster and out of
// cluster config setup.
func GetClientOrDie() *bpfmanclientset.Clientset {
	return bpfmanclientset.NewForConfigOrDie(getk8sConfigOrDie())
}

// Returns true if loaded.  False if not.  Also returns the condition type.
func isProgLoaded(conditions *[]metav1.Condition) (bool, string) {
	// Get most recent condition
	conLen := len(*conditions)

	if conLen <= 0 {
		return false, "None"
	}

	condition := (*conditions)[0]

	if condition.Type != string(bpfmaniov1alpha1.ProgramReconcileSuccess) {
		return false, condition.Type
	}

	return true, condition.Type
}

func isKprobebpfmanProgLoaded(c *bpfmanclientset.Clientset, progConfName string) wait.ConditionFunc {
	ctx := context.Background()

	return func() (bool, error) {
		log.Info(".") // progress bar!
		bpfProgConfig, err := c.BpfmanV1alpha1().KprobePrograms().Get(ctx, progConfName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		progLoaded, condType := isProgLoaded(&bpfProgConfig.Status.Conditions)

		if !progLoaded {
			log.Info("kprobProgram: %s not ready with condition: %s, waiting until timeout", progConfName, condType)
			return false, nil
		}

		return true, nil
	}
}

func isTcbpfmanProgLoaded(c *bpfmanclientset.Clientset, progConfName string) wait.ConditionFunc {
	ctx := context.Background()

	return func() (bool, error) {
		log.Info(".") // progress bar!
		bpfProgConfig, err := c.BpfmanV1alpha1().TcPrograms().Get(ctx, progConfName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		progLoaded, condType := isProgLoaded(&bpfProgConfig.Status.Conditions)

		if !progLoaded {
			log.Info("tcProgram: %s not ready with condition: %s, waiting until timeout", progConfName, condType)
			return false, nil
		}

		return true, nil
	}
}

func isTracepointbpfmanProgLoaded(c *bpfmanclientset.Clientset, progConfName string) wait.ConditionFunc {
	ctx := context.Background()

	return func() (bool, error) {
		log.Info(".") // progress bar!
		bpfProgConfig, err := c.BpfmanV1alpha1().TracepointPrograms().Get(ctx, progConfName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		progLoaded, condType := isProgLoaded(&bpfProgConfig.Status.Conditions)

		if !progLoaded {
			log.Info("tracepointProgram: %s not ready with condition: %s, waiting until timeout", progConfName, condType)
			return false, nil
		}

		return true, nil
	}
}

func isXdpbpfmanProgLoaded(c *bpfmanclientset.Clientset, progConfName string) wait.ConditionFunc {
	ctx := context.Background()

	return func() (bool, error) {
		log.Info(".") // progress bar!
		bpfProgConfig, err := c.BpfmanV1alpha1().XdpPrograms().Get(ctx, progConfName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		progLoaded, condType := isProgLoaded(&bpfProgConfig.Status.Conditions)

		if !progLoaded {
			log.Info("xdpProgram: %s not ready with condition: %s, waiting until timeout", progConfName, condType)
			return false, nil
		}

		return true, nil
	}
}

// WaitForBpfProgConfLoad ensures the Program object is loaded and deployed successfully, specifically
// it checks the config objects' conditions to look for the `Loaded` state.
func WaitForBpfProgConfLoad(c *bpfmanclientset.Clientset, progName string, timeout time.Duration, progType ProgramType) error {
	switch progType {
	case Kprobe:
		return wait.PollImmediate(time.Second, timeout, isKprobebpfmanProgLoaded(c, progName))
	case Tc:
		return wait.PollImmediate(time.Second, timeout, isTcbpfmanProgLoaded(c, progName))
	case Xdp:
		return wait.PollImmediate(time.Second, timeout, isXdpbpfmanProgLoaded(c, progName))
	case Tracepoint:
		return wait.PollImmediate(time.Second, timeout, isTracepointbpfmanProgLoaded(c, progName))
	// TODO: case Uprobe: not covered.  Since Uprobe has the same ProgramType as
	// Kprobe, we need a different way to distinguish them.  Options include
	// creating an internal ProgramType for Uprobe or using a different
	// identifier such as the string representation of the program type.
	default:
		return fmt.Errorf("unknown bpf program type: %s", progType)
	}
}

// IsBpfmanDeployed is used to check for the existence of bpfman in a Kubernetes cluster. Specifically it checks for
// the existence of the bpfman.io CRD api group within the apiserver. If getting the k8s config fails this will panic.
func IsBpfmanDeployed() bool {
	config := getk8sConfigOrDie()

	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		panic(err)
	}

	apiList, err := client.ServerGroups()
	if err != nil {
		log.Info("issue occurred while fetching ServerGroups")
		panic(err)
	}

	for _, v := range apiList.Groups {
		if v.Name == "bpfman.io" {

			log.Info("bpfman.io found in apis, bpfman is deployed")
			return true
		}
	}
	return false
}

func IsBpfProgramConditionFailure(conditions *[]metav1.Condition) bool {
	if conditions == nil || *conditions == nil || len(*conditions) == 0 {
		return true
	}

	numConditions := len(*conditions)

	if numConditions > 1 {
		// We should only ever have one condition so log a message, but
		// still look at (*conditions)[0].
		log.Info("more than one BpfProgramCondition", "numConditions", numConditions)
	}

	if (*conditions)[0].Type == string(bpfmaniov1alpha1.BpfProgCondNotLoaded) ||
		(*conditions)[0].Type == string(bpfmaniov1alpha1.BpfProgCondNotUnloaded) ||
		(*conditions)[0].Type == string(bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound) ||
		(*conditions)[0].Type == string(bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded) ||
		(*conditions)[0].Type == string(bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError) {
		return true
	}

	return false
}
