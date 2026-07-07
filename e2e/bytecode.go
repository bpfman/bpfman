//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
)

// BytecodeDir returns the directory that contains the
// testdata/bpf/ object tree. The e2e suite references BPF objects
// by their "testdata/bpf/<name>.bpf.o" path relative to this
// directory; the loader -- in-process for the package's own tests,
// or a separate `bpfman serve` for the gRPC suite -- opens the
// resulting file directly off disk.
//
// BPFMAN_E2E_BYTECODE_DIR overrides the location. Otherwise the
// current working directory is used, which is the e2e package
// directory under `go test ./e2e`. Precompiled-binary runs (the CI
// bundle, `make test-e2e`) set the override because their cwd is
// the repo root, not the package directory. The build system
// decides where the objects live, not the test -- mirroring
// resolveBpfmanBinary in the gRPC suite.
func BytecodeDir() string {
	if d := os.Getenv("BPFMAN_E2E_BYTECODE_DIR"); d != "" {
		return d
	}
	return "."
}

// BytecodePath resolves name (e.g. "testdata/bpf/xdp_pass.bpf.o")
// to an absolute path under BytecodeDir. Absolute so a path handed
// to a separate daemon process resolves regardless of that
// process's working directory.
func BytecodePath(name string) string {
	abs, err := filepath.Abs(filepath.Join(BytecodeDir(), name))
	if err != nil {
		// filepath.Abs only fails if the cwd is unreadable, in
		// which case the whole suite is already doomed; fall back
		// to the unresolved join rather than masking the real error
		// behind a panic here.
		return filepath.Join(BytecodeDir(), name)
	}

	return abs
}
