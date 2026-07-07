// Package dispatcher provides XDP and TC dispatcher programs for
// multi-program chaining at a single network attach point.
//
// # Problem
//
// The Linux kernel allows only one XDP program per interface and one
// TC classifier per (interface, direction). Dispatchers solve this
// by interposing a single BPF program that chains calls to up to 10
// user programs through a single attachment.
//
// # Mechanism
//
// A dispatcher is a BPF program with 10 stub functions (prog0
// through prog9). User programs attach as BPF extensions
// (BPF_PROG_TYPE_EXT / freplace) that replace these stubs. The
// dispatcher calls each enabled slot in order; a per-slot
// proceed-on bitmask controls whether to continue or return after
// each call. Configuration is baked into .rodata at load time;
// changing it requires loading a new dispatcher instance.
//
// # Dispatcher types
//
//   - XDP ([DispatcherTypeXDP]): attached via a kernel BPF link,
//     atomically updatable across revisions.
//   - TC ingress ([DispatcherTypeTCIngress]) and TC egress
//     ([DispatcherTypeTCEgress]): attached via netlink tc filter.
//
// # Rebuild model
//
// Every attach or detach triggers a full rebuild: load a new
// dispatcher with updated .rodata, re-attach ALL extensions to it,
// atomically swap the interface attachment, persist everything in a
// single transaction, then remove the old revision's pins.
//
// The critical consequence is that every extension link gets a new
// kernel link ID on every rebuild. The bpfman link ID is the stable
// management handle for the dispatcher member; the captured kernel
// link ID is refreshed from the newly-created extension link during
// each snapshot replacement.
//
// # Store invariant
//
// Dispatcher snapshot replacement must preserve existing bpfman link
// IDs and overwrite captured kernel link IDs. New members receive
// bpfman link IDs from the store in the same transaction that persists
// the snapshot.
//
// # Types
//
// [Key] identifies a dispatcher by (Type, Nsid, Ifindex). [State]
// records runtime state: revision, kernel program ID, kernel link ID
// (XDP only), and TC filter priority.
//
// [XDPConfig] and [TCConfig] define .rodata structures.
// [LoadXDPDispatcher] and [LoadTCDispatcher] inject configuration
// into embedded BPF bytecode and return a CollectionSpec.
//
// The extension spec types ([XDPExtensionAttachSpec],
// [TCExtensionAttachSpec]) are value objects describing attachment
// parameters.
package dispatcher
