package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// runScriptWithCheck runs script end-to-end with the static pre-flight
// pass enabled or disabled, returning stderr and the run error. It
// mirrors runWholeProgramStdin but exposes NoCheck so one test can
// compare the two pre-flight modes against the same source.
func runScriptWithCheck(t *testing.T, script string, noCheck bool) (string, error) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	cli := &cli.CLI{Out: &outBuf, Err: &errBuf}
	lr := driver.NewScannerReader(strings.NewReader(script), nil)

	err := driver.Run(t.Context(), driver.Config{
		CLI:          cli,
		LineReader:   lr,
		Session:      runtime.NewSession(),
		File:         "<test>",
		NoCheck:      noCheck,
		Fallback:     commandFallback,
		BindFallback: bindCommandFallback,
		MakeAssert:   makeExecAssert,
	})
	return errBuf.String(), err
}

// TestScriptRun_PreflightAbortsBeforeSideEffects proves the user-facing
// guarantee of the static pre-flight pass: a script whose later
// statement is statically invalid is rejected as a whole, before any
// earlier statement's side effect runs. The trigger is the arity error
// on a zero-argument 'wait'; the observable earlier side effect is
// 'exec touch' creating a marker file.
//
// The NoCheck arm is the control that proves the difference is
// pre-flight, not a broken touch. With pre-flight disabled the same
// script runs until 'wait' fails at runtime, by which point the touch
// has already happened. So the marker being absent under pre-flight and
// present without it isolates pre-flight as the gate.
func TestScriptRun_PreflightAbortsBeforeSideEffects(t *testing.T) {
	t.Parallel()

	t.Run("preflight on aborts before the exec", func(t *testing.T) {
		t.Parallel()

		marker := filepath.Join(t.TempDir(), "marker")
		script := "exec touch " + marker + "\nwait\n"

		stderr, err := runScriptWithCheck(t, script, false)

		require.Error(t, err, "preflight must reject the script")
		assert.Contains(t, stderr, "wait: expected at least 1", "the arity issue on wait is what preflight caught")
		assert.NoFileExists(t, marker, "the earlier exec must not run when preflight aborts the program")
	})

	t.Run("preflight off lets the exec run before wait fails", func(t *testing.T) {
		t.Parallel()

		marker := filepath.Join(t.TempDir(), "marker")
		script := "exec touch " + marker + "\nwait\n"

		stderr, err := runScriptWithCheck(t, script, true)

		require.Error(t, err, "wait still fails at runtime without preflight")
		assert.Contains(t, stderr, "wait requires exactly one argument", "without preflight the failure is the runtime arity guard")
		assert.FileExists(t, marker, "without preflight the earlier exec runs before wait fails")
	})
}
