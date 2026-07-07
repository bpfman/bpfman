// Fire-kind registry: the framework half of the `fire` builtin.
// FireKind metadata and the registration API live in the driver
// package; the actual fire-worker subprocess entry points live
// in cmd/bpfman-shell/fixturemode, each calling RegisterFireKind
// from init().

package driver

import "sort"

// FireKind describes one fire kind: the BPFMAN_SHELL_MODE value
// passed to the spawned bpfman-shell, a short summary for help
// and completion text, and a NeedsBinary flag that controls
// whether the resulting Job carries a target_binary path the
// script can read via $work.target_binary.
//
// NeedsBinary == true: the kind's correctness depends on the
// kernel attachment surface (uprobe targets a symbol in this
// binary), so target_binary is the running bpfman-shell ELF and
// carries the semantic guarantee. NeedsBinary == false: the
// kind's effect is purely a syscall or signal; target_binary is
// not exposed on the Job, and a script that tries to read it
// receives a runtime field error rather than a silent empty
// string.
type FireKind struct {
	// Mode is the BPFMAN_SHELL_MODE value passed to the spawned
	// bpfman-shell to select this fire worker.
	Mode string

	// Summary is the short description shown in help and
	// completion.
	Summary string

	// NeedsBinary reports whether the resulting Job carries a
	// target_binary path. When true the kind targets a symbol in
	// the running bpfman-shell ELF and exposes it as
	// $work.target_binary; when false the kind's effect is a syscall
	// or signal and target_binary is not exposed.
	NeedsBinary bool
}

// fireKinds is the registry of fire kinds. Populated by
// RegisterFireKind from per-worker init() blocks in
// cmd/bpfman-shell. Read-only after init.
var fireKinds = map[string]FireKind{}

// RegisterFireKind adds a fire kind to the registry. Duplicate
// names panic at startup because silently shadowing is the kind
// of bug that only surfaces months later.
func RegisterFireKind(name string, k FireKind) {
	if _, dup := fireKinds[name]; dup {
		panic("driver: fire kind " + name + " registered twice")
	}
	fireKinds[name] = k
}

// FireKinds returns the read-only registry of fire kinds keyed
// by name. Callers must treat the map as immutable.
func FireKinds() map[string]FireKind { return fireKinds }

// FireKindNames returns the registered names in stable sorted
// order. Used by the `fire` builtin's diagnostic message for
// unknown kinds and by completion.
func FireKindNames() []string {
	names := make([]string, 0, len(fireKinds))
	for n := range fireKinds {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
