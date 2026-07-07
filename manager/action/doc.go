// Package action defines the instruction set for BPF lifecycle
// operations.
//
// # Actions as opcodes
//
// Each Action type is an opcode in a small, domain-specific virtual
// machine. An action is pure data: a tagged instruction with typed
// operands that describes what to do, never how. The executor
// (manager/executor.go) is the single interpreter that dispatches
// each opcode to the appropriate I/O subsystem -- store, kernel, or
// filesystem.
//
//   - Store:      GetProgramFromStore, CreateLink, CreatePendingLink,
//     DeleteLink, FinaliseLink
//   - Kernel:     LoadProgram, UnloadProgram, DetachLink, RemoveMapsPins,
//     AttachTracepoint, AttachKprobe, AttachTCX,
//     AttachUprobeLocal, AttachUprobeContainer, AttachFentry,
//     AttachFexit
//   - Filesystem: PublishBytecode, RemoveProgramDir,
//     RemoveDispatcherRevDir
//   - Rebuild:    RebuildXDPDispatcher, RebuildTCDispatcher,
//     RebuildDispatcherForDetach, RemoveDispatcher
//
// Rebuild actions are cross-subsystem operations that the executor
// handles internally (kernel + store with inline rollback). They
// encapsulate a full dispatcher rebuild so the plan interpreter sees
// a single atomic action rather than multiple steps.
//
// Execute runs an instruction for its side effect. ExecuteResult
// runs it and returns a typed value (used by LoadProgram and the
// Attach* actions which produce AttachOutput). The generic
// Produce[T] wrapper provides compile-time type safety over the
// raw any return.
//
// # Plans compose opcodes
//
// Plan nodes (operation package) compute which opcodes to emit based
// on bindings from earlier results, and declare undo opcodes for
// rollback. The plan interpreter walks the nodes, executes each
// instruction via the executor, accumulates undo instructions, and
// on failure reverses and executes them. Plan builders never perform
// I/O directly; they construct actions and delegate.
//
// # Adding new actions
//
// Define a new struct with typed operands, implement the isAction
// marker method, and add a case to the executor's type switch.
// There is intentionally one switch: all I/O interpretation lives
// in one place, so changes to how an operation is performed require
// editing one function.
package action
