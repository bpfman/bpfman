// Package ns provides bpfman's private bpfman-ns transport.
//
// This package is not a general namespace library. It owns the wire contract
// used when bpfman needs a short-lived child process to attach a uprobe within
// a target process's mount namespace.
//
// The most important detail is ordering: when _BPFMAN_MNT_NS is present, the C
// constructor in nsexec.c calls setns(CLONE_NEWNS) before the Go runtime starts
// and before any package init or main function runs. Code in the child-side
// runner therefore starts life already inside the target mount namespace.
//
// This package is also the single source of truth for the bpfman-ns inherited
// file descriptor contract. The parent-side attach code passes a single
// ExtraFiles entry, the Unix socket, at the slot named by SocketFD: fd 3. The
// child opens the target binary in the container namespace and returns its fd
// over that socket; the parent then attaches and pins the uprobe in its own
// namespace via /proc/self/fd of the received fd. The writer-lock fd is also
// inherited, but its position is communicated separately via the
// lock.WriterLockFDEnvVar environment variable because its ExtraFiles slot is
// not fixed. The lock inheritance is part of the correctness contract: the
// child opens the target as part of a serialised manager operation, so it must
// run under the same writer scope rather than as an uncoordinated side process.
// ModeEnvVar, ModeBPFManNS, MntNsEnvVar, and the fd constants are part of the
// same private protocol and must stay in step with the parent and runner.
//
// The child command implementation lives in internal/bpfman/ns/runner. Keeping
// it separate lets parent-side attach code import this transport package
// without also importing the argument parser and uprobe attach implementation.
// bpfman-ns is not a separate binary or CLI: the parent re-execs the current
// executable with BPFMAN_MODE=bpfman-ns set, and the runner recognises that
// mode before the normal command line is parsed.
package ns
