// lsm is the e2e built-in for deterministic LSM file_open stimulus. It
// leases a probe -- a unique marker comm plus a target file -- and fires
// opens of that file under the marker, so an LSM program filtering
// file_open by the marker comm counts exactly the fixture's opens,
// isolated from the host's own file activity. The marker crosses into
// the program at load time via the probe's marker_hex global.
package builtins

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func init() {
	Register(driver.Builtin{
		Name:     "lsm",
		Handler:  handleLsm,
		Category: driver.CategoryJobs,
		Usage:    "lsm probe  |  lsm fire $probe N",
		Summary:  "Lease and fire a deterministic LSM file_open probe for e2e tests.",
		Detail: "lsm probe leases a unique 8-byte marker comm and a target file in " +
			"a fresh tempdir, returning $probe with .marker, .marker_hex, .file, and " +
			".dir. Load an lsm/file_open program with -g target_comm=0x${probe.marker_hex} " +
			"so it counts only the marker's opens, then lsm fire $probe N opens the " +
			"target N times under the marker comm. Remove $probe.dir on cleanup.",
	})
}

func handleLsm(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) == 0 {
		return runtime.Value{}, fmt.Errorf("lsm: subcommand required (valid: probe, fire)")
	}
	sub := driver.ArgText(c.Args[0])
	rest := c.Args[1:]
	switch sub {
	case "probe":
		return handleLsmProbe(rest)
	case "fire":
		return handleLsmFire(c, rest)
	default:
		return runtime.Value{}, fmt.Errorf("lsm: unknown subcommand %q (valid: probe, fire)", sub)
	}
}

// handleLsmProbe leases a probe: a unique marker comm and a target file
// in a fresh tempdir. The marker is 8 lowercase letters so it fits the
// comm field with room to spare and packs cleanly into the u64 the
// program's filter compares.
func handleLsmProbe(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 0 {
		return runtime.Value{}, fmt.Errorf("lsm probe: takes no arguments")
	}
	marker, err := randomMarker()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("lsm probe: %w", err)
	}

	dir, err := os.MkdirTemp("", "lsm-probe-")
	if err != nil {
		return runtime.Value{}, fmt.Errorf("lsm probe: create tempdir: %w", err)
	}

	file := filepath.Join(dir, "target")
	if err := os.WriteFile(file, []byte("lsm probe target\n"), 0o644); err != nil {
		return runtime.Value{}, fmt.Errorf("lsm probe: create target file: %w", err)
	}

	return runtime.ValueFromLsmProbe(&runtime.LsmProbe{
		Marker:    marker,
		MarkerHex: hex.EncodeToString([]byte(marker)),
		File:      file,
		Dir:       dir,
	}), nil
}

// handleLsmFire opens the probe's target file N times from a worker
// subprocess whose comm is the probe's marker, so the program's filter
// attributes exactly those opens. The worker is a re-exec of this binary
// in lsm-open-worker mode; it runs synchronously so the counter is
// settled when fire returns.
func handleLsmFire(c driver.Ctx, args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("lsm fire: requires $probe and N")
	}
	probe, err := lsmProbeFromArg(args[0])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("lsm fire: %w", err)
	}

	n, err := strconv.Atoi(driver.ArgText(args[1]))
	if err != nil {
		return runtime.Value{}, fmt.Errorf("lsm fire: N: %w", err)
	}

	if n < 0 {
		return runtime.Value{}, fmt.Errorf("lsm fire: N must not be negative (got %d)", n)
	}

	shellPath, err := os.Executable()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("lsm fire: resolve executable path: %w", err)
	}

	cmd := exec.CommandContext(c.Ctx, shellPath, probe.Marker, probe.File, strconv.Itoa(n))
	cmd.Env = append(os.Environ(), "BPFMAN_SHELL_MODE=lsm-open-worker")
	if out, err := cmd.CombinedOutput(); err != nil {
		return runtime.Value{}, fmt.Errorf("lsm fire: worker failed: %w: %s", err, out)
	}
	return runtime.ValueFromEnvelope(runtime.OkEnvelope()), nil
}

// randomMarker returns 8 lowercase ASCII letters, unique enough per
// probe that concurrent lsm fixtures on one host do not share a comm.
func randomMarker() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random marker: %w", err)
	}
	for i := range b {
		b[i] = 'a' + b[i]%26
	}
	return string(b[:]), nil
}

func lsmProbeFromArg(a runtime.Arg) (*runtime.LsmProbe, error) {
	sva, ok := a.(runtime.StructuredValueArg)
	if !ok {
		return nil, fmt.Errorf("expected a $probe argument, got %T", a)
	}
	if sva.Value.Kind() != semantics.OriginLsm {
		return nil, fmt.Errorf("expected a $probe argument, got a %s value", sva.Value.Kind())
	}
	probe, ok := sva.Value.Origin().(*runtime.LsmProbe)
	if !ok {
		return nil, fmt.Errorf("$probe has no underlying handle (got %T)", sva.Value.Origin())
	}
	return probe, nil
}
