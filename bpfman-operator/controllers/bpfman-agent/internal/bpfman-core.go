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
	"strconv"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/containers/image/docker/reference"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("agent-intern")

func imagePullPolicyConversion(policy bpfmaniov1alpha1.PullPolicy) int32 {
	switch policy {
	case bpfmaniov1alpha1.PullAlways:
		return 0
	case bpfmaniov1alpha1.PullIfNotPresent:
		return 1
	case bpfmaniov1alpha1.PullNever:
		return 2
	default:
		return 1
	}
}

func GetBytecode(c client.Client, b *bpfmaniov1alpha1.BytecodeSelector) (*gobpfman.BytecodeLocation, error) {
	if b.Image != nil {
		bytecodeImage := b.Image

		ref, err := reference.ParseNamed(bytecodeImage.Url)
		if err != nil {
			return nil, err
		}

		var username, password string
		if bytecodeImage.ImagePullSecret != nil {
			creds, err := ParseAuth(c, bytecodeImage.ImagePullSecret.Name, bytecodeImage.ImagePullSecret.Namespace)
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

		return &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_Image{Image: &gobpfman.BytecodeImage{
				Url:             bytecodeImage.Url,
				ImagePullPolicy: imagePullPolicyConversion(bytecodeImage.ImagePullPolicy),
				Username:        &username,
				Password:        &password,
			}},
		}, nil
	} else {
		return &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: *b.Path},
		}, nil
	}
}

func buildBpfmanUnloadRequest(id uint32) *gobpfman.UnloadRequest {
	return &gobpfman.UnloadRequest{
		Id: id,
	}
}

func LoadBpfmanProgram(ctx context.Context, bpfmanClient gobpfman.BpfmanClient,
	loadRequest *gobpfman.LoadRequest) (*uint32, error) {
	var res *gobpfman.LoadResponse

	res, err := bpfmanClient.Load(ctx, loadRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to load bpfProgram via bpfman: %w", err)
	}
	kernelInfo := res.GetKernelInfo()
	if kernelInfo == nil {
		return nil, fmt.Errorf("no kernel info for load bpfProgram")
	}
	id := kernelInfo.GetId()

	return &id, nil
}

func UnloadBpfmanProgram(ctx context.Context, bpfmanClient gobpfman.BpfmanClient, id uint32) error {
	_, err := bpfmanClient.Unload(ctx, buildBpfmanUnloadRequest(id))
	if err != nil {
		return fmt.Errorf("failed to unload bpfProgram via bpfman: %v",
			err)
	}
	return nil
}

func ListBpfmanPrograms(ctx context.Context, bpfmanClient gobpfman.BpfmanClient, programType internal.ProgramType) (map[string]*gobpfman.ListResponse_ListResult, error) {
	listOnlyBpfmanPrograms := true
	listReq := gobpfman.ListRequest{
		ProgramType:        programType.Uint32(),
		BpfmanProgramsOnly: &listOnlyBpfmanPrograms,
	}

	out := map[string]*gobpfman.ListResponse_ListResult{}

	listResponse, err := bpfmanClient.List(ctx, &listReq)
	if err != nil {
		return nil, err
	}

	for _, result := range listResponse.Results {
		info := result.GetInfo()
		if info != nil {
			metadata := info.GetMetadata()
			if uuid, ok := metadata[internal.UuidMetadataKey]; ok {
				out[uuid] = result
			} else {
				return nil, fmt.Errorf("Unable to get uuid from program metadata")
			}
		}
	}

	return out, nil
}

func GetBpfmanProgram(ctx context.Context, bpfmanClient gobpfman.BpfmanClient, uuid types.UID) (*gobpfman.ListResponse_ListResult, error) {
	listReq := gobpfman.ListRequest{
		MatchMetadata: map[string]string{internal.UuidMetadataKey: string(uuid)},
	}

	listResponse, err := bpfmanClient.List(ctx, &listReq)
	if err != nil {
		return nil, err
	}

	if len(listResponse.Results) != 1 {
		return nil, fmt.Errorf("multiple programs found for uuid: %+v ", uuid)
	}

	return listResponse.Results[0], nil
}

func ListAllPrograms(ctx context.Context, bpfmanClient gobpfman.BpfmanClient) ([]*gobpfman.ListResponse_ListResult, error) {
	listResponse, err := bpfmanClient.List(ctx, &gobpfman.ListRequest{})
	if err != nil {
		return nil, err
	}

	return listResponse.Results, nil
}

// Convert a list result into a set of kernel info annotations
func Build_kernel_info_annotations(p *gobpfman.ListResponse_ListResult) map[string]string {
	kernelInfo := p.GetKernelInfo()
	if kernelInfo != nil {
		return map[string]string{
			"Kernel-ID":                     fmt.Sprint(kernelInfo.GetId()),
			"Name":                          kernelInfo.GetName(),
			"Type":                          internal.ProgramType(kernelInfo.GetProgramType()).String(),
			"Loaded-At":                     kernelInfo.GetLoadedAt(),
			"Tag":                           kernelInfo.GetTag(),
			"GPL-Compatible":                fmt.Sprintf("%v", kernelInfo.GetGplCompatible()),
			"Map-IDs":                       fmt.Sprintf("%v", kernelInfo.GetMapIds()),
			"BTF-ID":                        fmt.Sprint(kernelInfo.GetBtfId()),
			"Size-Translated-Bytes":         fmt.Sprint(kernelInfo.GetBytesXlated()),
			"JITed":                         fmt.Sprintf("%v", kernelInfo.GetJited()),
			"Size-JITed-Bytes":              fmt.Sprint(kernelInfo.GetBytesJited()),
			"Kernel-Allocated-Memory-Bytes": fmt.Sprint(kernelInfo.GetBytesMemlock()),
			"Verified-Instruction-Count":    fmt.Sprint(kernelInfo.GetVerifiedInsns()),
		}
	}
	return nil
}

// get the program ID from a bpfProgram
func GetID(p *bpfmaniov1alpha1.BpfProgram) (*uint32, error) {
	idString, ok := p.Annotations[internal.IdAnnotation]
	if !ok {
		return nil, nil
	}

	id, err := strconv.ParseUint(idString, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to get program ID: %v", err)
	}
	uid := uint32(id)
	return &uid, nil
}
