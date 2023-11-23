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

// BpfProgram is the Schema for the Bpfprograms API
type BpfProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BpfProgramSpec `json:"spec"`
	// +optional
	Status BpfProgramStatus `json:"status,omitempty"`
}

// BpfProgramSpec defines the desired state of BpfProgram
type BpfProgramSpec struct {
	// Type specifies the bpf program type
	// +optional
	Type string `json:"type,omitempty"`
}

// BpfProgramStatus defines the observed state of BpfProgram
// TODO Make these a fixed set of metav1.Condition.types and metav1.Condition.reasons
type BpfProgramStatus struct {
	// Conditions houses the updates regarding the actual implementation of
	// the bpf program on the node
	// Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true

// BpfProgramList contains a list of BpfProgram
type BpfProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfProgram `json:"items"`
}
