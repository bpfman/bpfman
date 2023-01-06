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

// +kubebuilder:validation:Required
package v1alpha1

// BpfProgramAttachPoint defines the allowed attach points
// for a program loaded via bpfd Exactly one attach point must
// be set.
// +kubebuilder:validation:MaxProperties=1
// +kubebuilder:validation:MinProperties=1
type BpfProgramAttachPoint struct {
	// NetworkMultiAttach defines an attach point for programs
	// which attach to network devices and must be
	// ordered via bpfd.
	// +optional
	NetworkMultiAttach *BpfNetworkMultiAttach `json:"networkmultiattach,omitempty"`

	// SingleAttach defines an attach point for programs which
	// attach to a single entity and do not need to be ordered.
	// +optional
	SingleAttach *BpfSingleAttach `json:"singleattach,omitempty"`
}

// +kubebuilder:validation:Enum=ABORTED;DROP;PASS;TX;REDIRECT;DISPATCHER_RETURN
type ProceedOnValue string

// BpfNetworkMultiAttach defines an bpf attach
// point for programs which attach to network devices,
// i.e interfaces, and must be prioritized
type BpfNetworkMultiAttach struct {
	// Interface refers to the name of a network interface.
	Interface string `json:"interface"`

	// Priority specifies the priority of the bpf program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// Direction specifies the direction of traffic the bpfprogram should
	// attach to for a given network device, this field should only be
	// set for programs of type TC.
	// TODO(astoycos) see if kubebuilder can handle more complicated validation
	// for this.
	// +kubebuilder:validation:Enum=NONE;INGRESS;EGRESS
	// +kubebuilder:default=NONE
	// +optional
	Direction string `json:"direction"`

	// ProceedOn allows the user to call other programs in chain on this exit code.
	// Multiple values are supported by repeating the parameter. This feature
	// is only applicable for XDP programs.
	// NOTE: These values are not updatable following bpfProgramConfig creation.
	// +optional
	ProceedOn []ProceedOnValue `json:"proceedon"`
}

// BpfSingleAttach defines an ebpf attach
// point for programs which attach to single linux entities,
// i.e cgroups, tracepoints, kprobes etc.
type BpfSingleAttach struct {
	// Name refers to the name of the attach point
	Name string `json:"name"`
}
