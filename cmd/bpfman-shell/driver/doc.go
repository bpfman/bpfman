// Package driver is the framework layer that drives the shipped
// bpfman-shell DSL.
//
// It sits between:
//
//   - the phase packages under shell/ (syntax, check, lower, ir,
//     runtime, semantics), which define and execute the language
//   - the embedding binary in cmd/bpfman-shell, which supplies
//     app policy such as CLI mode selection, assertion reporting,
//     builtin registration, and the bpfman-specific fallback bridge
//
// "driver" therefore means orchestration rather than language
// semantics. The package owns:
//
//   - whole-program input handling and script-mode execution
//   - import expansion and frontend wiring
//   - parse-only inspection pipelines (--ast, --lowered)
//   - builtin dispatch registry types and lookup
//   - diagnostic/source-location helpers shared by the runner
//   - external command argv flattening and related runner utilities
//
// It deliberately does not own the language itself; the shell/*
// packages do. It also does not own product policy; cmd/bpfman-shell
// does.
package driver
