package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/platform"
	pb "github.com/bpfman/bpfman/server/pb"
)

func grpcLinkID(id bpfman.LinkID) uint32 {
	// The legacy gRPC API carries link handles as uint32; bpfman
	// handles are uint64, so this narrows at the boundary.
	return uint32(id)
}

func bpfmanLinkID(id uint32) bpfman.LinkID {
	return bpfman.LinkID(id)
}

// Attach implements the Attach RPC method.
func (s *Server) Attach(ctx context.Context, req *pb.AttachRequest) (*pb.AttachResponse, error) {
	if req.Attach == nil {
		return nil, status.Error(codes.InvalidArgument, "attach info is required")
	}

	return withWriterLock(ctx, s, func(ctx context.Context, writeLock lock.WriterScope) (*pb.AttachResponse, error) {
		var attachType string
		var resp *pb.AttachResponse
		var err error

		programID := kernel.ProgramID(req.Id)

		switch info := req.Attach.Info.(type) {
		case *pb.AttachInfo_TracepointAttachInfo:
			attachType = "tracepoint"
			resp, err = s.attachTracepoint(ctx, writeLock, programID, info.TracepointAttachInfo)
		case *pb.AttachInfo_XdpAttachInfo:
			attachType = "xdp"
			resp, err = s.attachXDP(ctx, writeLock, programID, info.XdpAttachInfo)
		case *pb.AttachInfo_TcAttachInfo:
			attachType = "tc"
			resp, err = s.attachTC(ctx, writeLock, programID, info.TcAttachInfo)
		case *pb.AttachInfo_TcxAttachInfo:
			attachType = "tcx"
			resp, err = s.attachTCX(ctx, writeLock, programID, info.TcxAttachInfo)
		case *pb.AttachInfo_KprobeAttachInfo:
			attachType = "kprobe"
			resp, err = s.attachKprobe(ctx, writeLock, programID, info.KprobeAttachInfo)
		case *pb.AttachInfo_UprobeAttachInfo:
			attachType = "uprobe"
			resp, err = s.attachUprobe(ctx, writeLock, programID, info.UprobeAttachInfo)
		case *pb.AttachInfo_FentryAttachInfo:
			attachType = "fentry"
			resp, err = s.attachFentry(ctx, writeLock, programID, info.FentryAttachInfo)
		case *pb.AttachInfo_FexitAttachInfo:
			attachType = "fexit"
			resp, err = s.attachFexit(ctx, writeLock, programID, info.FexitAttachInfo)
		default:
			return nil, status.Errorf(codes.Unimplemented, "attach type %T not yet implemented", req.Attach.Info)
		}

		if err != nil {
			return nil, err
		}

		s.logger.InfoContext(ctx, "Attach", "type", attachType, "program_id", req.Id, "link_id", resp.LinkId)
		return resp, nil
	})
}

// attachManagerError translates an error returned by Manager.Attach
// into a gRPC status. It centralises the mappings every attach handler
// shares: a missing program is NotFound, and a program whose loaded
// type the attach verb cannot drive (ErrAttachKindMismatch) is a client
// precondition failure (FailedPrecondition), not an internal fault.
// Anything else is Internal. Handlers with extra type-specific cases
// (an unresolved interface, a near-miss tracepoint) check those first
// and delegate the rest here.
func attachManagerError(programID kernel.ProgramID, kind string, err error) error {
	var notFound bpfman.ErrProgramNotFound
	if errors.As(err, &notFound) || errors.Is(err, platform.ErrRecordNotFound) {
		return status.Errorf(codes.NotFound, "program with ID %d not found", programID)
	}

	var mismatch bpfman.ErrAttachKindMismatch
	if errors.As(err, &mismatch) {
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	return status.Errorf(codes.Internal, "attach %s: %v", kind, err)
}

// attachTracepoint handles tracepoint attachment via the manager.
func (s *Server) attachTracepoint(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, info *pb.TracepointAttachInfo) (*pb.AttachResponse, error) {
	spec, err := bpfman.NewTracepointAttachSpecFromString(programID, info.Tracepoint)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid tracepoint attach spec: %v", err)
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		var tpNotFound bpfman.ErrTracepointNotFound
		if errors.As(err, &tpNotFound) {
			return nil, status.Errorf(codes.NotFound, "%v", err)
		}
		return nil, attachManagerError(programID, "tracepoint", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachXDP handles XDP attachment via the manager.
func (s *Server) attachXDP(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, info *pb.XDPAttachInfo) (*pb.AttachResponse, error) {
	// Build the spec from the request; the manager resolves the
	// interface name inside the target netns.
	spec, err := bpfman.NewXDPAttachSpec(programID, info.Iface, int(info.Priority))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid XDP attach spec: %v", err)
	}

	// Use provided proceed-on or default
	if len(info.ProceedOn) > 0 {
		spec, err = spec.WithProceedOnCodes(info.ProceedOn)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid XDP proceed_on: %v", err)
		}
	}

	// Apply network namespace if specified
	if info.GetNetns() != "" {
		spec = spec.WithNetns(info.GetNetns())
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		if errors.Is(err, platform.ErrInterfaceNotFound) {
			return nil, status.Errorf(codes.InvalidArgument, "attach XDP: %v", err)
		}
		return nil, attachManagerError(programID, "XDP", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachTC handles TC attachment via the manager.
func (s *Server) attachTC(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, info *pb.TCAttachInfo) (*pb.AttachResponse, error) {
	// Build the spec from the request; the manager resolves the
	// interface name inside the target netns.
	spec, err := bpfman.NewTCAttachSpecFromString(programID, info.Iface, info.Direction, int(info.Priority))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid TC attach spec: %v", err)
	}

	// Use provided proceed-on if any; otherwise the manager default
	// (Pipe|DispatcherReturn) applies, matching Rust bpfman.
	if len(info.ProceedOn) > 0 {
		spec, err = spec.WithProceedOnCodes(info.ProceedOn)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid TC proceed_on: %v", err)
		}
	}

	// Apply network namespace if specified
	if info.GetNetns() != "" {
		spec = spec.WithNetns(info.GetNetns())
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		if errors.Is(err, platform.ErrInterfaceNotFound) {
			return nil, status.Errorf(codes.InvalidArgument, "attach TC: %v", err)
		}
		return nil, attachManagerError(programID, "TC", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachTCX handles TCX attachment via the manager.
func (s *Server) attachTCX(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, info *pb.TCXAttachInfo) (*pb.AttachResponse, error) {
	// Build the spec from the request; the manager resolves the
	// interface name inside the target netns.
	spec, err := bpfman.NewTCXAttachSpecFromString(programID, info.Iface, info.Direction, int(info.Priority))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid TCX attach spec: %v", err)
	}

	// Apply network namespace if specified
	if info.GetNetns() != "" {
		spec = spec.WithNetns(info.GetNetns())
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		if errors.Is(err, platform.ErrInterfaceNotFound) {
			return nil, status.Errorf(codes.InvalidArgument, "attach TCX: %v", err)
		}
		return nil, attachManagerError(programID, "TCX", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachKprobe handles kprobe/kretprobe attachment via the manager.
func (s *Server) attachKprobe(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, info *pb.KprobeAttachInfo) (*pb.AttachResponse, error) {
	if info.FnName == "" {
		return nil, status.Error(codes.InvalidArgument, "fn_name is required for kprobe attachment")
	}

	// Construct AttachSpec with validation
	spec, err := bpfman.NewKprobeAttachSpec(programID, info.FnName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid kprobe attach spec: %v", err)
	}

	if info.Offset != 0 {
		spec = spec.WithOffset(info.Offset)
	}

	// Call manager - it will determine retprobe from program type
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		return nil, attachManagerError(programID, "kprobe", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachUprobe handles uprobe/uretprobe attachment via the manager.
func (s *Server) attachUprobe(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, info *pb.UprobeAttachInfo) (*pb.AttachResponse, error) {
	s.logger.DebugContext(ctx, "attachUprobe request", "program_id", programID, "target", info.Target, "fn_name", info.GetFnName(), "offset", info.Offset, "pid", info.GetPid(), "container_pid", info.GetContainerPid(), "container_pid_ptr", info.ContainerPid)

	if info.Target == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required for uprobe attachment")
	}

	// Construct UprobeAttachSpec with validated input
	spec, err := bpfman.NewUprobeAttachSpec(programID, info.Target, info.GetPid(), info.GetContainerPid())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid uprobe attach spec: %v", err)
	}

	if info.GetFnName() != "" {
		spec = spec.WithFnName(info.GetFnName())
	}
	if info.Offset != 0 {
		spec = spec.WithOffset(info.Offset)
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		return nil, attachManagerError(programID, "uprobe", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachFentry handles fentry attachment via the manager.
// The attach function is stored in the program metadata from load time.
func (s *Server) attachFentry(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, _ *pb.FentryAttachInfo) (*pb.AttachResponse, error) {
	// Construct FentryAttachSpec with validated input
	spec, err := bpfman.NewFentryAttachSpec(programID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid fentry attach spec: %v", err)
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		return nil, attachManagerError(programID, "fentry", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// attachFexit handles fexit attachment via the manager.
// The attach function is stored in the program metadata from load time.
func (s *Server) attachFexit(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, _ *pb.FexitAttachInfo) (*pb.AttachResponse, error) {
	// Construct FexitAttachSpec with validated input
	spec, err := bpfman.NewFexitAttachSpec(programID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid fexit attach spec: %v", err)
	}

	// Call manager
	link, err := s.mgr.Attach(ctx, writeLock, spec)
	if err != nil {
		return nil, attachManagerError(programID, "fexit", err)
	}

	return &pb.AttachResponse{
		LinkId: grpcLinkID(link.Record.ID),
	}, nil
}

// Detach implements the Detach RPC method.
func (s *Server) Detach(ctx context.Context, req *pb.DetachRequest) (*pb.DetachResponse, error) {
	return withWriterLock(ctx, s, func(ctx context.Context, writeLock lock.WriterScope) (*pb.DetachResponse, error) {
		if err := s.mgr.Detach(ctx, writeLock, bpfmanLinkID(req.LinkId)); err != nil {
			var notFound bpfman.ErrLinkNotFound
			switch {
			case errors.As(err, &notFound):
				return nil, status.Errorf(codes.NotFound, "%v", err)
			case errors.Is(err, platform.ErrRecordNotFound):
				return nil, status.Errorf(codes.NotFound, "link with ID %d not found", req.LinkId)
			default:
				return nil, status.Errorf(codes.Internal, "detach link: %v", err)
			}
		}

		s.logger.InfoContext(ctx, "Detach", "link_id", req.LinkId)
		return &pb.DetachResponse{}, nil
	})
}
