// Package builtins holds the shipped command and pure-builtin
// mechanisms for bpfman-shell.
//
// These are app-private helpers for the embedding binary, not part of
// the reusable language phases under shell/. The package owns the
// concrete builtin handlers, any builtin-private support code, and
// registration of those handlers with the driver registry.
package builtins
