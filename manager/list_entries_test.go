package manager_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
)

// findEntry returns the list entry with the given program ID, or nil.
func findEntry(entries []bpfman.ProgramListEntry, id kernel.ProgramID) *bpfman.ProgramListEntry {
	for i := range entries {
		if entries[i].ProgramID == id {
			return &entries[i]
		}
	}
	return nil
}

// loadManagedTracepoint loads one bpfman-managed tracepoint program
// tagged with the given application and returns its program ID.
func loadManagedTracepoint(t *testing.T, fix *testFixture, ctx context.Context, app string) kernel.ProgramID {
	t.Helper()
	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("managed.o"), "managed_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	md := map[string]string{}
	if app != "" {
		md[manager.ApplicationMetadataKey] = app
	}
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{UserMetadata: md})
	require.NoError(t, err)
	return prog.Record.ProgramID
}

// TestListProgramEntries_DefaultExcludesUnmanaged verifies that without
// --all the listing carries only bpfman-managed programs, as managed
// entries with their Record and kernel observation.
func TestListProgramEntries_DefaultExcludesUnmanaged(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	managedID := loadManagedTracepoint(t, fix, ctx, "demo")
	fix.Kernel.InjectKernelProgram(900, "stray_xdp", bpfman.ProgramTypeXDP)

	res, err := fix.Manager.ListProgramEntries(ctx)
	require.NoError(t, err)
	require.Len(t, res.Programs, 1, "default listing is managed-only")

	e := res.Programs[0]
	assert.Equal(t, managedID, e.ProgramID)
	assert.True(t, e.Managed)
	require.NotNil(t, e.Record, "managed entry carries its store record")
	assert.NotNil(t, e.Kernel, "managed entry carries kernel observation")
	assert.Equal(t, "tracepoint", e.Type)
	assert.Equal(t, "managed_prog", e.FunctionName)
	assert.Equal(t, "demo", e.Application)
}

// TestListProgramEntries_AllIncludesUnmanaged verifies that --all adds
// kernel-only programs as honest entries: Managed false, no Record, the
// kernel observation present, and the type and function name taken from
// the kernel.
func TestListProgramEntries_AllIncludesUnmanaged(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	managedID := loadManagedTracepoint(t, fix, ctx, "demo")
	fix.Kernel.InjectKernelProgram(900, "stray_xdp", bpfman.ProgramTypeXDP)

	res, err := fix.Manager.ListProgramEntries(ctx, bpfman.WithIncludeUnmanaged())
	require.NoError(t, err)
	require.Len(t, res.Programs, 2)

	require.NotNil(t, findEntry(res.Programs, managedID))

	stray := findEntry(res.Programs, 900)
	require.NotNil(t, stray, "unmanaged program appears under --all")
	assert.False(t, stray.Managed, "unmanaged program is flagged not-managed")
	assert.Nil(t, stray.Record, "kernel-only entry carries no synthetic record")
	require.NotNil(t, stray.Kernel, "kernel-only entry carries the kernel observation")
	assert.Equal(t, "xdp", stray.Type)
	assert.Equal(t, "stray_xdp", stray.FunctionName)
	assert.Equal(t, kernel.ProgramID(900), stray.Kernel.ID)
	assert.NotNil(t, stray.Links, "links is an empty array, not null")
	assert.Empty(t, stray.Links)
	assert.Empty(t, stray.Application)
}

// TestListProgramEntries_AllRespectsTypeFilter verifies that the
// program-type filter applies to kernel-only rows against their kernel
// type.
func TestListProgramEntries_AllRespectsTypeFilter(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	loadManagedTracepoint(t, fix, ctx, "demo") // tracepoint
	fix.Kernel.InjectKernelProgram(900, "stray_xdp", bpfman.ProgramTypeXDP)

	// --all --type xdp: the managed tracepoint is filtered out, the
	// unmanaged xdp remains.
	res, err := fix.Manager.ListProgramEntries(ctx, bpfman.WithIncludeUnmanaged(), bpfman.WithTypes(bpfman.ProgramTypeXDP))
	require.NoError(t, err)
	require.Len(t, res.Programs, 1)
	assert.Equal(t, kernel.ProgramID(900), res.Programs[0].ProgramID)
	assert.False(t, res.Programs[0].Managed)

	// --all --type kprobe: neither matches.
	res, err = fix.Manager.ListProgramEntries(ctx, bpfman.WithIncludeUnmanaged(), bpfman.WithTypes(bpfman.ProgramTypeKprobe))
	require.NoError(t, err)
	assert.Empty(t, res.Programs)
}

// TestListProgramEntries_AllSelectorExcludesUnmanaged verifies that a
// metadata selector excludes kernel-only rows (they carry no bpfman
// metadata) while still matching managed programs.
func TestListProgramEntries_AllSelectorExcludesUnmanaged(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	managedID := loadManagedTracepoint(t, fix, ctx, "demo")
	fix.Kernel.InjectKernelProgram(900, "stray_xdp", bpfman.ProgramTypeXDP)

	res, err := fix.Manager.ListProgramEntries(ctx, bpfman.WithIncludeUnmanaged(), bpfman.MatchingLabels(map[string]string{manager.ApplicationMetadataKey: "demo"}))
	require.NoError(t, err)
	require.Len(t, res.Programs, 1, "selector excludes the unmanaged program")
	assert.Equal(t, managedID, res.Programs[0].ProgramID)
}
