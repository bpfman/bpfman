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

// EbpfProgramConfigSpec defines the desired state of EbpfProgramConfig
type EbpfProgramConfigSpec struct {
	// ProgramName specifies the name of the bpf program to be loaded.
	Name string `json:"name"`

	// Type specifies the bpf program type.
	Type string `json:"type"`

	// NodeSelector allows the user to specify which nodes to deploy the
	// bpf program to.  This field must be specified, to select all nodes
	// use standard metav1.LabelSelector semantics and make it empty.
	NodeSelector metav1.LabelSelector `json:"nodeselector"`

	// Priority specifies the priority of the bpf program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// AttachPoint describes the kernel attach point for the ebpf program
	// if there is one. Attach points usually correspond to program type
	// in some way.
	AttachPoint EbpfProgramAttachPoint `json:"attachpoint"`

	// Bytecode configures where the bpf program's bytecode should be loaded
	// from.
	ByteCode ByteCodeSource `json:"bytecode"`
}

// ByteCodeSource describes the location of the bytecode for the ebpf program.
// Exactly one of the sources must be specified.
// +kubebuilder:validation:MaxProperties=1
// +kubebuilder:validation:MinProperties=1
type ByteCodeSource struct {
	// ImageUrl specifies the bytecode image to pull and use.
	// +optional
	ImageUrl *string `json:"imageurl,omitempty"`

	// Path specifies the host bound directory where the bytecode lives on each
	// node where the program is to be deployed.
	// +optional
	Path *string `json:"path,omitempty"`
}

// EbpfProgramConfigStatus defines the observed state of EbpfProgramConfig
type EbpfProgramConfigStatus struct {
	// Conditions houses the global cluster state for the ebpfProgram
	// Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// EbpfProgramConfig is the Schema for the ebpfprogramconfigs API
type EbpfProgramConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec EbpfProgramConfigSpec `json:"spec"`
	// +optional
	Status EbpfProgramConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// EbpfProgramConfigList contains a list of EbpfProgramConfig
type EbpfProgramConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EbpfProgramConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EbpfProgramConfig{}, &EbpfProgramConfigList{})
}
