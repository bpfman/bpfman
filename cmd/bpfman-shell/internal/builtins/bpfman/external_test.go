package bpfmanbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/kernel"
)

// TestDecodeBpfmanResult_ProgramListDTO proves the external dispatch
// backend decodes `program list` into the ProgramListResult DTO,
// preserving the top-level summary fields and -- crucially -- a
// kernel-only entry's null record. Decoding into a flat []Program
// shape would silently drop those, so this locks external/library
// parity for the list command.
func TestDecodeBpfmanResult_ProgramListDTO(t *testing.T) {
	t.Parallel()

	const managedID = kernel.ProgramID(11)
	const strayID = kernel.ProgramID(900)

	result := bpfman.ProgramListResult{
		Programs: []bpfman.ProgramListEntry{
			{
				ProgramID:    managedID,
				Managed:      true,
				Application:  "demo",
				Type:         "xdp",
				FunctionName: "xdp_stats",
				Links:        []bpfman.LinkID{100},
				Record: &bpfman.ProgramRecord{
					ProgramID: managedID,
					Load:      bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
				},
				Kernel: &kernel.Program{ID: managedID, Name: "xdp_stats"},
			},
			{
				ProgramID:    strayID,
				Managed:      false,
				Type:         "tracepoint",
				FunctionName: "stray",
				Links:        []bpfman.LinkID{},
				Record:       nil,
				Kernel:       &kernel.Program{ID: strayID, Name: "stray"},
			},
		},
	}
	stdout, err := json.Marshal(result)
	require.NoError(t, err)

	v, err := decodeBpfmanResult([]runtime.Arg{word("program"), word("list")}, stdout)
	require.NoError(t, err)

	decoded, ok := v.Origin().(bpfman.ProgramListResult)
	require.True(t, ok, "program list must decode into ProgramListResult, not the old []Program shape")
	require.Len(t, decoded.Programs, 2)

	managed := decoded.Programs[0]
	assert.True(t, managed.Managed)
	require.NotNil(t, managed.Record)
	assert.Equal(t, managedID, managed.Record.ProgramID)
	assert.NotNil(t, managed.Kernel)

	stray := decoded.Programs[1]
	assert.False(t, stray.Managed)
	assert.Nil(t, stray.Record, "kernel-only entry keeps a null record through external decode")
	require.NotNil(t, stray.Kernel)
	assert.Equal(t, strayID, stray.Kernel.ID)
	assert.Equal(t, "tracepoint", stray.Type)
}

func TestArgToCLIText_StructuredProgramAndLink(t *testing.T) {
	t.Parallel()

	progVal, err := runtime.ValueFromStruct(bpfman.Program{
		Record: bpfman.ProgramRecord{ProgramID: kernel.ProgramID(42)},
	})
	require.NoError(t, err)
	got, err := argToCLIText(runtime.StructuredValueArg{
		Name:  "prog",
		Value: progVal.WithKind(semantics.OriginProgram),
	})
	require.NoError(t, err)
	assert.Equal(t, "42", got)

	linkVal, err := runtime.ValueFromStruct(bpfman.Link{
		Record: bpfman.LinkRecord{ID: bpfman.LinkID(99)},
	})
	require.NoError(t, err)
	got, err = argToCLIText(runtime.StructuredValueArg{
		Name:  "link",
		Value: linkVal.WithKind(semantics.OriginLink),
	})
	require.NoError(t, err)
	assert.Equal(t, "99", got)
}

func TestCommandSupportsOutput(t *testing.T) {
	t.Parallel()

	// Render their own formats; no injected output flag.
	assert.False(t, commandSupportsOutput([]string{"image", "build"}))
	assert.False(t, commandSupportsOutput([]string{"image", "inspect"}))

	// Mutation verbs print nothing on success and have no output flag,
	// so the shell must not inject one.
	assert.False(t, commandSupportsOutput([]string{"program", "unload"}))
	assert.False(t, commandSupportsOutput([]string{"program", "delete"}))
	assert.False(t, commandSupportsOutput([]string{"link", "detach"}))
	assert.False(t, commandSupportsOutput([]string{"link", "delete"}))

	// Query and load verbs produce structured output.
	assert.True(t, commandSupportsOutput([]string{"program", "list"}))
	assert.True(t, commandSupportsOutput([]string{"program", "get"}))
	assert.True(t, commandSupportsOutput([]string{"program", "load", "file"}))
	assert.True(t, commandSupportsOutput([]string{"link", "get"}))
	assert.True(t, commandSupportsOutput([]string{"link", "attach", "xdp"}))
	assert.True(t, commandSupportsOutput([]string{"image", "verify"}))
}

func TestDispatchCommandExternalInheritsBPFMANConfig(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bpfman")
	seen := filepath.Join(dir, "seen-config")
	configPath := filepath.Join(dir, "no-signature-verification.toml")
	script := `#!/bin/sh
printf '%s' "$BPFMAN_CONFIG" > "$BPFMAN_CONFIG_SEEN"
printf '{"programs":[]}'
`
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))

	t.Setenv("BPFMAN_BIN", bin)
	t.Setenv("BPFMAN_CONFIG", configPath)
	t.Setenv("BPFMAN_CONFIG_SEEN", seen)

	_, err := dispatchCommandExternal(t.Context(), []runtime.Arg{
		word("program"),
		word("list"),
	})
	require.NoError(t, err)

	got, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Equal(t, configPath, string(got))
}

func TestDispatchCommandExternal_ContextCancelInterruptsChildProcessGroup(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bpfman")
	child := filepath.Join(dir, "child.sh")
	ready := filepath.Join(dir, "ready")
	ack := filepath.Join(dir, "ack")
	require.NoError(t, os.WriteFile(bin, []byte(`#!/bin/sh
"$BPFMAN_CANCEL_CHILD" "$BPFMAN_CANCEL_ACK" "$BPFMAN_CANCEL_READY"; :
`), 0o755))
	require.NoError(t, os.WriteFile(child, []byte(`#!/bin/sh
trap 'echo interrupted > "$1"; exit 0' INT
echo ready > "$2"
sleep 2
`), 0o755))

	t.Setenv("BPFMAN_BIN", bin)
	t.Setenv("BPFMAN_CANCEL_CHILD", child)
	t.Setenv("BPFMAN_CANCEL_READY", ready)
	t.Setenv("BPFMAN_CANCEL_ACK", ack)

	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("script context cancelled")
	errCh := make(chan error, 1)
	go func() {
		_, err := dispatchCommandExternal(ctx, []runtime.Arg{
			word("program"),
			word("list"),
		})
		errCh <- err
	}()

	assert.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, time.Second, 20*time.Millisecond)
	cancel(cause)

	assert.Eventually(t, func() bool {
		_, err := os.Stat(ack)
		return err == nil
	}, time.Second, 20*time.Millisecond)
	assert.Equal(t, cause, <-errCh)
}
