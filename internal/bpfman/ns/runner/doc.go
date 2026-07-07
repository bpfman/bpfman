// Package runner implements the child-side bpfman-ns command.
//
// Runner code is entered after internal/bpfman/ns has already switched the
// process into the target mount namespace from its C constructor. It should not
// call setns itself. Its job is to parse the private bpfman-ns arguments,
// verify the inherited writer scope, open the target binary -- whose path
// resolves only in the container namespace -- and return its fd to the parent.
// The writer scope is inherited from the parent manager operation because the
// open is part of a serialised manager operation and must remain serialised
// with other bpfman operations.
//
// The runner performs no BPF work. The parent, back in bpfman's own namespace,
// reaches the returned fd through /proc/self/fd and creates and pins the
// perf-event bpf_link there. Doing the link create in bpfman's namespace rather
// than the container's is deliberate: Cilium's bpf-perf-link feature probe is
// reliable there, so the parent gets a pinnable bpf_link instead of the
// ioctl-style perf-event attachment that cannot be pinned.
package runner
