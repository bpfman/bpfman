// Package kernel contains pure data types representing BPF objects as
// observed from the kernel.
//
// # Overview
//
// These types model BPF programs, links, maps, and runtime statistics
// as read-only snapshots of kernel state. They are populated by
// querying the BPF subsystem (via platform/ebpf/) and carry no
// behaviour beyond accessor methods. The package performs no I/O.
//
// The types wrap cilium/ebpf's info types to decouple the rest of the
// codebase from that dependency while preserving all available
// information from the kernel. Optional field availability is
// indicated by Has* fields where the kernel version determines
// whether information is present.
//
// # Core Types
//
//   - [Program]: a loaded BPF program with identity, ownership, size,
//     and memory information
//   - [Link]: a BPF link with type-specific fields (tracepoint, XDP,
//     TC, kprobe, cgroup, netfilter, etc.)
//   - [Map]: a BPF map with structure, BTF, and memory information
//   - [ProgramStats]: runtime statistics (run count, runtime duration,
//     recursion misses); requires kernel.bpf_stats_enabled=1
//
// # Pinned Types
//
// [PinnedProgram] and [PinnedMap] represent objects pinned on the BPF
// filesystem, extending the core types with a filesystem path.
// [PinDirContents] aggregates all pinned objects found in a directory
// scan.
//
// # Type Aliases
//
// [ProgramType] and [MapType] are normalised string types constructed
// via [NewProgramType] and [NewMapType], which enforce lowercase for
// consistent comparison.
//
// # Dependency Flow
//
// kernel/ is a pure leaf package. It is imported by the root bpfman
// package, platform/, platform/ebpf/, inspect/, and manager/.
// It never imports from those packages.
package kernel
