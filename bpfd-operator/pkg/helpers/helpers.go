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
	"os"
	"time"

	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdclientset "github.com/redhat-et/bpfd/bpfd-operator/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	bpfdoperator "github.com/redhat-et/bpfd/bpfd-operator/controllers/bpfd-operator"
)

const (
	DefaultMapDir = "/run/bpfd/fs/maps"
)

type ProgType string

const (
	Tc         ProgType = "TC"
	Xdp        ProgType = "XDP"
	TracePoint ProgType = "TRACEPOINT"
)

var log = ctrl.Log.WithName("bpfd-helpers")

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

// GetClientOrDie gets the bpfd Kubernetes Client dynamically switching between in cluster and out of
// cluster config setup.
func GetClientOrDie() *bpfdclientset.Clientset {
	return bpfdclientset.NewForConfigOrDie(getk8sConfigOrDie())
}

// GetMaps is meant to be used by applications wishing to use BPFD. It takes in a bpf program
// name and a list of map names, and returns a map corelating map name to map pin path.
func GetMaps(c *bpfdclientset.Clientset, bpfProgramConfigName string, mapNames []string) (map[string]string, error) {
	bpfMaps := map[string]string{}
	ctx := context.Background()

	// Get the nodename where this pod is running
	nodeName := os.Getenv("NODENAME")
	if nodeName == "" {
		return nil, fmt.Errorf("NODENAME env var not set")
	}
	bpfProgramName := bpfProgramConfigName + "-" + nodeName

	bpfProgram, err := c.BpfdV1alpha1().BpfPrograms().Get(ctx, bpfProgramName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting BpfProgram %s: %v", bpfProgramName, err)
	}

	// TODO (astoycos) This doesn't support multiple programs in a single bpfProgram Object yet
	for _, v := range bpfProgram.Spec.Programs {
		for _, mapName := range mapNames {

			if pinPath, ok := v.Maps[mapName]; !ok {
				return nil, fmt.Errorf("map: %s not found", mapName)
			} else {
				bpfMaps[mapName] = pinPath
			}

		}
	}

	return bpfMaps, nil
}

// NewBpfProgramConfig is a good starting point for operators who want to deploy and
// manage bpfProgramConfig objects programatically.  It takes in the desired program
// name and type returning a skeleton bpfProgram Object for further use.
func NewBpfProgramConfig(name string, progType ProgType) *bpfdiov1alpha1.BpfProgramConfig {
	switch progType {
	case Xdp, Tc:
		return &bpfdiov1alpha1.BpfProgramConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: bpfdiov1alpha1.BpfProgramConfigSpec{
				Type: string(progType),
				AttachPoint: bpfdiov1alpha1.BpfProgramAttachPoint{
					NetworkMultiAttach: &bpfdiov1alpha1.BpfNetworkMultiAttach{},
				},
			},
		}
	case TracePoint:
		return &bpfdiov1alpha1.BpfProgramConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: bpfdiov1alpha1.BpfProgramConfigSpec{
				Type: string(progType),
				AttachPoint: bpfdiov1alpha1.BpfProgramAttachPoint{
					SingleAttach: &bpfdiov1alpha1.BpfSingleAttach{},
				},
			},
		}
	default:
		return nil
	}
}

// CreateOrUpdateOwnedBpfProgConf creates or updates a bpfProgramConfig object while also setting the owner reference to
// another Kubernetes core object or CRD.
func CreateOrUpdateOwnedBpfProgConf(c *bpfdclientset.Clientset, progConfig *bpfdiov1alpha1.BpfProgramConfig, owner client.Object, ownerScheme *runtime.Scheme) error {
	progName := progConfig.GetName()
	ctx := context.Background()

	err := ctrl.SetControllerReference(owner, progConfig, ownerScheme)
	if err != nil {
		log.Error(err, "Failed to set controller reference")
		return err
	}

	progConfigExisting, err := c.BpfdV1alpha1().BpfProgramConfigs().Get(ctx, progName, metav1.GetOptions{})
	if err != nil {
		// Create if not found
		if errors.IsNotFound(err) {
			_, err = c.BpfdV1alpha1().BpfProgramConfigs().Create(ctx, progConfig, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("error creating BpfProgramConfig %s: %v", progName, err)
			}

			return nil
		}
		return fmt.Errorf("error getting BpfProgramConfig %s: %v", progName, err)
	}

	progConfig.SetResourceVersion(progConfigExisting.GetResourceVersion())

	// Update if already exists
	_, err = c.BpfdV1alpha1().BpfProgramConfigs().Update(ctx, progConfig, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating BpfProgramConfig %s: %v", progName, err)
	}

	return nil
}

// CreateOrUpdateBpfProgConf creates or updates a bpfProgramConfig object.
func CreateOrUpdateBpfProgConf(c *bpfdclientset.Clientset, progConfig *bpfdiov1alpha1.BpfProgramConfig) error {
	progName := progConfig.GetName()
	ctx := context.Background()

	progConfigExisting, err := c.BpfdV1alpha1().BpfProgramConfigs().Get(ctx, progName, metav1.GetOptions{})
	if err != nil {
		// Create if not found
		if errors.IsNotFound(err) {
			_, err = c.BpfdV1alpha1().BpfProgramConfigs().Create(ctx, progConfig, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("error creating BpfProgramConfig %s: %v", progName, err)
			}

			return nil
		}
		return fmt.Errorf("error getting BpfProgramConfig %s: %v", progName, err)
	}

	progConfig.SetResourceVersion(progConfigExisting.GetResourceVersion())

	// Update if already exists
	_, err = c.BpfdV1alpha1().BpfProgramConfigs().Update(ctx, progConfig, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating BpfProgramConfig %s: %v", progName, err)
	}

	return nil
}

// DeleteBpfProgramConf deletes a given bpfProgramConfig object based on name.
func DeleteBpfProgConf(c *bpfdclientset.Clientset, progName string) error {
	ctx := context.Background()

	err := c.BpfdV1alpha1().BpfProgramConfigs().Delete(ctx, progName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("error Deleting BpfProgramConfig %s: %v", progName, err)
	}

	return nil
}

// DeleteBpfProgConfLabels deletes a single or set of bpfProgramConfig object based
// on a labelSelector.
func DeleteBpfProgConfLabels(c *bpfdclientset.Clientset, selector *metav1.LabelSelector) error {
	ctx := context.Background()

	err := c.BpfdV1alpha1().BpfProgramConfigs().DeleteCollection(ctx, metav1.DeleteOptions{},
		metav1.ListOptions{
			LabelSelector: labels.Set(selector.MatchLabels).String(),
		})
	if err != nil {
		return fmt.Errorf("error Deleting BpfProgramConfigs for labels %s: %v", labels.Set(selector.MatchLabels).String(), err)
	}

	return nil
}

func isbpfdProgConfLoaded(c *bpfdclientset.Clientset, progConfName string) wait.ConditionFunc {
	ctx := context.Background()

	return func() (bool, error) {
		log.Info(".") // progress bar!

		bpfProgConfig, err := c.BpfdV1alpha1().BpfProgramConfigs().Get(ctx, progConfName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		// Get most recent condition
		conLen := len(bpfProgConfig.Status.Conditions)

		if conLen <= 0 {
			return false, nil
		}

		recentIdx := len(bpfProgConfig.Status.Conditions) - 1

		condition := bpfProgConfig.Status.Conditions[recentIdx]

		if condition.Type != string(bpfdoperator.BpfProgConfigReconcileSuccess) {
			return false, fmt.Errorf("BpfProgramConfig: %s not ready with condition: %s", progConfName, condition.Type)
		}

		return true, nil
	}
}

// WaitForBpfProgConfLoad ensures the bpfProgramConfig object is loaded and deployed successfully, specifically
// it checks the config objects' conditions to look for the `Loaded` state.
func WaitForBpfProgConfLoad(c *bpfdclientset.Clientset, progName string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, isbpfdProgConfLoaded(c, progName))
}

// IsBpfdDeployed is used to check for the existence of bpfd in a Kubernetes cluster. Specifically it checks for
// the existence of the bpfd.io CRD api group within the apiserver. If getting the k8s config fails this will panic.
func IsBpfdDeployed() bool {
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
		if v.Name == "bpfd.io" {

			log.Info("bpfd.io found in apis, bpfd is deployed")
			return true
		}
	}
	return false
}
