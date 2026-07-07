// Package inspect provides a correlated view of bpfman's state across
// store, kernel, and filesystem.
//
// # Overview
//
// BPF objects can exist in three places independently: the SQLite
// store (what bpfman manages), the kernel (what is actually loaded),
// and the bpffs filesystem (where pins live). Objects can drift
// between these sources -- a program may be in the store but missing
// from the kernel if it was unloaded externally, or pinned on the
// filesystem but absent from the store if bpfman crashed mid-load.
//
// This package correlates objects across all three sources and
// annotates each with a [Presence] indicating where it exists. CLI
// commands, the coherency engine, and diagnostic tools use this to
// understand the actual state of the system.
//
// # Snapshot
//
// [Snapshot] builds a point-in-time [Observation] by reading from all three
// sources. The returned Observation contains every known BPF
// object, correlated by kernel ID:
//
//   - [ProgramView]: programs with store record, kernel info, and
//     filesystem pin status
//   - [LinkRow]: links with store record and kernel presence
//   - [DispatcherRow]: dispatchers with program and link presence
//
// Use [Observation.ManagedPrograms], [Observation.ManagedLinks], and
// [Observation.ManagedDispatchers] for the store-first view (objects that
// bpfman is actively managing).
//
// # Targeted Lookups
//
// [GetLink] performs targeted single-object lookups, which are more
// efficient than a full Snapshot when only one object is needed.
//
// # Interfaces
//
// The package defines narrow interfaces ([StoreLister],
// [KernelLister], [LinkGetter], [KernelLinkGetter]) to accept only
// the methods it needs from the store and kernel adapters.
package inspect
