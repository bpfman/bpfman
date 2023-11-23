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

// KprobeProgram is the Schema for the KprobePrograms API
// +kubebuilder:printcolumn:name="BpfFunctionName",type=string,JSONPath=`.spec.bpffunctionname`
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`,priority=1
// +kubebuilder:printcolumn:name="Offset",type=integer,JSONPath=`.spec.offset`,priority=1
// +kubebuilder:printcolumn:name="RetProbe",type=boolean,JSONPath=`.spec.retprobe`,priority=1
type KprobeProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KprobeProgramSpec `json:"spec"`
	// +optional
	Status KprobeProgramStatus `json:"status,omitempty"`
}

// KprobeProgramSpec defines the desired state of KprobeProgram
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`
// +kubebuilder:printcolumn:name="Offset",type=integer,JSONPath=`.spec.offset`
// +kubebuilder:printcolumn:name="RetProbe",type=boolean,JSONPath=`.spec.retprobe`
// +kubebuilder:validation:XValidation:message="offset cannot be set for kretprobes",rule="self.retprobe == false || self.offset == 0"
type KprobeProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Functions to attach the kprobe to.
	FunctionNames []string `json:"func_names"`

	// Offset added to the address of the function for kprobe.
	// Not allowed for kretprobes.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// Whether the program is a kretprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	RetProbe bool `json:"retprobe"`

	// // Namespace to attach the uprobe in. (Not supported yet by bpfman.)
	// // +optional
	// Namespace string `json:"namespace"`
}

// KprobeProgramStatus defines the observed state of KprobeProgram
type KprobeProgramStatus struct {
	// Conditions houses the global cluster state for the KprobeProgram. The explicit
	// condition types are defined internally.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// KprobeProgramList contains a list of KprobePrograms
type KprobeProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KprobeProgram `json:"items"`
}
