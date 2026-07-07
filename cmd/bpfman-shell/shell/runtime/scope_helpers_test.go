package runtime

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"

// Test-only scope helpers. The production interpreter manages defer
// scopes through the IR EnterDeferScope/RunDefers instructions and runs
// programs via execProgram; the helpers below let the runtime tests drive
// a body inside a fresh defer scope and run a lowered program against a
// caller-owned scope, so defer-scope and job-leak behaviour can be pinned
// directly.

// withDeferScope runs fn inside a fresh defer scope, restoring the outer
// scope on return and executing every registered deferred statement in
// LIFO order regardless of fn's outcome.
func withDeferScope(env *Env, fn func() error) error {
	return runWithDeferScope(env, fn)
}

// runWithDeferScope establishes a defer scope around fn. The previous
// scope is saved and restored on exit so nested scopes compose; fn's
// error is returned verbatim and defer execution happens regardless of
// fn's outcome.
func runWithDeferScope(env *Env, fn func() error) error {
	saved := env.defers
	var stack []deferEntry
	env.defers = &stack
	bodyErr := fn()
	env.defers = saved
	_ = runDefers(env, stack)
	return bodyErr
}

// execInScope runs lp's body against env without opening a fresh
// program-level defer scope, so defers registered by the body append to
// the caller's scope (which the caller must have set on env.defers).
func execInScope(lp *ir.Program, env *Env) error {
	return execProgram(lp, env, true)
}
