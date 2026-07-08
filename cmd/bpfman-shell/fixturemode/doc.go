// Package fixturemode owns the BPFMAN_SHELL_MODE helper entry points.
//
// These modes bypass the normal script runner and turn
// bpfman-shell into small helper processes used by tests and
// fixtures. They are command-private support code for the binary,
// not part of the shell language itself. A mode is selected by the
// BPFMAN_SHELL_MODE environment variable; the remaining argv is the
// mode's own, dispatched by Run in mode.go.
//
// The modes:
//
//   - uprobe-fire-worker: a stable-PID worker that fires the cgo'd
//     bpfman_shell_uprobe_call_malloc symbol N times per wave, gated
//     by numbered sentinel/ack files. Scripts attach uprobes to the
//     bpfman-shell binary (or, via libc malloc, to the worker's
//     libc) and drive deterministic traffic through the wave
//     protocol. See uprobe.go.
//
//   - unlinkat-fire-worker: the same wave-protocol worker shape
//     firing unlinkat(2), for sys_enter/sys_exit_unlinkat
//     tracepoint fixtures. See unlinkat.go.
//
//   - kill-fire-worker: the wave-protocol worker shape firing
//     kill(2). See kill.go.
//
//   - ldso-cache-writer: not a worker; a one-shot generator that
//     writes a minimal new-format glibc ld.so.cache from
//     soname=path pairs (`bpfman-shell <outfile> <soname>=<path>...`
//     under the mode). Scripts install the result as a synthetic
//     /etc/ld.so.cache inside a target mount namespace to test
//     container uprobe library-name resolution: an entry that
//     exists in no host's cache proves resolution happened inside
//     the namespace. ldconfig cannot build such a cache because it
//     takes keys from ELF sonames, not file names. See ldsocache.go.
package fixturemode
