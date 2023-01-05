//go:build linux
// +build linux

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

package bpfdAppClient

import (
	"context"
	"fmt"
	"log"
	"os"

	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	DefaultMapDir = "/run/bpfd/fs/maps"
)

func GetMapPathDyn(bpfProgramConfigName string, mapIndex string) (string, error){
	var mapPath string

	config, err := rest.InClusterConfig()
	if err != nil {
		return mapPath, err
	}

	ctx := context.Background()

	// Get the nodename where this pod is running
	nodeName := os.Getenv("NODENAME")
	if nodeName == "" {
		return mapPath, fmt.Errorf("NODENAME env var not set")
	}
	bpfProgramName := bpfProgramConfigName + "-" + nodeName

	// Get map pin path from relevant BpfProgram Object with a dynamic go-client
	clientSet := dynamic.NewForConfigOrDie(config)

	bpfProgramResource := schema.GroupVersionResource{
		Group:    "bpfd.io",
		Version:  "v1alpha1",
		Resource: "bpfprograms",
	}

	bpfProgramBlob, err := clientSet.Resource(bpfProgramResource).
		Get(ctx, fmt.Sprintf("%s", bpfProgramName), metav1.GetOptions{})
	if err != nil {
		log.Printf("Error reading BpfProgram %s: %v", bpfProgramName, err)
		return mapPath, err
	}

	var bpfProgram bpfdiov1alpha1.BpfProgram
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(bpfProgramBlob.UnstructuredContent(), &bpfProgram)
	if err != nil {
		panic(err)
	}

	for _, v := range bpfProgram.Spec.Programs {
		mapPath = v.Maps[mapIndex]
		log.Printf("mapPath=%s", mapPath)
	}

	return mapPath, nil
}