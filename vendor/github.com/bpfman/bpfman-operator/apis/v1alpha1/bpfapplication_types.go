/*
Copyright 2023 The bpfman Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EBPFProgType defines the supported eBPF program types
type EBPFProgType string

const (
	// ProgTypeXDP refers to the XDP program type.
	ProgTypeXDP EBPFProgType = "XDP"

	// ProgTypeTC refers to the TC program type.
	ProgTypeTC EBPFProgType = "TC"

	// ProgTypeTCX refers to the TCx program type.
	ProgTypeTCX EBPFProgType = "TCX"

	// ProgTypeFentry refers to the Fentry program type.
	ProgTypeFentry EBPFProgType = "Fentry"

	// ProgTypeFexit refers to the Fexit program type.
	ProgTypeFexit EBPFProgType = "Fexit"

	// ProgTypeKprobe refers to the Kprobe program type.
	ProgTypeKprobe EBPFProgType = "Kprobe"

	// ProgTypeKretprobe refers to the Kprobe program type.
	ProgTypeKretprobe EBPFProgType = "Kretprobe"

	// ProgTypeUprobe refers to the Uprobe program type.
	ProgTypeUprobe EBPFProgType = "Uprobe"

	// ProgTypeUretprobe refers to the Uretprobe program type.
	ProgTypeUretprobe EBPFProgType = "Uretprobe"

	// ProgTypeTracepoint refers to the Tracepoint program type.
	ProgTypeTracepoint EBPFProgType = "Tracepoint"
)

// BpfApplicationProgram defines the desired state of BpfApplication
// +union
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'XDP' ?  has(self.xdp) : !has(self.xdp)",message="xdp configuration is required when type is XDP, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TC' ?  has(self.tc) : !has(self.tc)",message="tc configuration is required when type is TC, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TCX' ?  has(self.tcx) : !has(self.tcx)",message="tcx configuration is required when type is TCX, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Fentry' ?  has(self.fentry) : !has(self.fentry)",message="fentry configuration is required when type is Fentry, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Fexit' ?  has(self.fexit) : !has(self.fexit)",message="fexit configuration is required when type is Fexit, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Kprobe' ?  has(self.kprobe) : !has(self.kprobe)",message="kprobe configuration is required when type is Kprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Kretprobe' ?  has(self.kretprobe) : !has(self.kretprobe)",message="kretprobe configuration is required when type is Kretprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Uprobe' ?  has(self.uprobe) : !has(self.uprobe)",message="uprobe configuration is required when type is Uprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Uretprobe' ?  has(self.uretprobe) : !has(self.uretprobe)",message="uretprobe configuration is required when type is Uretprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Tracepoint' ?  has(self.tracepoint) : !has(self.tracepoint)",message="tracepoint configuration is required when type is Tracepoint, and forbidden otherwise"
type BpfApplicationProgram struct {
	// Type specifies the bpf program type
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:="XDP";"TC";"TCX";"Fentry";"Fexit";"Kprobe";"Kretprobe";"Uprobe";"Uretprobe";"Tracepoint"
	Type EBPFProgType `json:"type,omitempty"`

	// xdp defines the desired state of the application's XdpPrograms.
	// +unionMember
	// +optional
	XDP *XdpProgramInfo `json:"xdp,omitempty"`

	// tc defines the desired state of the application's TcPrograms.
	// +unionMember
	// +optional
	TC *TcProgramInfo `json:"tc,omitempty"`

	// tcx defines the desired state of the application's TcPrograms.
	// +unionMember
	// +optional
	TCX *TcProgramInfo `json:"tcx,omitempty"`

	// fentry defines the desired state of the application's FentryPrograms.
	// +unionMember
	// +optional
	Fentry *FentryProgramInfo `json:"fentry,omitempty"`

	// fexit defines the desired state of the application's FexitPrograms.
	// +unionMember
	// +optional
	Fexit *FexitProgramInfo `json:"fexit,omitempty"`

	// kprobe defines the desired state of the application's KprobePrograms.
	// +unionMember
	// +optional
	Kprobe *KprobeProgramInfo `json:"kprobe,omitempty"`

	// kretprobe defines the desired state of the application's KretprobePrograms.
	// +unionMember
	// +optional
	Kretprobe *KprobeProgramInfo `json:"kretprobe,omitempty"`

	// uprobe defines the desired state of the application's UprobePrograms.
	// +unionMember
	// +optional
	Uprobe *UprobeProgramInfo `json:"uprobe,omitempty"`

	// uretprobe defines the desired state of the application's UretprobePrograms.
	// +unionMember
	// +optional
	Uretprobe *UprobeProgramInfo `json:"uretprobe,omitempty"`

	// tracepoint defines the desired state of the application's TracepointPrograms.
	// +unionMember
	// +optional
	Tracepoint *TracepointProgramInfo `json:"tracepoint,omitempty"`
}

// BpfApplicationSpec defines the desired state of BpfApplication
type BpfApplicationSpec struct {
	BpfAppCommon `json:",inline"`

	// Programs is a list of bpf programs supported for a specific application.
	// It's possible that the application can selectively choose which program(s)
	// to run from this list.
	// +kubebuilder:validation:MinItems:=1
	Programs []BpfApplicationProgram `json:"programs,omitempty"`
}

// BpfApplicationStatus defines the observed state of BpfApplication
type BpfApplicationStatus struct {
	BpfProgramStatusCommon `json:",inline"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// BpfApplication is the Schema for the bpfapplications API
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type BpfApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BpfApplicationSpec   `json:"spec,omitempty"`
	Status BpfApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// BpfApplicationList contains a list of BpfApplications
type BpfApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfApplication `json:"items"`
}
