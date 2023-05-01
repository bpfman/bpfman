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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InterfaceSelector defines interface to attach to.
// +kubebuilder:validation:MaxProperties=1
// +kubebuilder:validation:MinProperties=1
type InterfaceSelector struct {
	// Interface refers to the name of a network interface to attach BPF program too.
	// +optional
	Interface *string `json:"interface,omitempty"`

	// Attach BPF program to the primary interface on the node. Only 'true' accepted.
	// +optional
	PrimaryNodeInterface *bool `json:"primarynodeinterface,omitempty"`
}

// BpfProgramCommon defines the common attributes for all BPF programs
type BpfProgramCommon struct {
	// SectionName is the the section name described in the bpf Program
	SectionName string `json:"sectionname"`

	// NodeSelector allows the user to specify which nodes to deploy the
	// bpf program to.  This field must be specified, to select all nodes
	// use standard metav1.LabelSelector semantics and make it empty.
	NodeSelector metav1.LabelSelector `json:"nodeselector"`

	// Bytecode configures where the bpf program's bytecode should be loaded
	// from.
	ByteCode BytecodeSelector `json:"bytecode"`

	// GlobalData allows the user to to set global variables when the program is loaded
	// with an array of raw bytes. This is a very low level primitive. The caller
	// is responsible for formatting the byte string appropriately considering
	// such things as size, endianness, alignment and packing of data structures.
	// +optional
	GlobalData map[string][]byte `json:"globaldata,omitempty"`
}

// PullPolicy describes a policy for if/when to pull a container image
// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type PullPolicy string

const (
	// PullAlways means that bpfd always attempts to pull the latest bytecode image. Container will fail If the pull fails.
	PullAlways PullPolicy = "Always"
	// PullNever means that bpfd never pulls an image, but only uses a local image. Container will fail if the image isn't present
	PullNever PullPolicy = "Never"
	// PullIfNotPresent means that bpfd pulls if the image isn't present on disk. Container will fail if the image isn't present and the pull fails.
	PullIfNotPresent PullPolicy = "IfNotPresent"
)

// BytecodeSelector defines the various ways to reference bpf bytecode objects.
type BytecodeSelector struct {
	// Image used to specify a bytecode container image.
	Image *BytecodeImage `json:"image,omitempty"`

	// Path is used to specify a bytecode object via filepath.
	Path *string `json:"path,omitempty"`
}

// BytecodeImage defines how to specify a bytecode container image.
type BytecodeImage struct {
	// Valid container image URL used to reference a remote bytecode image.
	Url string `json:"url"`

	// PullPolicy describes a policy for if/when to pull a bytecode image. Defaults to IfNotPresent.
	// +kubebuilder:default:=IfNotPresent
	// +optional
	ImagePullPolicy PullPolicy `json:"imagepullpolicy"`

	// ImagePullSecret is the name of the secret bpfd should use to get remote image
	// repository secrets.
	// +optional
	ImagePullSecret *ImagePullSecretSelector `json:"imagepullsecret,omitempty"`
}

// ImagePullSecretSelector defines the name and namespace of an image pull secret.
type ImagePullSecretSelector struct {
	// Name of the secret which contains the credentials to access the image repository.
	Name string `json:"name"`

	// Namespace of the secret which contains the credentials to access the image repository.
	Namespace string `json:"namespace"`
}
