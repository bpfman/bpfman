package server

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
	pb "github.com/bpfman/bpfman/server/pb"
)

// List implements the List RPC method.
func (s *Server) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	var opts []bpfman.ListOption
	if req.ProgramType != nil {
		pt, err := protoToBpfmanType(pb.BpfmanProgramType(*req.ProgramType))
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid program type: %v", err)
		}

		opts = append(opts, bpfman.WithTypes(pt))
	}
	if len(req.MatchMetadata) > 0 {
		opts = append(opts, bpfman.MatchingLabels(req.MatchMetadata))
	}

	result, err := s.mgr.ListPrograms(ctx, opts...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list programs: %v", err)
	}

	var results []*pb.ListResponse_ListResult
	for _, prog := range result {
		// Only include programs that are also in kernel
		if prog.Status.Kernel == nil {
			continue
		}
		kp := prog.Status.Kernel

		info := &pb.ProgramInfo{
			Name:       prog.Record.Meta.Name,
			Bytecode:   &pb.BytecodeLocation{Location: &pb.BytecodeLocation_File{File: prog.Record.Load.ObjectPath()}},
			Metadata:   prog.Record.Meta.Metadata,
			GlobalData: prog.Record.Load.GlobalData(),
			MapPinPath: prog.Record.Handles.MapsDir.String(),
			MapUsedBy:  programIDsToStrings(prog.Status.MapUsedBy),
		}
		if prog.Record.Handles.MapOwnerID != nil {
			v := uint32(*prog.Record.Handles.MapOwnerID)
			info.MapOwnerId = &v
		}

		mapIDs := make([]uint32, len(kp.MapIDs))
		for i, id := range kp.MapIDs {
			mapIDs[i] = uint32(id)
		}

		results = append(results, &pb.ListResponse_ListResult{
			Info: info,
			KernelInfo: &pb.KernelProgramInfo{
				Id:          uint32(prog.Record.ProgramID),
				Name:        kp.Name,
				ProgramType: bpfmanTypeToProto(prog.Record.Load.ProgramType()),
				Tag:         kp.Tag,
				LoadedAt:    kp.LoadedAt.Format(time.RFC3339),
				MapIds:      mapIDs,
				BtfId:       kp.BTFId,
				BytesXlated: kp.XlatedSize,
				BytesJited:  kp.JitedSize,
			},
		})
	}

	s.logger.InfoContext(ctx, "List", "programs", len(results))
	return &pb.ListResponse{Results: results}, nil
}

// Get implements the Get RPC method.
func (s *Server) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	prog, err := s.mgr.Get(ctx, kernel.ProgramID(req.Id))
	if errors.Is(err, platform.ErrRecordNotFound) {
		return nil, status.Errorf(codes.NotFound, "program with ID %d not found", req.Id)
	}
	var reconcileErr manager.ErrProgramRequiresReconciliation
	if errors.As(err, &reconcileErr) {
		return nil, status.Error(codes.Internal, reconcileErr.Error())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get program: %v", err)
	}

	kp := prog.Status.Kernel

	// Managed link IDs from the program's status.
	linkIDs := make([]uint32, 0, len(prog.Status.Links))
	for _, link := range prog.Status.Links {
		linkIDs = append(linkIDs, grpcLinkID(link.Record.ID))
	}

	s.logger.InfoContext(ctx, "Get", "program_id", req.Id, "program_name", prog.Record.Meta.Name, "links", len(linkIDs))

	info := &pb.ProgramInfo{
		Name:       prog.Record.Meta.Name,
		Bytecode:   &pb.BytecodeLocation{Location: &pb.BytecodeLocation_File{File: prog.Record.Load.ObjectPath()}},
		Metadata:   prog.Record.Meta.Metadata,
		GlobalData: prog.Record.Load.GlobalData(),
		MapPinPath: prog.Record.Handles.MapsDir.String(),
		MapUsedBy:  programIDsToStrings(prog.Status.MapUsedBy),
		Links:      linkIDs,
	}
	if prog.Record.Handles.MapOwnerID != nil {
		v := uint32(*prog.Record.Handles.MapOwnerID)
		info.MapOwnerId = &v
	}

	mapIDs := make([]uint32, len(kp.MapIDs))
	for i, id := range kp.MapIDs {
		mapIDs[i] = uint32(id)
	}

	return &pb.GetResponse{
		Info: info,
		KernelInfo: &pb.KernelProgramInfo{
			Id:            req.Id,
			Name:          kp.Name,
			ProgramType:   bpfmanTypeToProto(prog.Record.Load.ProgramType()),
			Tag:           kp.Tag,
			LoadedAt:      kp.LoadedAt.Format(time.RFC3339),
			GplCompatible: prog.Record.GPLCompatible,
			MapIds:        mapIDs,
			BtfId:         kp.BTFId,
			BytesXlated:   kp.XlatedSize,
			BytesJited:    kp.JitedSize,
		},
	}, nil
}
