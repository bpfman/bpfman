// Package ebpf implements [platform.KernelOperations] using the
// cilium/ebpf library.
//
// # Overview
//
// This is the concrete I/O boundary for all BPF kernel interactions.
// It translates the abstract interfaces defined in platform/ into
// cilium/ebpf API calls: loading programs, attaching to hooks,
// enumerating kernel objects, managing pins, and handling netlink
// operations for TC.
//
// The package is organised by concern:
//
//   - ebpf.go: kernel adapter core, program/link/map enumeration and
//     lookup by ID
//   - load.go: program loading with global data injection, map
//     pinning, and ELF parsing
//   - pin.go: bpffs pin and unpin operations
//   - attach_xdp.go: XDP dispatcher and extension attachment
//   - attach_tc.go: TC dispatcher and extension attachment, netlink
//     filter management
//   - attach_tracing.go: tracepoint, kprobe, fentry/fexit attachment
//   - attach_uprobe.go: uprobe attachment for both local and
//     container namespaces
//   - convert.go: conversion from cilium/ebpf info types to kernel/
//     domain types
//   - program_info.go: program info extraction
//   - link_info.go: link info extraction with type-specific fields
//   - tracepoint.go: tracepoint existence validation
//
// # Container Uprobe
//
// Container uprobe attachment spawns a bpfman-ns helper process that
// enters the target container's mount namespace before creating the
// perf_event-based attachment. These links cannot be pinned to bpffs,
// so the file descriptor is stored in a sync.Map keyed by a unique
// identifier. The link remains active as long as the fd is held open.
//
// # TC Netlink
//
// TC dispatcher attachment uses legacy netlink operations (clsact
// qdisc creation and tc filter addition) rather than BPF links. This
// matches the upstream Rust bpfman approach and is visible to tc(8)
// tooling. Detachment removes the filter by ifindex, parent, priority,
// and handle.
//
// # Program Validation
//
// [NewProgramValidator] provides [platform.ProgramValidator] for
// checking requested program names against BPF object files. It
// opens ELF files using cilium/ebpf's spec parser and reports any
// requested name the object does not contain.
package ebpf
