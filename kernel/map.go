package kernel

import "strings"

// Map represents a BPF map in the kernel, captured from cilium/ebpf's
// MapInfo. The Has* fields are availability discriminators for their
// companion field: when a HasX field is false the companion X was not
// reported by the running kernel (typically too old) and its value is
// meaningless regardless of whether it is zero.
type Map struct {
	// ID is the kernel-assigned map ID, unique while the map is loaded.
	ID MapID `json:"id"`

	// Name is the map name supplied at load time, truncated by the kernel
	// to BPF_OBJ_NAME_LEN-1 (15) bytes. Empty on kernels before 4.15.
	Name string `json:"name"`

	// MapType is the kernel map type (for example "hash", "array",
	// "lruhash", "percpuarray"), lowercased.
	MapType MapType `json:"map_type"`

	// KeySize is the size of each map key, in bytes.
	KeySize uint32 `json:"key_size"`

	// ValueSize is the size of each map value, in bytes.
	ValueSize uint32 `json:"value_size"`

	// MaxEntries is the maximum number of entries the map can hold. Its
	// exact meaning is map-type-specific.
	MaxEntries uint32 `json:"max_entries"`

	// Flags is the bitmask of BPF map-creation flags (BPF_F_*) supplied
	// when the map was created, such as BPF_F_NO_PREALLOC or BPF_F_RDONLY.
	Flags uint32 `json:"flags"`

	// BTFId is the ID of the BTF object describing the map's key and value
	// types. Valid only when HasBTFId is true.
	BTFId uint32 `json:"btf_id"`

	// HasBTFId reports whether BTFId was reported by the kernel (BTF map
	// info is available from kernel 4.18). When false, BTFId is meaningless.
	HasBTFId bool `json:"has_btf_id"`

	// MapExtra is an opaque, map-type-specific value supplied at creation
	// (for a bloom-filter map, for example, the number of hash functions).
	// Valid only when HasMapExtra is true.
	MapExtra uint64 `json:"map_extra"`

	// HasMapExtra reports whether MapExtra was reported by the kernel
	// (available from kernel 5.16). When false, MapExtra is meaningless.
	HasMapExtra bool `json:"has_map_extra"`

	// Memlock is the approximate kernel memory locked (accounted) for this
	// map, in bytes. Valid only when HasMemlock is true.
	Memlock uint64 `json:"memlock"`

	// HasMemlock reports whether Memlock was reported by the kernel
	// (available from kernel 4.10). When false, Memlock is meaningless.
	HasMemlock bool `json:"has_memlock"`

	// Frozen reports whether the map has been frozen, making it read-only
	// to user space (BPF_MAP_FREEZE). Requires kernel 5.2+ and procfs
	// access; always false when the kernel does not support freezing.
	Frozen bool `json:"frozen"`
}

// PinnedMap represents a BPF map discovered by scanning a bpffs pin
// directory. It carries the map's structural attributes plus the path
// it was found at, rather than the full kernel Map.
type PinnedMap struct {
	// ID is the kernel-assigned map ID, unique while the map is loaded.
	ID MapID `json:"id"`

	// Name is the map name as reported by the kernel.
	Name string `json:"name"`

	// Type is the kernel map type, lowercased.
	Type MapType `json:"type"`

	// KeySize is the size of each map key, in bytes.
	KeySize uint32 `json:"key_size"`

	// ValueSize is the size of each map value, in bytes.
	ValueSize uint32 `json:"value_size"`

	// MaxEntries is the maximum number of entries the map can hold.
	MaxEntries uint32 `json:"max_entries"`

	// PinnedPath is the bpffs path the map is pinned at.
	PinnedPath string `json:"pinned_path"`
}

// PinDirContents holds the BPF objects found by scanning a single pin
// directory: every entry that loaded as a program or, when requested, a
// map.
type PinDirContents struct {
	// Programs holds the pinned programs found in the directory; an empty
	// slice when none were found.
	Programs []PinnedProgram `json:"programs"`

	// Maps holds the pinned maps found in the directory; an empty slice
	// when none were found or when map scanning was not requested.
	Maps []PinnedMap `json:"maps"`
}

// LoadResult contains the result of loading a program via the CLI: the
// pinned program, its pinned maps, and the directory they were pinned
// under.
type LoadResult struct {
	// Program is the loaded, pinned program.
	Program PinnedProgram `json:"program"`

	// Maps holds the program's pinned maps; an empty slice when the
	// program has no maps.
	Maps []PinnedMap `json:"maps"`

	// PinDir is the bpffs directory the program and its maps were pinned
	// under.
	PinDir string `json:"pin_dir"`
}

// IsInternalMapName reports whether a map name denotes a libbpf-internal
// map -- the maps materialised from a program's ELF data sections
// (.rodata, .data, .bss) and from .kconfig, rather than maps the author
// declared. libbpf names them with a leading dot. They are an
// implementation detail of the object file: the load path does not pin
// them and they are not shared by name, so callers filter on this when
// deciding what to pin, share, or report.
func IsInternalMapName(name string) bool {
	return strings.HasPrefix(name, ".")
}
