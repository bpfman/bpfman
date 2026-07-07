package check

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"

// CheckImportLibrary validates an importable file with no caller-visible
// defs. Test-only: production import expansion calls
// CheckImportLibraryWithDefs directly (see the driver frontend). This is
// the no-defs convenience the check tests use.
func CheckImportLibrary(prog *syntax.Program) []Issue {
	return CheckImportLibraryWithDefs(prog, nil)
}
