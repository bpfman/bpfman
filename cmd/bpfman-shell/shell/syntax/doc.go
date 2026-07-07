// Package syntax owns the surface language of bpfman-shell.
//
// It defines:
//
//   - source-level tokens and tokenisation
//   - the parsed AST
//   - keyword metadata
//   - parser and source-formatting helpers
//   - AST inspection and dump helpers
//
// syntax stops at the surface tree. Static analysis, lowering,
// IR execution, and runtime semantics live in sibling packages.
package syntax
