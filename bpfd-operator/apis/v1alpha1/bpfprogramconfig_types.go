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

// All fields are required unless explicitly marked optional
// +kubebuilder:validation:Required
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// BpfProgramConfig is the Schema for the Bpfprogramconfigs API
type BpfProgramConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BpfProgramConfigSpec `json:"spec"`
	// +optional
	Status BpfProgramConfigStatus `json:"status,omitempty"`
}

// BpfProgramConfigSpec defines the desired state of BpfProgramConfig
type BpfProgramConfigSpec struct {
	// ProgramName specifies the name of the bpf program to be loaded.
	Name string `json:"name"`

	// Type specifies the bpf program type.
	Type string `json:"type"`

	// NodeSelector allows the user to specify which nodes to deploy the
	// bpf program to.  This field must be specified, to select all nodes
	// use standard metav1.LabelSelector semantics and make it empty.
	NodeSelector metav1.LabelSelector `json:"nodeselector"`

	// AttachPoint describes the kernel attach point for the Bpf program
	// if there is one. Attach points usually correspond to program type
	// in some way.
	AttachPoint BpfProgramAttachPoint `json:"attachpoint"`

	// Bytecode configures where the bpf program's bytecode should be loaded
	// from. It is a standard URL (RFC-1738) which currently only supports
	// local files (file:///<local path>) or container registry links
	// (image://<container image URL>)
	// +kubebuilder:validation:Pattern=`(file|image)\:(\/){1,3}[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)`
	ByteCode string `json:"bytecode"`
}

// BpfProgramConfigStatus defines the observed state of BpfProgramConfig
type BpfProgramConfigStatus struct {
	// Conditions houses the global cluster state for the BpfProgram
	// Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true

// BpfProgramConfigList contains a list of BpfProgramConfig
type BpfProgramConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfProgramConfig `json:"items"`
}
