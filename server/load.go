package server

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
	pb "github.com/bpfman/bpfman/server/pb"
)

// Load implements the Load RPC method. Manager.Load decides whether the
// request needs the writer flock: ordinary loads stay lockless, while
// explicit map-owner joins and PinByName loads serialise internally.
func (s *Server) Load(ctx context.Context, req *pb.LoadRequest) (*pb.LoadResponse, error) {
	if req.Bytecode == nil {
		return nil, status.Error(codes.InvalidArgument, "bytecode location is required")
	}

	if len(req.Info) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one program info is required")
	}

	// Build LoadSource from bytecode location
	var source manager.LoadSource
	switch loc := req.Bytecode.Location.(type) {
	case *pb.BytecodeLocation_File:
		source.FilePath = loc.File
	case *pb.BytecodeLocation_Image:
		pullPolicy, err := protoToPullPolicy(loc.Image.ImagePullPolicy)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid pull policy: %v", err)
		}

		ref := platform.ImageRef{URL: loc.Image.Url, PullPolicy: pullPolicy, Auth: protoImageAuth(loc.Image)}
		source.Image = &ref
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid bytecode location")
	}

	// Build ProgramSpecs from request info. Every program must be
	// named (the guard above rejects an empty Info); there is no
	// whole-object load.
	programs := make([]manager.ProgramSpec, 0, len(req.Info))
	for _, info := range req.Info {
		if info.Name == "" {
			return nil, status.Error(codes.InvalidArgument, "program name is required")
		}

		progType, err := protoToBpfmanType(info.ProgramType)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid program type for %s: %v", info.Name, err)
		}

		// Extract AttachFunc from ProgSpecificInfo for fentry/fexit
		var attachFunc string
		if info.Info != nil {
			switch i := info.Info.Info.(type) {
			case *pb.ProgSpecificInfo_FentryLoadInfo:
				attachFunc = i.FentryLoadInfo.FnName
			case *pb.ProgSpecificInfo_FexitLoadInfo:
				attachFunc = i.FexitLoadInfo.FnName
			}
		}

		var mapOwnerID kernel.ProgramID
		if req.MapOwnerId != nil && *req.MapOwnerId != 0 {
			mapOwnerID = kernel.ProgramID(*req.MapOwnerId)
		}

		programs = append(programs, manager.ProgramSpec{
			Name:       info.Name,
			Type:       progType,
			AttachFunc: attachFunc,
			MapOwnerID: mapOwnerID,
		})
	}

	loaded, err := s.mgr.Load(ctx, source, programs, manager.LoadOpts{
		UserMetadata: req.Metadata,
		GlobalData:   req.GlobalData,
		Owner:        "bpfman",
	})
	if err != nil {
		switch {
		case errors.Is(err, manager.ErrImagePullerNotConfigured):
			return nil, status.Error(codes.Unimplemented, "OCI image loading not configured on this server")
		case errors.Is(err, platform.ErrMapOwnerNotFound):
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		default:
			return nil, status.Errorf(codes.Internal, "failed to load programs: %v", err)
		}
	}

	// Convert results to proto response
	resp := &pb.LoadResponse{
		Programs: make([]*pb.LoadResponseInfo, 0, len(loaded)),
	}

	for i, prog := range loaded {
		info := req.Info[i]

		// Format LoadedAt as RFC3339 if available
		var loadedAt string
		if prog.Status.Kernel != nil && !prog.Status.Kernel.LoadedAt.IsZero() {
			loadedAt = prog.Status.Kernel.LoadedAt.Format(time.RFC3339)
		}

		progInfo := &pb.ProgramInfo{
			Name:       info.Name,
			Bytecode:   req.Bytecode,
			Metadata:   req.Metadata,
			GlobalData: req.GlobalData,
			MapPinPath: prog.Record.Handles.MapsDir.String(),
			MapUsedBy:  programIDsToStrings(prog.Status.MapUsedBy),
		}
		// Set MapOwnerId for dependent programs
		if prog.Record.Load.MapOwnerID() != 0 {
			v := uint32(prog.Record.Load.MapOwnerID())
			progInfo.MapOwnerId = &v
		}

		// Build KernelProgramInfo from status
		var kernelInfo *pb.KernelProgramInfo
		if prog.Status.Kernel != nil {
			kp := prog.Status.Kernel
			mapIDs := make([]uint32, len(kp.MapIDs))
			for j, id := range kp.MapIDs {
				mapIDs[j] = uint32(id)
			}
			kernelInfo = &pb.KernelProgramInfo{
				Id:            uint32(kp.ID),
				Name:          kp.Name,
				ProgramType:   bpfmanTypeToProto(prog.Record.Load.ProgramType()),
				LoadedAt:      loadedAt,
				Tag:           kp.Tag,
				GplCompatible: prog.Record.GPLCompatible,
				Jited:         kp.JitedSize > 0,
				MapIds:        mapIDs,
				BtfId:         kp.BTFId,
				BytesXlated:   kp.XlatedSize,
				BytesJited:    kp.JitedSize,
				BytesMemlock:  uint32(kp.Memlock),
				VerifiedInsns: kp.VerifiedInstructions,
			}
		}

		resp.Programs = append(resp.Programs, &pb.LoadResponseInfo{
			Info:       progInfo,
			KernelInfo: kernelInfo,
		})
	}

	// Log summary
	programIDs := make([]kernel.ProgramID, len(loaded))
	names := make([]string, len(loaded))
	for i, prog := range loaded {
		programIDs[i] = prog.Record.ProgramID
		names[i] = prog.Record.Meta.Name
	}
	s.logger.InfoContext(ctx, "Load", "programs", names, "program_ids", programIDs)

	return resp, nil
}

// Unload implements the Unload RPC method.
func (s *Server) Unload(ctx context.Context, req *pb.UnloadRequest) (*pb.UnloadResponse, error) {
	return withWriterLock(ctx, s, func(ctx context.Context, writeLock lock.WriterScope) (*pb.UnloadResponse, error) {
		if err := s.mgr.Unload(ctx, writeLock, kernel.ProgramID(req.Id)); err != nil {
			var notManaged bpfman.ErrProgramNotManaged
			var notFound bpfman.ErrProgramNotFound
			switch {
			case errors.As(err, &notManaged), errors.As(err, &notFound):
				return nil, status.Errorf(codes.NotFound, "%v", err)
			case errors.Is(err, platform.ErrRecordNotFound):
				return nil, status.Errorf(codes.NotFound, "program with ID %d not found", req.Id)
			default:
				return nil, status.Errorf(codes.Internal, "failed to unload program: %v", err)
			}
		}

		s.logger.InfoContext(ctx, "Unload", "program_id", req.Id)
		return &pb.UnloadResponse{}, nil
	})
}
