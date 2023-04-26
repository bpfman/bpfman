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

package testutils

import (
	"context"

	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	grpc "google.golang.org/grpc"
)

type BpfdClientFake struct {
	LoadRequests   map[string]*gobpfd.LoadRequest
	UnloadRequests map[string]*gobpfd.UnloadRequest
	ListRequests   []*gobpfd.ListRequest
	Programs       map[string]*gobpfd.ListResponse_ListResult
}

func NewBpfdClientFake() *BpfdClientFake {
	return &BpfdClientFake{
		LoadRequests:   map[string]*gobpfd.LoadRequest{},
		UnloadRequests: map[string]*gobpfd.UnloadRequest{},
		ListRequests:   []*gobpfd.ListRequest{},
		Programs:       map[string]*gobpfd.ListResponse_ListResult{},
	}
}

func (b *BpfdClientFake) Load(ctx context.Context, in *gobpfd.LoadRequest, opts ...grpc.CallOption) (*gobpfd.LoadResponse, error) {
	b.LoadRequests[*in.Common.Id] = in

	b.Programs[*in.Common.Id] = loadRequestToListResult(in)

	return &gobpfd.LoadResponse{}, nil
}

func (b *BpfdClientFake) Unload(ctx context.Context, in *gobpfd.UnloadRequest, opts ...grpc.CallOption) (*gobpfd.UnloadResponse, error) {
	b.UnloadRequests[in.Id] = in
	delete(b.Programs, in.Id)

	return &gobpfd.UnloadResponse{}, nil
}

func (b *BpfdClientFake) List(ctx context.Context, in *gobpfd.ListRequest, opts ...grpc.CallOption) (*gobpfd.ListResponse, error) {
	b.ListRequests = append(b.ListRequests, in)
	results := &gobpfd.ListResponse{Results: []*gobpfd.ListResponse_ListResult{}}
	for _, v := range b.Programs {
		results.Results = append(results.Results, v)
	}
	return results, nil
}

func loadRequestToListResult(loadReq *gobpfd.LoadRequest) *gobpfd.ListResponse_ListResult {
	listResult := &gobpfd.ListResponse_ListResult{}

	if loadReq.Common.GetImage() != nil {
		listResult.Location = &gobpfd.ListResponse_ListResult_Image{
			Image: loadReq.Common.GetImage(),
		}
	} else {
		listResult.Location = &gobpfd.ListResponse_ListResult_File{
			File: loadReq.Common.GetFile(),
		}
	}

	if loadReq.GetXdpAttachInfo() != nil {
		listResult.AttachInfo = &gobpfd.ListResponse_ListResult_XdpAttachInfo{
			XdpAttachInfo: loadReq.GetXdpAttachInfo(),
		}
	} else if loadReq.GetTcAttachInfo() != nil {
		listResult.AttachInfo = &gobpfd.ListResponse_ListResult_TcAttachInfo{
			TcAttachInfo: loadReq.GetTcAttachInfo(),
		}
	} else if loadReq.GetTracepointAttachInfo() != nil {
		listResult.AttachInfo = &gobpfd.ListResponse_ListResult_TracepointAttachInfo{
			TracepointAttachInfo: loadReq.GetTracepointAttachInfo(),
		}
	}

	listResult.Id = *loadReq.Common.Id
	listResult.SectionName = &loadReq.Common.SectionName
	listResult.ProgramType = loadReq.Common.ProgramType

	return listResult
}
