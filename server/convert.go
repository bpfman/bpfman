package server

import (
	"fmt"
	"strconv"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
	pb "github.com/bpfman/bpfman/server/pb"
)

// protoToBpfmanType converts proto program type to bpfman type.
// Returns an error for unknown or unspecified types (parse, don't validate).
func protoToBpfmanType(pt pb.BpfmanProgramType) (bpfman.ProgramType, error) {
	switch pt {
	case pb.BpfmanProgramType_XDP:
		return bpfman.ProgramTypeXDP, nil
	case pb.BpfmanProgramType_TC:
		return bpfman.ProgramTypeTC, nil
	case pb.BpfmanProgramType_TRACEPOINT:
		return bpfman.ProgramTypeTracepoint, nil
	case pb.BpfmanProgramType_KPROBE:
		return bpfman.ProgramTypeKprobe, nil
	case pb.BpfmanProgramType_UPROBE:
		return bpfman.ProgramTypeUprobe, nil
	case pb.BpfmanProgramType_FENTRY:
		return bpfman.ProgramTypeFentry, nil
	case pb.BpfmanProgramType_FEXIT:
		return bpfman.ProgramTypeFexit, nil
	case pb.BpfmanProgramType_TCX:
		return bpfman.ProgramTypeTCX, nil
	default:
		return "", fmt.Errorf("unknown program type: %d", pt)
	}
}

// bpfmanTypeToProto converts a bpfman ProgramType to its proto uint32 value.
func bpfmanTypeToProto(pt bpfman.ProgramType) uint32 {
	switch pt {
	case bpfman.ProgramTypeXDP:
		return uint32(pb.BpfmanProgramType_XDP)
	case bpfman.ProgramTypeTC:
		return uint32(pb.BpfmanProgramType_TC)
	case bpfman.ProgramTypeTracepoint:
		return uint32(pb.BpfmanProgramType_TRACEPOINT)
	case bpfman.ProgramTypeKprobe, bpfman.ProgramTypeKretprobe:
		return uint32(pb.BpfmanProgramType_KPROBE)
	case bpfman.ProgramTypeUprobe, bpfman.ProgramTypeUretprobe:
		return uint32(pb.BpfmanProgramType_UPROBE)
	case bpfman.ProgramTypeFentry:
		return uint32(pb.BpfmanProgramType_FENTRY)
	case bpfman.ProgramTypeFexit:
		return uint32(pb.BpfmanProgramType_FEXIT)
	case bpfman.ProgramTypeTCX:
		return uint32(pb.BpfmanProgramType_TCX)
	default:
		return uint32(pb.BpfmanProgramType_XDP) // zero value
	}
}

func programIDsToStrings(ids []kernel.ProgramID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatUint(uint64(id), 10)
	}
	return out
}

// protoToPullPolicy converts a proto image pull policy to managed type.
// Proto values: 0=Always, 1=IfNotPresent, 2=Never.
func protoToPullPolicy(policy int32) (bpfman.ImagePullPolicy, error) {
	switch policy {
	case 0:
		return bpfman.PullAlways, nil
	case 1:
		return bpfman.PullIfNotPresent, nil
	case 2:
		return bpfman.PullNever, nil
	default:
		return "", fmt.Errorf("unknown pull policy: %d", policy)
	}
}

// protoImageAuth converts optional proto registry credentials to ImageAuth.
// Empty or incomplete credentials are treated as anonymous access, matching
// the Rust API boundary's empty-string normalisation.
func protoImageAuth(image *pb.BytecodeImage) *platform.ImageAuth {
	if image == nil || image.Username == nil || image.Password == nil {
		return nil
	}
	if *image.Username == "" || *image.Password == "" {
		return nil
	}
	return &platform.ImageAuth{
		Username: *image.Username,
		Password: *image.Password,
	}
}
