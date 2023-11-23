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
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
)

// Only reconcile if a bpfprogram has been created for the controller's program type.
func BpfProgramTypePredicate(kind string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.(*bpfmaniov1alpha1.BpfProgram).Spec.Type == kind
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.(*bpfmaniov1alpha1.BpfProgram).Spec.Type == kind
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.(*bpfmaniov1alpha1.BpfProgram).Spec.Type == kind
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.(*bpfmaniov1alpha1.BpfProgram).Spec.Type == kind
		},
	}
}

// Only reconcile if a bpfprogram has been created for a controller's node.
func BpfProgramNodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()[K8sHostLabel] == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()[K8sHostLabel] == nodeName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()[K8sHostLabel] == nodeName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetLabels()[K8sHostLabel] == nodeName
		},
	}
}

// Only reconcile if a bpfprogram has been created for a controller's node.
func DiscoveredBpfProgramPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			_, ok := e.Object.GetLabels()[DiscoveredLabel]
			return ok
		},
		CreateFunc: func(e event.CreateEvent) bool {
			_, ok := e.Object.GetLabels()[DiscoveredLabel]
			return ok
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, ok := e.ObjectNew.GetLabels()[DiscoveredLabel]
			return ok
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			_, ok := e.Object.GetLabels()[DiscoveredLabel]
			return ok
		},
	}
}

func StatusChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfmaniov1alpha1.BpfProgram)
			newObject := e.ObjectNew.(*bpfmaniov1alpha1.BpfProgram)
			return !reflect.DeepEqual(oldObject.Status, newObject.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
}
