package ebpf

import (
	"errors"
	"time"

	"github.com/cilium/ebpf"

	"github.com/bpfman/bpfman/kernel"
)

// ToKernelProgram converts a cilium/ebpf ProgramInfo to a kernel.Program.
// The Load and Get paths both call this so the wire shape stays identical
// across a round-trip.
func ToKernelProgram(info *ebpf.ProgramInfo) *kernel.Program {
	if info == nil {
		return nil
	}

	id, _ := info.ID()
	uid, hasUID := info.CreatedByUID()
	btfID, hasBTFID := info.BTFID()
	memlock, hasMemlock := info.Memlock()
	loadTime, hasLoadTime := info.LoadTime()
	verifiedInsns, _ := info.VerifiedInstructions()
	jitedSize, _ := info.JitedSize()

	// xlated size reads as ErrRestrictedKernel when kptr_restrict and
	// bpf_jit_harden hide kernel-address information; surface that as
	// Restricted=true rather than swallowing it like the other reads.
	var restricted bool
	xlatedSize, err := info.TranslatedSize()
	if errors.Is(err, ebpf.ErrRestrictedKernel) {
		restricted = true
	}

	// LoadedAt is normalised to UTC second precision so it stays
	// comparable with record.created_at (same shape) and so the
	// Load and Get round-trip produces identical strings rather
	// than local-tz nanosecond drift on either side of the wire.
	var loadedAt time.Time
	if hasLoadTime {
		loadedAt = bootTime().Add(loadTime).UTC().Truncate(time.Second)
	}

	ebpfMapIDs, hasMapIDs := info.MapIDs()
	var mapIDs []kernel.MapID
	if hasMapIDs {
		mapIDs = make([]kernel.MapID, len(ebpfMapIDs))
		for i, mid := range ebpfMapIDs {
			mapIDs[i] = kernel.MapID(mid)
		}
	}

	return &kernel.Program{
		ID:                   kernel.ProgramID(id),
		Name:                 info.Name,
		ProgramType:          kernel.NewProgramType(info.Type.String()),
		Tag:                  info.Tag,
		LoadedAt:             loadedAt,
		UID:                  uid,
		HasUID:               hasUID,
		BTFId:                uint32(btfID),
		HasBTFId:             hasBTFID,
		MapIDs:               mapIDs,
		HasMapIDs:            hasMapIDs,
		JitedSize:            jitedSize,
		XlatedSize:           uint32(xlatedSize),
		VerifiedInstructions: verifiedInsns,
		Memlock:              memlock,
		HasMemlock:           hasMemlock,
		Restricted:           restricted,
	}
}
