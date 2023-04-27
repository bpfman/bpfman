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

package internal

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
)

// Only reconcile if a bpfprogram has been created for the controller's type.
func BpfProgramTypePredicate(kind string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.(*bpfdiov1alpha1.BpfProgram).Spec.Type == kind
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.(*bpfdiov1alpha1.BpfProgram).Spec.Type == kind
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.(*bpfdiov1alpha1.BpfProgram).Spec.Type == kind

		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.(*bpfdiov1alpha1.BpfProgram).Spec.Type == kind
		},
	}
}
