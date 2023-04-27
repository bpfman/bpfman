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
// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	apisv1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	versioned "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/client/clientset/versioned"
	internalinterfaces "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/client/listers/apis/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// XdpProgramInformer provides access to a shared informer and lister for
// XdpPrograms.
type XdpProgramInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.XdpProgramLister
}

type xdpProgramInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewXdpProgramInformer constructs a new informer for XdpProgram type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewXdpProgramInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredXdpProgramInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredXdpProgramInformer constructs a new informer for XdpProgram type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredXdpProgramInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.BpfdV1alpha1().XdpPrograms().List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.BpfdV1alpha1().XdpPrograms().Watch(context.TODO(), options)
			},
		},
		&apisv1alpha1.XdpProgram{},
		resyncPeriod,
		indexers,
	)
}

func (f *xdpProgramInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredXdpProgramInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *xdpProgramInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&apisv1alpha1.XdpProgram{}, f.defaultInformer)
}

func (f *xdpProgramInformer) Lister() v1alpha1.XdpProgramLister {
	return v1alpha1.NewXdpProgramLister(f.Informer().GetIndexer())
}
