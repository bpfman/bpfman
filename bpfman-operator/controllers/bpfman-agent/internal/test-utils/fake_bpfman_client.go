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
	"fmt"
	"math/rand"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	grpc "google.golang.org/grpc"
)

type BpfmanClientFake struct {
	LoadRequests         map[int]*gobpfman.LoadRequest
	UnloadRequests       map[int]*gobpfman.UnloadRequest
	ListRequests         []*gobpfman.ListRequest
	GetRequests          map[int]*gobpfman.GetRequest
	Programs             map[int]*gobpfman.ListResponse_ListResult
	PullBytecodeRequests map[int]*gobpfman.PullBytecodeRequest
}

func NewBpfmanClientFake() *BpfmanClientFake {
	return &BpfmanClientFake{
		LoadRequests:         map[int]*gobpfman.LoadRequest{},
		UnloadRequests:       map[int]*gobpfman.UnloadRequest{},
		ListRequests:         []*gobpfman.ListRequest{},
		GetRequests:          map[int]*gobpfman.GetRequest{},
		Programs:             map[int]*gobpfman.ListResponse_ListResult{},
		PullBytecodeRequests: map[int]*gobpfman.PullBytecodeRequest{},
	}
}

func NewBpfmanClientFakeWithPrograms(programs map[int]*gobpfman.ListResponse_ListResult) *BpfmanClientFake {
	return &BpfmanClientFake{
		LoadRequests:   map[int]*gobpfman.LoadRequest{},
		UnloadRequests: map[int]*gobpfman.UnloadRequest{},
		ListRequests:   []*gobpfman.ListRequest{},
		GetRequests:    map[int]*gobpfman.GetRequest{},
		Programs:       programs,
	}
}

func (b *BpfmanClientFake) Load(ctx context.Context, in *gobpfman.LoadRequest, opts ...grpc.CallOption) (*gobpfman.LoadResponse, error) {
	id := rand.Intn(100)
	b.LoadRequests[id] = in

	b.Programs[id] = loadRequestToListResult(in, uint32(id))

	return &gobpfman.LoadResponse{
		Info:       b.Programs[id].Info,
		KernelInfo: b.Programs[id].KernelInfo,
	}, nil
}

func (b *BpfmanClientFake) Unload(ctx context.Context, in *gobpfman.UnloadRequest, opts ...grpc.CallOption) (*gobpfman.UnloadResponse, error) {
	b.UnloadRequests[int(in.Id)] = in
	delete(b.Programs, int(in.Id))

	return &gobpfman.UnloadResponse{}, nil
}

func (b *BpfmanClientFake) List(ctx context.Context, in *gobpfman.ListRequest, opts ...grpc.CallOption) (*gobpfman.ListResponse, error) {
	b.ListRequests = append(b.ListRequests, in)
	results := &gobpfman.ListResponse{Results: []*gobpfman.ListResponse_ListResult{}}
	for _, v := range b.Programs {
		results.Results = append(results.Results, v)
	}
	return results, nil
}

func loadRequestToListResult(loadReq *gobpfman.LoadRequest, id uint32) *gobpfman.ListResponse_ListResult {
	mapOwnerId := loadReq.GetMapOwnerId()
	programInfo := gobpfman.ProgramInfo{
		Name:       loadReq.GetName(),
		Bytecode:   loadReq.GetBytecode(),
		Attach:     loadReq.GetAttach(),
		GlobalData: loadReq.GetGlobalData(),
		MapOwnerId: &mapOwnerId,
		Metadata:   loadReq.GetMetadata(),
	}
	kernelInfo := gobpfman.KernelProgramInfo{
		Id:          id,
		ProgramType: loadReq.GetProgramType(),
	}

	return &gobpfman.ListResponse_ListResult{
		Info:       &programInfo,
		KernelInfo: &kernelInfo,
	}
}

func (b *BpfmanClientFake) Get(ctx context.Context, in *gobpfman.GetRequest, opts ...grpc.CallOption) (*gobpfman.GetResponse, error) {
	if b.Programs[int(in.Id)] != nil {
		return &gobpfman.GetResponse{
			Info:       b.Programs[int(in.Id)].Info,
			KernelInfo: b.Programs[int(in.Id)].KernelInfo,
		}, nil
	} else {
		return nil, fmt.Errorf("Requested program does not exist")
	}
}

func (b *BpfmanClientFake) PullBytecode(ctx context.Context, in *gobpfman.PullBytecodeRequest, opts ...grpc.CallOption) (*gobpfman.PullBytecodeResponse, error) {
	return &gobpfman.PullBytecodeResponse{}, nil
}
