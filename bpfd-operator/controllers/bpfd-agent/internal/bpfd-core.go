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
	"context"
	"fmt"
	"os"
	"path/filepath"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	"github.com/containers/image/docker/reference"

	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("bpfd-agent-internal")

func imagePullPolicyConversion(policy bpfdiov1alpha1.PullPolicy) int32 {
	switch policy {
	case bpfdiov1alpha1.PullAlways:
		return 0
	case bpfdiov1alpha1.PullIfNotPresent:
		return 1
	case bpfdiov1alpha1.PullNever:
		return 2
	default:
		return 1
	}
}

type BpfdBytecodeRep interface {
	*gobpfd.LoadRequestCommon_Image | *gobpfd.LoadRequestCommon_File
}

func GetBytecode(c client.Client, namespace string, b *bpfdiov1alpha1.BytecodeSelector) (interface{}, error) {
	if b.Image != nil {
		bytecodeImage := b.Image

		ref, err := reference.ParseNamed(bytecodeImage.Url)
		if err != nil {
			return nil, err
		}

		var username, password string
		if bytecodeImage.ImagePullSecret != "" {
			creds, err := ParseAuth(c, bytecodeImage.ImagePullSecret, namespace)
			if err != nil {
				return nil, err
			}

			if creds == nil {
				return nil, fmt.Errorf("no registry credentials found in secret: %s", bytecodeImage.ImagePullSecret)
			}

			domain := reference.Domain(ref)

			// All docker.io image domains resolve to https://index.docker.io/v1/ in the credentials JSON file.
			if domain == "docker.io" || domain == "" {
				domain = "https://index.docker.io/v1/"
			}

			cred := (*creds)[domain]

			username = cred.Username
			password = cred.Password
		}

		return &gobpfd.LoadRequestCommon_Image{
			Image: &gobpfd.BytecodeImage{
				Url:             bytecodeImage.Url,
				ImagePullPolicy: imagePullPolicyConversion(bytecodeImage.ImagePullPolicy),
				Username:        username,
				Password:        password,
			},
		}, nil
	} else {
		return &gobpfd.LoadRequestCommon_File{
			File: *b.Path,
		}, nil
	}
}

func BuildBpfdCommon(bytecode interface{}, sectionName string, programType internal.SupportedProgramType, Id string, globalData map[string][]byte) *gobpfd.LoadRequestCommon {
	if imageBytecode, ok := bytecode.(*gobpfd.LoadRequestCommon_Image); ok {
		return &gobpfd.LoadRequestCommon{
			Location:    imageBytecode,
			SectionName: sectionName,
			ProgramType: *programType.Int32(),
			Id:          &Id,
			GlobalData:  globalData,
		}
	}

	if fileBytecode, ok := bytecode.(*gobpfd.LoadRequestCommon_File); ok {
		return &gobpfd.LoadRequestCommon{
			Location:    fileBytecode,
			SectionName: sectionName,
			ProgramType: *programType.Int32(),
			Id:          &Id,
			GlobalData:  globalData,
		}
	}

	return nil
}

func buildBpfdUnloadRequest(uuid string) *gobpfd.UnloadRequest {
	return &gobpfd.UnloadRequest{
		Id: uuid,
	}
}

func LoadBpfdProgram(ctx context.Context, bpfdClient gobpfd.LoaderClient,
	loadRequest *gobpfd.LoadRequest) (map[string]string, error) {
	var res *gobpfd.LoadResponse

	res, err := bpfdClient.Load(ctx, loadRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to load bpfProgram via bpfd: %w", err)
	}
	uuid := res.GetId()

	maps, err := GetMapsForUUID(uuid)
	if err != nil {
		return nil, fmt.Errorf("failed to get bpfProgram's Maps: %v", err)
	}
	//Convert to bpfProgramFormat map[uuid]map[mapName]mapPath
	return maps, nil
}

// GetMapsForUUID returns any maps for the specified bpf program
// which bpfd is managing
func GetMapsForUUID(uuid string) (map[string]string, error) {
	maps := map[string]string{}
	programMapPath := fmt.Sprintf("%s/%s", internal.BpfdMapFs, uuid)

	// The directory may not be created instantaneously by bpfd so wait 10 seconds
	if err := filepath.Walk(programMapPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Name() != uuid {
			maps[info.Name()] = path
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return maps, nil
}

func UnloadBpfdProgram(ctx context.Context, bpfdClient gobpfd.LoaderClient, id string) error {
	_, err := bpfdClient.Unload(ctx, buildBpfdUnloadRequest(id))
	if err != nil {
		return fmt.Errorf("failed to unload bpfProgram via bpfd: %v",
			err)
	}
	return nil
}
