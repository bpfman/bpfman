// Package manager provides high-level orchestration for BPF program
// lifecycle management using the fetch/compute/execute pattern.
//
// # Overview
//
// The manager is the primary API for loading, attaching, detaching, and
// querying BPF programs. It coordinates between three layers:
//
//   - Store: SQLite database for program and link metadata
//   - Kernel: cilium/ebpf adapter for BPF syscalls
//   - Filesystem: bpffs pins and bytecode persistence
//
// Callers include the gRPC server (server/) for daemon mode and the CLI
// (cmd/bpfman/) for direct invocation. Both delegate to Manager methods
// after acquiring the appropriate locks.
//
// # Actions and the executor
//
// The action package (manager/action/) defines a small instruction set
// for BPF lifecycle operations. Each action type is an opcode -- pure
// data describing what to do with typed operands. The executor
// (manager/executor.go) is the single interpreter: one type switch
// dispatches each opcode to the store, kernel, or filesystem. Plan
// builders never perform I/O directly; they construct actions and
// delegate to the executor.
//
// Execute runs an instruction for its side effect. ExecuteResult runs
// it and returns a value (e.g., LoadProgram produces LoadOutput). The
// generic action.Produce[T] wrapper provides compile-time type safety
// over the raw any return.
//
// # Fetch/Compute/Execute
//
// Mutating operations follow a phased pattern:
//
//  1. FETCH: gather state from store, kernel, and filesystem
//  2. COMPUTE: build a plan -- a sequence of nodes that emit actions
//  3. EXECUTE: the plan interpreter runs each node's action through
//     the executor, accumulates undo actions, and on failure rolls
//     back in reverse order
//
// The fetch phase runs before the plan is constructed. It gathers the
// inputs needed to determine which actions to emit. This phase may
// involve I/O (store queries, image pulls, program discovery) but
// produces no side effects that require rollback.
//
// One manager-level variation remains. AttachTCX() observes the
// existing TCX links on the interface and computes the new program's
// chain position before the kernel attach, because the order depends
// on that pre-attach link state. The dispatcher paths (AttachXDP(),
// AttachTC(), Detach()) instead push their cross-subsystem
// observe-decide-act into the rebuild executor actions
// (RebuildXDPDispatcher, RebuildTCDispatcher,
// RebuildDispatcherForDetach): the manager does only its lightweight
// fetch -- resolving the interface for an attach, extracting the
// dispatcher key for a detach -- then emits a single rebuild action
// that queries the dispatcher snapshot, recomputes membership, and
// persists atomically. At manager level those paths follow the
// standard phasing.
//
// Read-only methods (Get, ListPrograms, GetLink, etc.) are fetch-only
// and do not build plans, since they are purely observational.
//
// The platform layer (platform/) provides the I/O abstractions for
// BPF operations.
//
// # Atomic Load Model
//
// Load operations provide atomic semantics: either a program is fully
// loaded with metadata persisted, or nothing is left behind. The
// per-program load plan emits two actions through the executor:
//
//  1. LoadProgram -- load into kernel and pin to bpffs
//  2. PublishBytecode -- copy object file to per-program directory
//
// The store work -- verifying no stale row exists, persisting the
// metadata, and recording shared-map references -- is batched into a
// single sqlite transaction once every program's plan has run (see
// loadBody's phase B), so a whole multi-program batch commits or aborts
// together. These store steps run directly against that shared
// transaction rather than each opening its own.
//
// On failure the plan interpreter rolls back the completed per-program
// actions in reverse order (RemoveProgramDir, then UnloadProgram and
// RemoveMapsPins); a phase-B commit failure unwinds the batch by
// unloading each already-loaded program. On success, programs only
// appear in the store after the full sequence completes. Crash residue
// (kernel program plus bpffs pin plus bytecode directory without a
// matching store row) is benign: the kernel keeps the program alive
// while the pin holds, so the kernel ID cannot be recycled. Operators
// clean up manually with bpftool plus rm under the bytecode directory.
//
// # Rollback and Error Reporting
//
// Rollback operates at two scopes that compose cleanly.
//
// The plan interpreter (operation/run.go) handles rollback across
// actions. Each plan node may declare undo actions via UndoFrom.
// When a node fails, the interpreter walks previously
// completed nodes in reverse order and executes their undo actions.
// This is the inter-step scope: it ensures that a multi-step
// operation either completes fully or leaves no partial artefacts
// from earlier steps.
//
// The executor handles rollback within a single action. Deep actions
// such as RebuildXDPDispatcher and RebuildTCDispatcher perform a
// mini-transaction internally: kernel I/O followed by a store
// persist. If the persist fails, the executor rolls back the kernel
// artefacts before returning an error. The plan interpreter never
// sees the partial internal state -- it receives a clean error and
// undoes earlier nodes as usual.
//
// The two scopes nest: if a deep action fails internally, its inline
// rollback cleans up within that action, then the plan interpreter
// undoes any earlier nodes that succeeded. Failed mutating
// operations (Load, Unload, Attach*, Detach*) return plain errors.
// Rollback failures are logged but do not alter the returned error.
//
// # Attachment Types
//
// The manager supports multiple BPF attachment points:
//
//   - XDP: network interface ingress via dispatcher programs
//   - TC: traffic control ingress/egress via dispatchers
//   - TCX: traffic control ingress/egress using native kernel multi-prog (no dispatchers)
//   - Tracepoint: kernel tracepoints (sched/sched_switch, etc.)
//   - Kprobe/Kretprobe: kernel function entry/return
//   - Uprobe/Uretprobe: userspace function entry/return
//   - Fentry/Fexit: fast kernel function tracing
//
// XDP and TC attachments use dispatcher programs that chain multiple
// extension programs at a single attach point. The dispatcher state
// is tracked in the store.
//
// # Concurrency
//
// Mutating manager methods (Unload, Attach*, Detach*) require the
// caller to hold the cross-process writer flock provided by the
// lock package (lock/); the server takes the flock per request and
// passes the writer scope into the manager call. Read-only methods
// (Get, ListPrograms, GetLink, ListLinks) are not gated by the
// flock and may be called concurrently with mutators. Load is the
// other caller-lockless path: it manages its own conditional flock
// acquisition for explicit map-owner joins and LIBBPF_PIN_BY_NAME
// maps, and otherwise runs without the flock; see the package-level
// comments on Manager.Load for the safety argument.
//
// # Dependencies
//
// Create a Manager via New(), providing:
//
//   - Runtime: capability token proving bpffs is mounted
//   - Store: database interface (platform.Store)
//   - KernelOperations: BPF syscall adapter
//   - ProgramValidator: program-name validation against object files
//   - ImagePuller: optional OCI image puller for container images
//   - Logger: structured logger with op_id support
//
// The Runtime is obtained from fs/runtime.New() after
// ensuring directories exist and bpffs is mounted.
package manager
