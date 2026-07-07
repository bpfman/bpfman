// tempdir creates a private temporary directory and returns a
// structured value carrying the absolute path on its .path field.
// The motivating use case is e2e fixtures that need per-invocation
// sentinel/ack paths so parallel script instances cannot collide
// on shared `/tmp/foo.go`-style paths; the same shape covers any
// future test that needs a private scratch directory.
//
// Lifecycle is caller-driven: pair with `defer rm -rf $wd.path` for
// the canonical cleanup. tempdir does not register itself in any
// auto-cleanup mechanism, which keeps the builtin small and matches
// the existing `net veth-pair` / `fire` / `start` pattern of
// "returns a handle; caller defers the teardown".
package builtins

import (
	"fmt"
	"os"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "tempdir",
		Handler:  HandleTempdir,
		Category: driver.CategoryIO,
		Usage:    "tempdir <prefix>",
		Summary:  "Create a private temp directory; primary carries .path (assignable).",
		Detail: "Wraps os.MkdirTemp under the OS default temp dir. <prefix> names " +
			"the leading component (so concurrent runs are distinguishable in " +
			"ls /tmp); the random suffix guarantees uniqueness. Cleanup is the " +
			"caller's responsibility -- pair with 'defer rm -rf $wd.path' for " +
			"the canonical lifecycle. Use this in place of hard-coded /tmp " +
			"paths whenever a script may run concurrently with itself, since " +
			"shared paths race on rm/touch operations across instances.",
	})
}

// HandleTempdir is the registry handler for `tempdir PREFIX`.
// PREFIX names the directory's leading component so concurrent
// invocations can be told apart in `ls /tmp`; os.MkdirTemp appends
// a random suffix to guarantee uniqueness. The directory is the
// caller's to clean up.
func HandleTempdir(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) != 1 {
		return runtime.Value{}, fmt.Errorf("tempdir: requires exactly one PREFIX argument")
	}
	prefix := strings.TrimSpace(driver.ArgText(c.Args[0]))
	if prefix == "" {
		return runtime.Value{}, fmt.Errorf("tempdir: PREFIX must not be empty")
	}
	dir, err := os.MkdirTemp("", prefix+".*")
	if err != nil {
		return runtime.Value{}, fmt.Errorf("tempdir: %w", err)
	}
	return runtime.ValueFromMap(map[string]any{"path": dir}), nil
}
