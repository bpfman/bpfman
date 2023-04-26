/*
Copyright 2022.

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

package bpfdclient

import (
	"io"
	"os"

	"github.com/redhat-et/bpfd/bpfd-operator/internal"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func LoadAndConfigureBpfdDs(config *corev1.ConfigMap) *appsv1.DaemonSet {
	// Load static bpfd deployment from disk
	file, err := os.Open(internal.BpfdDaemonManifestPath)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, nil)

	staticBpfdDeployment := obj.(*appsv1.DaemonSet)

	// Runtime Configurable fields
	bpfdNamespace := config.Data["bpfd.namespace"]
	bpfdImage := config.Data["bpfd.image"]
	bpfdAgentImage := config.Data["bpfd.agent.image"]
	bpfdLogLevel := config.Data["bpfd.log.level"]

	// Annotate the log level on the ds so we get automatic restarts on changes.
	if staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations == nil {
		staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}
	staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations["bpfd.io.bpfd.loglevel"] = bpfdLogLevel
	staticBpfdDeployment.Name = "bpfd-daemon"
	staticBpfdDeployment.Namespace = bpfdNamespace
	staticBpfdDeployment.Spec.Template.Spec.Containers[0].Image = bpfdImage
	staticBpfdDeployment.Spec.Template.Spec.Containers[1].Image = bpfdAgentImage
	controllerutil.AddFinalizer(staticBpfdDeployment, "bpfd.io.operator/finalizer")

	return staticBpfdDeployment
}
