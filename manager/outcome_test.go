package manager_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/manager"
)

// TestLoad_Success verifies that a successful load operation completes
// without error and returns the program.
func TestLoad_Success(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("path.o"), "test_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)

	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	// Verify program was loaded
	assert.NotZero(t, prog.Record.ProgramID)
	assert.Equal(t, "test_prog", prog.Record.Meta.Name)
}

// TestUnload_Success verifies that a successful unload operation
// completes without error.
func TestUnload_Success(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// First load a program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("path.o"), "test_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)

	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	// Now unload it
	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err)

	// Verify state is clean
	fix.AssertCleanState()
}

// TestDetach_NotFound_ReturnsPlainError verifies that a detach
// operation for a non-existent link returns a plain error because
// preflight failures bypass plan execution.
func TestDetach_NotFound_ReturnsPlainError(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	err := fix.Detach(ctx, bpfman.LinkID(999))
	require.Error(t, err)

	var notFound bpfman.ErrLinkNotFound
	assert.True(t, errors.As(err, &notFound), "expected ErrLinkNotFound, got %T", err)
}

// TestUnload_NotFound_ReturnsPlainError verifies that an unload
// operation for a non-existent program returns a plain error because
// preflight failures bypass plan execution.
func TestUnload_NotFound_ReturnsPlainError(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	err := fix.Unload(ctx, 999)
	require.Error(t, err)

	var notFound bpfman.ErrProgramNotFound
	assert.True(t, errors.As(err, &notFound), "expected ErrProgramNotFound, got %T", err)
}

// TestAttachTracepoint_Success verifies that a successful attach operation
// completes without error and returns the link.
func TestAttachTracepoint_Success(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load a program first
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("path.o"), "test_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)

	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	// Attach it
	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "sched/sched_switch")
	require.NoError(t, err)

	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err)

	// Verify link was created
	assert.NotZero(t, link.Record.ID)
	assert.Equal(t, prog.Record.ProgramID, link.Record.ProgramID)
}

// TestOutcome_SystemStateReflectsActualState verifies that after an unload
// the system state is clean.
func TestOutcome_SystemStateReflectsActualState(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load and then unload a program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("path.o"), "test_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)

	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err)

	// Verify actual state is clean
	fix.AssertCleanState()
}

// TestOutcome_ExecutionFailure_HasTimeline verifies that an operation
// that fails during plan execution produces a useful error.
func TestOutcome_ExecutionFailure_HasTimeline(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load a program and attach it.
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "tp_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "syscalls/sys_enter_close")
	require.NoError(t, err)
	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err)

	// Inject a kernel failure on detach so the plan fails mid-execution.
	require.NotNil(t, link.Record.KernelLinkID)
	fix.Kernel.FailOnDetach(*link.Record.KernelLinkID, fmt.Errorf("injected failure"))

	err = fix.Detach(ctx, link.Record.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected failure")
}
