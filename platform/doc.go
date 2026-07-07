// Package platform defines the I/O boundary interfaces for bpfman.
//
// All side effects flow through interfaces defined here. The manager
// never performs I/O directly; it depends on these abstractions,
// keeping business logic testable without a real kernel or database.
//
// # Store Interfaces
//
// The [Store] interface composes narrow, single-responsibility
// interfaces for database access:
//
//   - [ProgramReader], [ProgramWriter], [ProgramLister]: CRUD for
//     program records keyed by kernel ID
//   - [LinkReader], [LinkWriter], [LinkLister]: CRUD for link records
//     with type-specific detail tables
//   - [DispatcherStore]: XDP/TC dispatcher state management
//   - [MapOwnershipReader]: map sharing dependency queries
//   - [Transactional]: atomic multi-operation commits
//
// This composition enables narrow dependency injection: a function
// that only reads programs accepts [ProgramReader] rather than the
// full [Store].
//
// The concrete implementation lives in platform/store/sqlite/.
//
// # Kernel Interfaces
//
// The [KernelOperations] interface composes the kernel-side
// abstractions:
//
//   - [KernelSource]: enumerate and query kernel BPF objects
//   - [ProgramLoader]: load BPF programs and pin to bpffs
//   - [ProgramUnloader]: unpin and unload programs
//   - [ProgramAttacher]: attach programs to tracepoints, kprobes,
//     uprobes, fentry/fexit hooks
//   - [DispatcherAttacher]: attach XDP/TC dispatchers and extensions,
//     attach TCX programs
//   - [LinkDetacher]: remove link pins from bpffs
//   - [PinRemover]: remove arbitrary bpffs pins
//   - [PinInspector]: inspect pinned objects
//   - [TCFilterDetacher]: remove legacy TC filters via netlink
//   - [MapRepinner]: re-pin maps to new locations (used by CSI)
//
// The concrete implementation lives in platform/ebpf/, backed by
// cilium/ebpf.
//
// # Image Interfaces
//
// [ImagePuller] fetches BPF bytecode from OCI container images.
// [SignatureVerifier] checks image signatures. [ImageRef] and
// [PulledImage] describe the request and result. [ProgramValidator]
// checks requested program names against ELF object files.
//
// Implementations live in platform/image/oci/ and
// platform/image/verify/.
//
// # Dependency Flow
//
// The platform package sits between the manager and the concrete I/O
// implementations:
//
//	manager/ -> platform/ -> platform/store/sqlite/
//	                      -> platform/ebpf/
//	                      -> platform/image/oci/
//
// Pure packages (kernel/, action/) never import platform/.
package platform
