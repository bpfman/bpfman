// Package runtime implements the executable part of the bpfman-shell
// language.
//
// It owns the dynamic value model, command-argument boundary, runtime
// session state, expression evaluation, matches evaluation, and the IR
// interpreter. The package is SANS-IO by construction: it does not spawn
// processes or print to terminals itself. Instead, it interprets syntax
// and IR against an [Env] whose callbacks provide the command, bind,
// assert, trace, and cleanup hooks supplied by the embedding driver.
//
// The surrounding phase split is:
//
//   - [source] for coordinates
//   - [syntax] for tokenisation, parsing, and AST
//   - [semantics] for shared shape/origin/type metadata
//   - [check] for static analysis
//   - [ir] for the executable IR model and dump surface
//   - [lower] for syntax -> IR lowering
//   - runtime for execution over values, sessions, and callbacks
//
// # Runtime Surface
//
// [Session] holds variables and defs across a run. [Value] wraps the
// JSON-compatible dynamic values the language manipulates, preserving
// typed origins where the runtime needs capability checks or structured
// follow-on access. [Arg] is the typed post-expansion command-argument
// boundary used between the interpreter and its embedders.
//
// [EvalExpr] evaluates lowered [ir.Expr] values. [Exec] runs [ir.Program]
// bodies against an [Env]. [Env] owns the embedding hooks; the runtime
// owns the execution semantics that call them.
package runtime
