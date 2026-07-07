// Package shell is the umbrella namespace for the bpfman-shell language
// phases.
//
// Production code should import the concrete phase packages directly:
//
//   - [source] for positions and spans
//   - [syntax] for tokenisation, parsing, AST, and keyword metadata
//   - [semantics] for shared shape/origin/type metadata
//   - [check] for static analysis
//   - [ir] for the executable IR model and dump surface
//   - [lower] for syntax -> IR lowering
//   - [runtime] for execution over values, sessions, and callbacks
//
// The umbrella package intentionally exports no production API of its own.
// It exists as the architectural root for the language subsystem and to
// host package-scoped documentation. The surface-language reference lives
// in the parent directory: GRAMMAR-REFERENCE.md (syntax), LANGUAGE-SPEC.md
// (semantics), and GRAMMAR-RATIONALE.md (design rationale and parser
// gotchas).
package shell
