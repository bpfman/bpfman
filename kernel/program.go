package kernel

import "time"

// Program represents a BPF program loaded in the kernel, captured from
// cilium/ebpf's ProgramInfo. These values are observed, not created:
// bpfman reads them back from a program the kernel already holds.
//
// The Has* fields are availability discriminators for their companion
// field: when a HasX field is false the companion X was not reported by
// the running kernel (typically too old, or access was restricted) and
// its value is meaningless regardless of whether it is zero.
type Program struct {
	// ID is the kernel-assigned program ID, unique while the program is
	// loaded.
	ID ProgramID `json:"id"`

	// Name is the program name supplied at load time, truncated by the
	// kernel to BPF_OBJ_NAME_LEN-1 (15) bytes. Empty on kernels before 4.15.
	Name string `json:"name"`

	// ProgramType is the kernel program type (for example "xdp",
	// "schedcls", "kprobe", "tracing"), lowercased.
	ProgramType ProgramType `json:"program_type"`

	// Tag is the kernel-computed truncated hash of the program's
	// bytecode. Two programs with identical instructions share a tag.
	// Empty only when the kernel did not report it.
	Tag string `json:"tag"`

	// LoadedAt is the wall-clock time the program was loaded, normalised
	// to UTC second precision. The zero value means the kernel did not
	// report a load time (before kernel 4.15).
	LoadedAt time.Time `json:"loaded_at"`

	// UID is the user ID that loaded the program. Valid only when HasUID
	// is true.
	UID uint32 `json:"uid"`

	// HasUID reports whether UID was reported by the kernel (available
	// from kernel 4.15). When false, UID is meaningless.
	HasUID bool `json:"has_uid"`

	// BTFId is the ID of the BTF object associated with the program.
	// Valid only when HasBTFId is true.
	BTFId uint32 `json:"btf_id"`

	// HasBTFId reports whether BTFId was reported by the kernel (program
	// BTF is available from kernel 5.0). When false, BTFId is meaningless.
	HasBTFId bool `json:"has_btf_id"`

	// MapIDs holds the kernel IDs of the maps the program uses. Valid only
	// when HasMapIDs is true; an empty slice then means the program uses
	// no maps.
	MapIDs []MapID `json:"map_ids"`

	// HasMapIDs reports whether MapIDs was reported by the kernel
	// (available from kernel 4.15). When false, MapIDs is meaningless.
	HasMapIDs bool `json:"has_map_ids"`

	// JitedSize is the size, in bytes, of the program's JIT-compiled
	// machine code (the code actually run on the CPU). Zero when the JIT
	// is disabled, unsupported, or access is restricted.
	JitedSize uint32 `json:"jited_size"`

	// XlatedSize is the size, in bytes, of the program's translated
	// instructions after the verifier has rewritten them. Zero when
	// unsupported or when access is restricted (see Restricted).
	XlatedSize uint32 `json:"xlated_size"`

	// VerifiedInstructions is the number of instructions the verifier
	// processed when loading the program. Reported from kernel 5.16.
	VerifiedInstructions uint32 `json:"verified_insns"`

	// Memlock is the approximate kernel memory locked (accounted) for this
	// program, in bytes. Valid only when HasMemlock is true.
	Memlock uint64 `json:"memlock"`

	// HasMemlock reports whether Memlock was reported by the kernel
	// (available from kernel 4.10). When false, Memlock is meaningless.
	HasMemlock bool `json:"has_memlock"`

	// Restricted reports that the kernel withheld address-bearing
	// information (such as the translated instructions, hence XlatedSize)
	// because kernel.kptr_restrict and/or net.core.bpf_jit_harden are set.
	Restricted bool `json:"restricted"`
}

// PinnedProgram represents a BPF program discovered by scanning a bpffs
// pin directory. It carries a program's identity plus the path it was
// found at, used for CLI output rather than the full kernel Program.
type PinnedProgram struct {
	// ID is the kernel-assigned program ID, unique while the program is
	// loaded.
	ID ProgramID `json:"id"`

	// Name is the program name as reported by the kernel.
	Name string `json:"name"`

	// Type is the kernel program type, lowercased.
	Type ProgramType `json:"type"`

	// Tag is the kernel-computed truncated hash of the program's
	// bytecode. Empty only when the kernel did not report it.
	Tag string `json:"tag"`

	// PinnedPath is the bpffs path the program is pinned at.
	PinnedPath string `json:"pinned_path"`

	// MapIDs holds the kernel IDs of the maps the program uses; an empty
	// slice when the program has no maps.
	MapIDs []MapID `json:"map_ids"`
}
