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

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	"github.com/containers/image/docker/reference"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("agent-intern")

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

func GetBytecode(c client.Client, b *bpfdiov1alpha1.BytecodeSelector) (*gobpfd.BytecodeLocation, error) {
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

		return &gobpfd.BytecodeLocation{
			Location: &gobpfd.BytecodeLocation_Image{Image: &gobpfd.BytecodeImage{
				Url:             bytecodeImage.Url,
				ImagePullPolicy: imagePullPolicyConversion(bytecodeImage.ImagePullPolicy),
				Username:        &username,
				Password:        &password,
			}},
		}, nil
	} else {
		return &gobpfd.BytecodeLocation{
			Location: &gobpfd.BytecodeLocation_File{File: *b.Path},
		}, nil
	}
}

func buildBpfdUnloadRequest(id uint32) *gobpfd.UnloadRequest {
	return &gobpfd.UnloadRequest{
		Id: id,
	}
}

func LoadBpfdProgram(ctx context.Context, bpfdClient gobpfd.BpfdClient,
	loadRequest *gobpfd.LoadRequest) (*uint32, error) {
	var res *gobpfd.LoadResponse

	res, err := bpfdClient.Load(ctx, loadRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to load bpfProgram via bpfd: %w", err)
	}
	kernelInfo := res.GetKernelInfo()
	if kernelInfo == nil {
		return nil, fmt.Errorf("no kernel info for load bpfProgram")
	}
	id := kernelInfo.GetId()

	return &id, nil
}

func UnloadBpfdProgram(ctx context.Context, bpfdClient gobpfd.BpfdClient, id uint32) error {
	_, err := bpfdClient.Unload(ctx, buildBpfdUnloadRequest(id))
	if err != nil {
		return fmt.Errorf("failed to unload bpfProgram via bpfd: %v",
			err)
	}
	return nil
}

func ListBpfdPrograms(ctx context.Context, bpfdClient gobpfd.BpfdClient, programType internal.ProgramType) (map[string]*gobpfd.ListResponse_ListResult, error) {
	listOnlyBpfdPrograms := true
	listReq := gobpfd.ListRequest{
		ProgramType:      programType.Uint32(),
		BpfdProgramsOnly: &listOnlyBpfdPrograms,
	}

	out := map[string]*gobpfd.ListResponse_ListResult{}

	listResponse, err := bpfdClient.List(ctx, &listReq)
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

func GetBpfdProgram(ctx context.Context, bpfdClient gobpfd.BpfdClient, uuid types.UID) (*gobpfd.ListResponse_ListResult, error) {

	listReq := gobpfd.ListRequest{
		MatchMetadata: map[string]string{internal.UuidMetadataKey: string(uuid)},
	}

	listResponse, err := bpfdClient.List(ctx, &listReq)
	if err != nil {
		return nil, err
	}

	if len(listResponse.Results) != 1 {
		return nil, fmt.Errorf("multible programs found for uuid: %+v ", uuid)
	}

	return listResponse.Results[0], nil
}

func ListAllPrograms(ctx context.Context, bpfdClient gobpfd.BpfdClient) ([]*gobpfd.ListResponse_ListResult, error) {
	listResponse, err := bpfdClient.List(ctx, &gobpfd.ListRequest{})
	if err != nil {
		return nil, err
	}

	return listResponse.Results, nil
}

// Convert a list result into a set of kernel info annotations
func Build_kernel_info_annotations(p *gobpfd.ListResponse_ListResult) map[string]string {
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
func GetID(p *bpfdiov1alpha1.BpfProgram) (*uint32, error) {
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
