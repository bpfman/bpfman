// Package check runs static analysis over parsed bpfman-shell
// programs.
//
// It sits after syntax and before lowering/runtime, catching
// problems that should fail before any script side effects fire:
// undefined variables, arity mismatches, invalid control-flow
// placement, shape mistakes against sealed kinds, and similar
// whole-program issues.
//
// The package reports issues against syntax trees; it does not
// lower or execute programs.
package check
