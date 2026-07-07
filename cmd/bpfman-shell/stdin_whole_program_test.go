package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

func runWholeProgramStdin(t *testing.T, script string) (string, string, error) {
	t.Helper()

	return runWholeProgramStdinContext(t, t.Context(), script)
}

func runWholeProgramStdinContext(t *testing.T, ctx context.Context, script string) (string, string, error) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	cli := &cli.CLI{Out: &outBuf, Err: &errBuf}
	lr := driver.NewScannerReader(strings.NewReader(script), nil)

	err := driver.Run(ctx, driver.Config{
		CLI:          cli,
		LineReader:   lr,
		Session:      runtime.NewSession(),
		File:         "<stdin>",
		NoCheck:      false,
		Fallback:     commandFallback,
		BindFallback: bindCommandFallback,
		MakeAssert:   makeExecAssert,
	})
	return outBuf.String(), errBuf.String(), err
}

func TestScriptRun_StdinWholeProgram_HonoursContextDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	stdout, stderr, err := runWholeProgramStdinContext(t, ctx, "exec sleep 5\nprint after\n")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestScriptRun_StdinWholeProgram_PropagatesPlainCancellationCause(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("interrupted by signal")
	time.AfterFunc(50*time.Millisecond, func() {
		cancel(cause)
	})

	stdout, stderr, err := runWholeProgramStdinContext(t, ctx, "exec sleep 5\nprint after\n")
	require.ErrorIs(t, err, cause)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestScriptRun_StdinWholeProgram_ForwardDefVisible(t *testing.T) {
	t.Parallel()

	script := "hello\ndef hello() { print from-stdin }\n"
	stdout, stderr, err := runWholeProgramStdin(t, script)
	require.NoError(t, err)
	assert.Equal(t, []string{"from-stdin"}, exactOutputLines(stdout))
	assert.Empty(t, stderr)
}

//nolint:paralleltest // This test changes process cwd via t.Chdir to verify stdin import resolution semantics.
func TestScriptRun_StdinWholeProgram_ImportResolvesFromCwd(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.bpfman"), []byte("def hi() { print cwd-lib }\n"), 0o644))

	t.Chdir(dir)

	script := "import ./lib.bpfman\nhi\n"
	stdout, stderr, err := runWholeProgramStdin(t, script)
	require.NoError(t, err)
	assert.Equal(t, []string{"cwd-lib"}, exactOutputLines(stdout))
	assert.Empty(t, stderr)
}
