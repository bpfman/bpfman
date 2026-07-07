// Package bpfman provides types and interfaces for BPF program management.
// This is the root package containing shared domain types used across
// the client, manager, and server components.
//
// # Program Type Overview
//
// The core types form a hierarchy reflecting the program lifecycle:
//
//	Program             - The complete domain object combining Record and Status
//	+-- ProgramRecord   - DB-backed stored record (what bpfman manages)
//	|   +-- LoadSpec    - Validated load request (immutable, private fields)
//	|   +-- License/GPLCompatible - Discovered from ELF at load time
//	|   +-- Handles     - Filesystem paths (pin, maps)
//	|   +-- Meta        - User-facing metadata (name, owner, labels)
//	+-- ProgramStatus   - Observed runtime state (kernel + filesystem-derived paths)
//	    +-- Kernel      - Live kernel program info (nil if not present)
//	    +-- ProgPin     - Program pin path (derived from program ID)
//	    +-- MapDir      - Map pin directory (derived)
//	    +-- Bytecode    - Bytecode file path (derived)
//	    +-- Links       - Attached links with their own record/status
//	    +-- Maps        - Associated kernel maps with pin correlation
//
// # Program Lifecycle Flow
//
// 1. LoadSpec: User provides validated input describing what to load.
// Created via NewLoadSpec() or builder methods. Immutable after construction.
//
// 2. LoadOutput: Transient result from kernel.Load(). Contains kernel-assigned
// ID, pin paths, and license discovered from ELF. Not stored - just passes
// data from I/O boundary to manager.
//
// 3. ProgramRecord: Manager combines LoadSpec + LoadOutput + user metadata and
// stores it in the database. License and GPLCompatible live here because
// they are discovered properties, not part of the original load request.
//
// 4. ProgramStatus: Observed by querying kernel and filesystem. Represents
// what actually exists now - can diverge from Record if programs are unloaded
// externally or pin files are deleted.
//
// 5. Program: Combines Record + Status. The coherency and GC systems compare
// these to detect drift and generate remediation actions.
//
// # Link Type Overview
//
// Links follow a parallel pattern to programs:
//
//	Link              - The complete domain object combining Record and Status
//	+-- LinkRecord    - DB-backed stored record (what bpfman manages)
//	|   +-- ID        - bpfman management handle allocated by the store
//	|   +-- ProgramID - The program this link attaches
//	|   +-- KernelLinkID - Optional captured kernel bpf_link ID
//	|   +-- Kind      - Link type (tracepoint, kprobe, xdp, tc, etc.)
//	|   +-- PinPath   - Optional bpffs pin path
//	|   +-- Details   - Type-specific details (sealed interface)
//	+-- LinkStatus    - Observed runtime state
//	    +-- Kernel    - Live kernel link info, if captured and still present
//	    +-- KernelSeen - Whether kernel enumeration found the link
//	    +-- PinPresent - Whether the pin path exists on filesystem
//
// # Link Lifecycle Flow
//
// 1. *AttachSpec (e.g., TracepointAttachSpec): User provides validated input
// describing what to attach. Each attach type has its own spec type containing
// the program ID and type-specific parameters.
//
// 2. AttachOutput: Transient result from kernel attach operation. Contains
// the captured kernel bpf_link ID, kernel link info, and actual pin path, if
// those exist. Not stored directly - just passes data from the I/O boundary
// to the manager.
//
//	AttachOutput {
//	    KernelLinkID *kernel.LinkID // captured kernel bpf_link ID, if any
//	    KernelLink   *kernel.Link   // nil when no kernel ID was captured
//	    PinPath      LinkPath       // actual bpffs pin, empty if none
//	}
//
// 3. LinkSpec: Manager combines *AttachSpec + AttachOutput into a store input
// with no bpfman link ID. The store creates the LinkRecord and allocates the
// bpfman management handle.
//
// 4. LinkRecord: Stored output from CreateLink or a dispatcher snapshot
// replacement. The bpfman ID is distinct from the optional captured kernel
// bpf_link ID.
//
// 5. LinkStatus: Observed by querying kernel and filesystem. A nil
// KernelLinkID means bpfman did not capture a kernel bpf_link ID for this
// attachment; PinPath is nil unless a real bpffs pin was created.
//
// 6. Link: Combines Record + Status. The coherency paths compare these to
// detect drift and generate remediation actions.
//
// # Key Distinctions
//
// LoadSpec is input (what to load), ProgramRecord is stored output (what was
// loaded). They share some fields but serve different purposes.
//
// Similarly, *AttachSpec is user input, LinkSpec is store input, and LinkRecord
// is stored output. AttachOutput bridges the I/O boundary, carrying observed
// kernel IDs and pin paths to the manager without assigning bpfman handles.
package bpfman
