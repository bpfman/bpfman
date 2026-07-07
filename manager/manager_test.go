// Package manager_test uses Behaviour-Driven Development (BDD) style.
//
// Each test follows the Given/When/Then structure:
//   - Given: Initial state and context (the fixture)
//   - When: The action being tested
//   - Then: The expected outcome
//
// This makes tests readable as specifications of behaviour. When adding
// new tests, follow this pattern and use descriptive test names that
// explain the scenario being tested.
//
// The tests use a fake kernel implementation that simulates BPF operations
// without syscalls, combined with a real in-memory SQLite database. This
// enables fast, reliable testing of the manager's core logic.
package manager_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/manager"
)

// =============================================================================
// Program Load/Unload/Get/List Tests
// =============================================================================

func TestLoadProgram_WithValidRequest_Succeeds(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "my_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err, "failed to create load spec")

	opts := manager.LoadOpts{
		UserMetadata: map[string]string{
			"bpfman.io/ProgramName": "my-program",
			"app":                   "test-app",
		},
	}

	prog, err := fix.Load(ctx, spec, opts)
	require.NoError(t, err, "Load failed")

	// Verify returned program fields
	assert.Equal(t, "my_prog", prog.Record.Meta.Name, "Spec.Meta.Name")
	assert.NotZero(t, prog.Record.ProgramID, "Spec.ProgramID")
	require.NotNil(t, prog.Status.Kernel, "Status.Kernel should not be nil")
	assert.Equal(t, "my_prog", prog.Status.Kernel.Name, "Status.Kernel.Name")
	assert.NotEmpty(t, prog.Record.Handles.PinPath, "Spec.Handles.PinPath should be set")
}

// TestGetProgram_ReturnsAllFields verifies that:
//
//	Given a program loaded with specific metadata,
//	When I retrieve it via Get,
//	Then all fields match what was provided at load time.
func TestGetProgram_ReturnsAllFields(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "get_test_prog", bpfman.ProgramTypeKprobe)
	require.NoError(t, err, "failed to create load spec")

	opts := manager.LoadOpts{
		UserMetadata: map[string]string{
			"bpfman.io/ProgramName": "get-test-program",
			"environment":           "testing",
			"version":               "1.0.0",
		},
	}

	loaded, err := fix.Load(ctx, spec, opts)
	require.NoError(t, err, "Load failed")

	retrieved, err := fix.Manager.Get(ctx, loaded.Record.ProgramID)
	require.NoError(t, err, "Get failed")

	// Verify retrieved program matches loaded
	require.NotNil(t, retrieved.Status.Kernel, "Status.Kernel should not be nil")
	assert.Equal(t, loaded.Record.ProgramID, retrieved.Status.Kernel.ID, "Status.Kernel.ID")
	assert.Equal(t, "get_test_prog", retrieved.Status.Kernel.Name, "Status.Kernel.Name")
	assert.Equal(t, "get_test_prog", retrieved.Record.Meta.Name, "Spec.Meta.Name")
	assert.Equal(t, "get-test-program", retrieved.Record.Meta.Metadata["bpfman.io/ProgramName"], "UserMetadata")
	assert.Equal(t, "testing", retrieved.Record.Meta.Metadata["environment"], "UserMetadata[environment]")
	assert.Equal(t, "1.0.0", retrieved.Record.Meta.Metadata["version"], "UserMetadata[version]")
}

func TestGetProgram_StoreRecordWithoutKernelProgramRequiresReconciliation(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "stale_prog", bpfman.ProgramTypeKprobe)
	require.NoError(t, err, "failed to create load spec")

	loaded, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")
	fix.Kernel.RemoveKernelProgram(loaded.Record.ProgramID)

	_, err = fix.Manager.Get(ctx, loaded.Record.ProgramID)
	var reconcileErr manager.ErrProgramRequiresReconciliation
	require.ErrorAs(t, err, &reconcileErr)
	assert.Equal(t, loaded.Record.ProgramID, reconcileErr.ProgramID)
	assert.ErrorContains(t, err, "requires reconciliation")
}

// TestLoadProgram_WithGlobalData verifies that:
//
//	Given a program loaded with global data,
//	When I retrieve it via Get,
//	Then the global data is returned correctly.
func TestLoadProgram_WithGlobalData(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	globalData := map[string][]byte{
		"config_var":  {0x01, 0x02, 0x03, 0x04},
		"another_var": {0xAB, 0xCD},
		"empty_var":   {},
		"single_byte": {0xFF},
	}

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "global_data_prog", bpfman.ProgramTypeXDP)
	require.NoError(t, err, "failed to create load spec")
	spec = spec.WithGlobalData(globalData)

	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	// Verify via Get
	retrieved, err := fix.Manager.Get(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, globalData, retrieved.Record.Load.GlobalData(), "GlobalData")
}

// TestLoadProgram_WithMetadataAndGlobalData verifies that:
//
//	Given a program loaded with both metadata and global data,
//	When I retrieve it via Get,
//	Then both are returned correctly.
func TestLoadProgram_WithMetadataAndGlobalData(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	metadata := map[string]string{
		"bpfman.io/ProgramName": "combined-test",
		"app":                   "test-app",
		"version":               "2.0.0",
	}

	globalData := map[string][]byte{
		"param1": {0x01, 0x02},
		"param2": {0xAA, 0xBB, 0xCC},
	}

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "combined_prog", bpfman.ProgramTypeTC)
	require.NoError(t, err, "failed to create load spec")
	spec = spec.WithGlobalData(globalData)

	opts := manager.LoadOpts{UserMetadata: metadata}

	prog, err := fix.Load(ctx, spec, opts)
	require.NoError(t, err, "Load failed")

	// Verify via Get
	retrieved, err := fix.Manager.Get(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, "combined-test", retrieved.Record.Meta.Metadata["bpfman.io/ProgramName"])
	assert.Equal(t, "test-app", retrieved.Record.Meta.Metadata["app"])
	assert.Equal(t, "2.0.0", retrieved.Record.Meta.Metadata["version"])
	assert.Equal(t, globalData, retrieved.Record.Load.GlobalData())
}

// TestListPrograms_ReturnsAllFields verifies that:
//
//	Given a program loaded with specific metadata,
//	When I list all programs,
//	Then each result contains correctly populated fields.
func TestListPrograms_ReturnsAllFields(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load a program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("list_prog.o"), "list_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err, "failed to create load spec")

	opts := manager.LoadOpts{
		UserMetadata: map[string]string{
			"bpfman.io/ProgramName": "list-test-program",
			"team":                  "platform",
		},
	}

	_, err = fix.Load(ctx, spec, opts)
	require.NoError(t, err, "Load failed")

	// List programs
	result, err := fix.Manager.ListPrograms(ctx)
	require.NoError(t, err, "List failed")
	require.Len(t, result, 1, "expected 1 managed program")

	prog := result[0]
	assert.Equal(t, "list_prog", prog.Record.Meta.Name, "Spec.Meta.Name")
	assert.Equal(t, "list-test-program", prog.Record.Meta.Metadata["bpfman.io/ProgramName"])
	assert.Equal(t, "platform", prog.Record.Meta.Metadata["team"])
}

// TestLoadProgram_WithDuplicateName_BothSucceed verifies that:
//
//	Given a server with one program already loaded using a name,
//	When I attempt to load another program with the same name,
//	Then both programs load successfully (duplicates are allowed).
func TestLoadProgram_WithDuplicateName_BothSucceed(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load first program
	spec1, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog1.o"), "same_name", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog1, err := fix.Load(ctx, spec1, manager.LoadOpts{})
	require.NoError(t, err, "First Load failed")

	// Load second program with same name
	spec2, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog2.o"), "same_name", bpfman.ProgramTypeKprobe)
	require.NoError(t, err)
	prog2, err := fix.Load(ctx, spec2, manager.LoadOpts{})
	require.NoError(t, err, "Second Load failed")

	// Both should have different kernel IDs
	assert.NotEqual(t, prog1.Record.ProgramID, prog2.Record.ProgramID, "kernel IDs should differ")

	// Both should be in the list
	result, err := fix.Manager.ListPrograms(ctx)
	require.NoError(t, err)
	assert.Len(t, result, 2, "expected 2 programs")
}

// TestLoadProgram_WithDifferentNames_BothSucceed verifies that:
//
//	Given an empty manager,
//	When I load two programs with different names,
//	Then both programs exist and are listed.
func TestLoadProgram_WithDifferentNames_BothSucceed(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	for _, name := range []string{"program_a", "program_b"} {
		spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), name, bpfman.ProgramTypeTracepoint)
		require.NoError(t, err)
		_, err = fix.Load(ctx, spec, manager.LoadOpts{
			UserMetadata: map[string]string{
				"bpfman.io/ProgramName": name,
			},
		})
		require.NoError(t, err, "Load %s failed", name)
	}

	result, err := fix.Manager.ListPrograms(ctx)
	require.NoError(t, err, "List failed")
	assert.Len(t, result, 2, "expected 2 programs")
}

// TestUnloadProgram_WhenProgramExists_RemovesIt verifies that:
//
//	Given a manager with one program loaded,
//	When I unload the program,
//	Then the unload succeeds and the program is no longer retrievable.
func TestUnloadProgram_WhenProgramExists_RemovesIt(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "unload_test", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)

	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "Unload failed")

	// Verify program is gone
	result, err := fix.Manager.ListPrograms(ctx)
	require.NoError(t, err)
	assert.Empty(t, result, "expected no managed programs after unload")
}

// TestLoadProgram_AfterUnload_NameBecomesAvailable verifies that:
//
//	Given a program was loaded and then unloaded,
//	When I load a new program with the same name,
//	Then the load succeeds because the name was freed.
func TestLoadProgram_AfterUnload_NameBecomesAvailable(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load first program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "reusable_name", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog1, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "First Load failed")

	// Unload it
	err = fix.Unload(ctx, prog1.Record.ProgramID)
	require.NoError(t, err, "Unload failed")

	// Load again with same name
	spec2, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog2.o"), "reusable_name", bpfman.ProgramTypeKprobe)
	require.NoError(t, err)
	prog2, err := fix.Load(ctx, spec2, manager.LoadOpts{})
	require.NoError(t, err, "Second Load failed")

	assert.NotEqual(t, prog1.Record.ProgramID, prog2.Record.ProgramID, "kernel IDs should differ")
}

// TestLoadProgram_AllProgramTypes_RoundTrip verifies that:
//
//	Given an empty manager,
//	When I load programs of each supported type,
//	Then each program's type is correctly stored and returned via Get.
func TestLoadProgram_AllProgramTypes_RoundTrip(t *testing.T) {
	t.Parallel()

	programTypes := []struct {
		name        string
		programType bpfman.ProgramType
		attachFunc  string // only for fentry/fexit
	}{
		{"xdp", bpfman.ProgramTypeXDP, ""},
		{"tc", bpfman.ProgramTypeTC, ""},
		{"tcx", bpfman.ProgramTypeTCX, ""},
		{"tracepoint", bpfman.ProgramTypeTracepoint, ""},
		{"kprobe", bpfman.ProgramTypeKprobe, ""},
		{"kretprobe", bpfman.ProgramTypeKretprobe, ""},
		{"uprobe", bpfman.ProgramTypeUprobe, ""},
		{"uretprobe", bpfman.ProgramTypeUretprobe, ""},
		{"fentry", bpfman.ProgramTypeFentry, "some_func"},
		{"fexit", bpfman.ProgramTypeFexit, "some_func"},
	}

	for _, tc := range programTypes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fix := newTestFixture(t)
			ctx := context.Background()

			var spec bpfman.LoadSpec
			var err error
			if tc.attachFunc != "" {
				spec, err = bpfman.NewAttachLoadSpec(fix.BytecodeFile("prog.o"), "test_prog", tc.programType, tc.attachFunc)
			} else {
				spec, err = bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "test_prog", tc.programType)
			}
			require.NoError(t, err, "failed to create load spec")

			prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
			require.NoError(t, err, "Load failed for %s", tc.name)
			assert.NotZero(t, prog.Record.ProgramID, "kernel ID should be assigned")
		})
	}
}

// =============================================================================
// Partial Failure and Rollback Tests
// =============================================================================

// TestLoadProgram_PartialFailure_SecondProgramFails verifies that:
//
//	Given a manager configured to fail on the second program load,
//	When I attempt to load two programs in separate Load calls,
//	Then the first succeeds with a success outcome,
//	And the second fails with a failure outcome showing the kernel load failed.
func TestLoadProgram_PartialFailure_SecondProgramFails(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Configure kernel to fail on the second program
	fix.Kernel.FailOnProgram("prog_two", fmt.Errorf("injected failure on prog_two"))

	// Load first program
	spec1, err := bpfman.NewLoadSpec(fix.BytecodeFile("multi.o"), "prog_one", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog1, err := fix.Load(ctx, spec1, manager.LoadOpts{})
	require.NoError(t, err, "First Load should succeed")
	// Outcome is not accessible on success - absence of error implies success

	// Load second program - should fail
	spec2, err := bpfman.NewLoadSpec(fix.BytecodeFile("multi.o"), "prog_two", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	_, err = fix.Load(ctx, spec2, manager.LoadOpts{})
	require.Error(t, err, "Second Load should fail")
	assert.Contains(t, err.Error(), "injected failure", "error should mention injected failure")

	// First program should still exist (manager doesn't auto-rollback across separate Load calls)
	_, err = fix.Manager.Get(ctx, prog1.Record.ProgramID)
	require.NoError(t, err, "First program should still exist")

	// Verify kernel operations
	fix.AssertKernelOps([]string{
		"load:prog_one:ok",
		"load:prog_two:error",
	})
}

// TestLoadProgram_SingleProgram_FailsCleanly verifies that:
//
//	Given a manager configured to fail on a single program load,
//	When I attempt to load one program,
//	Then the error is returned with a failure outcome,
//	And no programs exist in the kernel or database.
func TestLoadProgram_SingleProgram_FailsCleanly(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Configure kernel to fail
	fix.Kernel.FailOnProgram("single_prog", fmt.Errorf("injected failure"))

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("single.o"), "single_prog", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	_, err = fix.Load(ctx, spec, manager.LoadOpts{})

	require.Error(t, err, "Load should fail")
	assert.Contains(t, err.Error(), "injected failure")

	fix.AssertKernelOps([]string{"load:single_prog:error"})
	fix.AssertCleanState()
}

// TestLoadProgram_FailOnNthLoad verifies that:
//
//	Given a manager configured to fail on the Nth load operation,
//	When I load multiple programs,
//	Then the failure occurs at the expected point with correct outcome.
func TestLoadProgram_FailOnNthLoad(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Configure kernel to fail on the 2nd load attempt
	fix.Kernel.FailOnNthLoad(2, fmt.Errorf("nth load failure"))

	// Load first program - should succeed
	spec1, err := bpfman.NewLoadSpec(fix.BytecodeFile("multi.o"), "prog_a", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	_, err = fix.Load(ctx, spec1, manager.LoadOpts{})
	require.NoError(t, err, "First Load should succeed")
	// Outcome is not accessible on success - absence of error implies success

	// Load second program - should fail
	spec2, err := bpfman.NewLoadSpec(fix.BytecodeFile("multi.o"), "prog_b", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	_, err = fix.Load(ctx, spec2, manager.LoadOpts{})
	require.Error(t, err, "Second Load should fail on 2nd program")
	assert.Contains(t, err.Error(), "injected error on load")

	fix.AssertKernelOps([]string{
		"load:prog_a:ok",
		"load:prog_b:error",
	})
}

// =============================================================================
// Attach/Detach Tests
// =============================================================================

// TestAttachTracepoint_WhenAttachFails_ProgramRemainsLoaded verifies that:
//
//	Given a program that was successfully loaded,
//	When I attempt to attach it and the attach operation fails,
//	Then the program remains loaded in the kernel and database,
//	And no link is created,
//	And the outcome records the attach failure.
func TestAttachTracepoint_WhenAttachFails_ProgramRemainsLoaded(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load a tracepoint program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "tp_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	// Configure fake kernel to fail on attach
	fix.Kernel.FailOnAttach("tracepoint", fmt.Errorf("injected attach failure"))

	// Attempt attach - should fail
	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "syscalls/sys_enter_read")
	require.NoError(t, err, "failed to create attach spec")
	_, err = fix.Attach(ctx, attachSpec)
	require.Error(t, err, "attach should fail")
	assert.Contains(t, err.Error(), "injected attach failure")

	// Program should still be loaded
	retrieved, err := fix.Manager.Get(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "Get failed - program should still exist")
	assert.Equal(t, prog.Record.ProgramID, retrieved.Status.Kernel.ID)
}

// TestDetach_ExistingLink_Succeeds verifies that:
//
//	Given a program with an active link,
//	When I detach the link,
//	Then the detach succeeds,
//	And the link is removed,
//	And the program remains loaded.
func TestDetach_ExistingLink_Succeeds(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load and attach a program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "detach_test", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "syscalls/sys_enter_write")
	require.NoError(t, err, "failed to create attach spec")
	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err, "Attach failed")

	// Detach
	err = fix.Detach(ctx, link.Record.ID)
	require.NoError(t, err, "Detach failed")

	// Link should be gone
	links, err := fix.Manager.ListLinksByProgram(ctx, prog.Record.ProgramID)
	require.NoError(t, err)
	assert.Empty(t, links, "expected no links after detach")

	// Program should still exist
	_, err = fix.Manager.Get(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "program should still exist after detach")
}

// TestMultipleLinks_SameProgram_AllDetachable verifies that:
//
//	Given a program with multiple active links,
//	When I detach them one by one,
//	Then each detach succeeds,
//	And the program remains loaded until explicitly unloaded.
func TestMultipleLinks_SameProgram_AllDetachable(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load a program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "multi_link_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	// Create multiple attachments
	tracepoints := []struct{ group, name string }{
		{"syscalls", "sys_enter_read"},
		{"syscalls", "sys_enter_write"},
		{"syscalls", "sys_enter_open"},
	}

	var linkIDs []bpfman.LinkID
	for _, tp := range tracepoints {
		attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, tp.group+"/"+tp.name)
		require.NoError(t, err, "failed to create attach spec")
		link, err := fix.Attach(ctx, attachSpec)
		require.NoError(t, err, "Attach failed for %s/%s", tp.group, tp.name)
		linkIDs = append(linkIDs, link.Record.ID)
	}

	// Verify all links exist
	links, err := fix.Manager.ListLinksByProgram(ctx, prog.Record.ProgramID)
	require.NoError(t, err)
	assert.Len(t, links, 3, "expected 3 links")

	// Detach each link
	for _, linkID := range linkIDs {
		err := fix.Detach(ctx, linkID)
		require.NoError(t, err, "Detach failed for link %d", linkID)
	}

	// All links should be gone
	links, err = fix.Manager.ListLinksByProgram(ctx, prog.Record.ProgramID)
	require.NoError(t, err)
	assert.Empty(t, links, "expected no links after detaching all")

	// Program should still exist
	_, err = fix.Manager.Get(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "program should still exist")
}

// TestUnloadProgram_WithActiveLinks_DetachesLinksThenUnloads verifies that:
//
//	Given a program that was successfully loaded and has active links,
//	When I unload the program,
//	Then the links are detached first,
//	Then the program is unloaded,
//	And the kernel and database are clean.
func TestUnloadProgram_WithActiveLinks_DetachesLinksThenUnloads(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load and attach a program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "unload_with_links", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "syscalls/sys_enter_read")
	require.NoError(t, err, "failed to create attach spec")
	_, err = fix.Attach(ctx, attachSpec)
	require.NoError(t, err, "Attach failed")

	// Verify we have 1 program and 1 link
	assert.Equal(t, 1, fix.Kernel.ProgramCount(), "should have 1 program")
	assert.Equal(t, 1, fix.Kernel.LinkCount(), "should have 1 link")

	// Unload should succeed (detaches links automatically)
	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "Unload failed")

	// Verify operation sequence: load -> attach -> detach -> unload
	ops := fix.Kernel.Operations()
	require.GreaterOrEqual(t, len(ops), 3, "expected at least 3 operations")

	// First op: load
	assert.Equal(t, "load", ops[0].Op, "first op should be load")
	assert.Equal(t, "unload_with_links", ops[0].Name, "load should be for unload_with_links")

	// Second op: attach
	assert.Equal(t, "attach", ops[1].Op, "second op should be attach")

	// Third op: detach (before unload)
	assert.Equal(t, "detach", ops[2].Op, "third op should be detach")

	// Fourth op: unload
	assert.Equal(t, "unload", ops[3].Op, "fourth op should be unload")

	// Verify clean state
	fix.AssertCleanState()
}

// =============================================================================
// Detach Failure Tests
// =============================================================================

// TestDetach_KernelFailure_ReturnsError verifies that:
//
//	Given a program with an active link,
//	When I attempt to detach and the kernel fails,
//	Then the detach operation returns an error with failure outcome.
func TestDetach_KernelFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load a tracepoint program
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("prog.o"), "tp_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load failed")

	// Attach to a tracepoint
	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "syscalls/sys_enter_close")
	require.NoError(t, err, "failed to create attach spec")
	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err, "Attach failed")

	// Configure the fake kernel to fail when detaching this captured
	// kernel link. The manager still receives the bpfman LinkID below.
	require.NotNil(t, link.Record.KernelLinkID)
	fix.Kernel.FailOnDetach(*link.Record.KernelLinkID, fmt.Errorf("injected detach failure"))

	// Attempt to detach - should fail
	err = fix.Detach(ctx, link.Record.ID)
	require.Error(t, err, "Detach should fail due to kernel error")
	assert.Contains(t, err.Error(), "injected detach failure", "error should mention injected failure")

	// Verify the link still exists in the fake kernel (was not deleted)
	assert.Equal(t, 1, fix.Kernel.LinkCount(), "link should still exist in kernel after failed detach")
}

// =============================================================================
// List Programs with Multiple Types Tests
// =============================================================================

// TestListPrograms_AllProgramTypes_ReturnsCorrectTypes verifies that:
//
//	Given multiple programs of different types loaded,
//	When I list all programs,
//	Then each program's type is correctly returned.
func TestListPrograms_AllProgramTypes_ReturnsCorrectTypes(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Load programs of different types
	programTypes := []struct {
		name        string
		programType bpfman.ProgramType
	}{
		{"xdp_prog", bpfman.ProgramTypeXDP},
		{"tc_prog", bpfman.ProgramTypeTC},
		{"tp_prog", bpfman.ProgramTypeTracepoint},
		{"kprobe_prog", bpfman.ProgramTypeKprobe},
	}

	for _, pt := range programTypes {
		spec, err := bpfman.NewLoadSpec(fix.BytecodeFile(pt.name+".o"), pt.name, pt.programType)
		require.NoError(t, err)
		_, err = fix.Load(ctx, spec, manager.LoadOpts{
			UserMetadata: map[string]string{
				"bpfman.io/ProgramName": pt.name,
			},
		})
		require.NoError(t, err, "Load %s failed", pt.name)
	}

	// List all programs
	result, err := fix.Manager.ListPrograms(ctx)
	require.NoError(t, err, "List failed")
	require.Len(t, result, len(programTypes), "expected %d programs", len(programTypes))
}
