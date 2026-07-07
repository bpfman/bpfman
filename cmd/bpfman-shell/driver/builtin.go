// Builtin registry: the framework half of the bpfman-shell
// dispatcher. The driver package owns the types, the registry, and
// the lookup; each builtin handler lives in the cmd/bpfman-shell
// package and uses these types to declare itself.

package driver

import (
	"context"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// Ctx is the dispatch context handed to every builtin handler.
// Fat-context by design: the dependency superset across the
// dispatched builtins is small (Ctx, CLI, Env, Args, Pos, Span)
// and uniform plumbing keeps each handler's wrapper to a few lines.
// Session is reachable via Env.Session; handler-internal toggles
// (isRequire, origin-style) need no dedicated field.
type Ctx struct {
	// Ctx is the cancellation/deadline context for the call.
	// Plumbed from the dispatcher's caller; long-running
	// builtins (start, wait, kill's escalation wait, exec)
	// observe it.
	Ctx context.Context

	// CLI carries the stdout/stderr writers and the
	// PrintOut/PrintErrf helpers.
	CLI *cli.CLI

	// Env is the active shell environment. Env.Session is the
	// canonical handle to variable bindings and defs; jobs and
	// import read Env directly to register background processes
	// and to inherit the caller's scope.
	Env *runtime.Env

	// Cmd is the command name as the user typed it (args[0]
	// before slicing).
	Cmd string

	// Args is the argument list with the command name already
	// stripped.
	Args []runtime.Arg

	// Pos is the source location of the program statement this builtin was
	// dispatched from.
	Pos SourceLoc

	// Span is the source extent of the originating CommandStmt
	// or BindStmt.
	Span source.Span
}

// Category constants are lightweight builtin taxonomy metadata kept
// close to each handler declaration.
const (
	CategorySession = "session" // bindings, defs
	CategoryIO      = "io"      // external commands, file, jq, print
	CategoryJobs    = "jobs"    // start / wait / kill / jobs
)

// Builtin describes one entry in the registry: how to run it and
// how to describe itself to tooling. Pointer-free because the
// registry is read-only at runtime.
type Builtin struct {
	// Name is the command word that selects this builtin during
	// dispatch.
	Name string

	// Handler runs the builtin, receiving the dispatch Ctx and
	// returning the assignable primary value, or runtime.Value{}
	// when the builtin binds nothing.
	Handler func(Ctx) (runtime.Value, error)

	// Category groups the builtin in help output; it is one of the
	// Category* constants, or empty when ungrouped.
	Category string

	// Usage is the one-line syntax shown in help.
	Usage string

	// Summary is the one-line description shown in help.
	Summary string

	// Detail is the optional multi-paragraph long help.
	Detail string
}

// builtinRegistry is the dispatcher's source of truth. Populated
// at init() time by handler files in cmd/bpfman-shell via
// RegisterBuiltin. Read-only after init; no synchronisation
// needed.
var builtinRegistry = map[string]Builtin{}

// RegisterBuiltin adds a builtin to the dispatch and help
// registry. Duplicate names panic at startup because silently
// shadowing is the kind of bug that only surfaces months later.
func RegisterBuiltin(b Builtin) {
	if _, dup := builtinRegistry[b.Name]; dup {
		panic("driver: builtin " + b.Name + " registered twice")
	}
	builtinRegistry[b.Name] = b
}

// LookupBuiltin returns the registered builtin for the given
// name; the second return is false when no builtin matches.
func LookupBuiltin(name string) (Builtin, bool) {
	b, ok := builtinRegistry[name]
	return b, ok
}

// Builtins returns the read-only registry of builtins keyed by
// name. Callers must treat the map as immutable; mutate via
// RegisterBuiltin at init() time only.
func Builtins() map[string]Builtin { return builtinRegistry }
