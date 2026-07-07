// Package ir defines the canonical lowered intermediate
// representation of bpfman-shell programs.
//
// The package owns the IR data model and IR-oriented helpers such as
// dumping/source formatting. It does not parse source text or execute
// programs directly; lowering lives in shell/lower and execution
// lives in shell/runtime.
package ir
