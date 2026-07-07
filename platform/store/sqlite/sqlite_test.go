package sqlite_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/store/sqlite"
)

// testLogger returns a logger for tests. By default it discards all output.
// Set BPFMAN_TEST_VERBOSE=1 to enable logging.
func testLogger() *slog.Logger {
	if os.Getenv("BPFMAN_TEST_VERBOSE") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testProgram returns a valid ProgramRecord for testing.
func testProgram() bpfman.ProgramRecord {
	return bpfman.ProgramRecord{
		Load: bpfman.TestLoadSpecWithPath(bpfman.ProgramTypeTracepoint, "/test/path/program.o"),
		Handles: bpfman.ProgramHandles{
			PinPath: "/sys/fs/bpf/test",
		},
		Meta: bpfman.ProgramMeta{
			Name: "test_program",
		},
		CreatedAt: time.Now(),
	}
}

func ptr[T any](v T) *T {
	return &v
}

func createEphemeralLink(t *testing.T, ctx context.Context, store platform.Store, programID kernel.ProgramID, kernelLinkID *kernel.LinkID, details bpfman.LinkDetails) bpfman.LinkRecord {
	t.Helper()
	record, err := store.CreateLink(ctx, bpfman.NewEphemeralLinkSpec(programID, kernelLinkID, details))
	require.NoError(t, err)
	return record
}

func createPinnedLink(t *testing.T, ctx context.Context, store platform.Store, programID kernel.ProgramID, kernelLinkID *kernel.LinkID, details bpfman.LinkDetails, pin bpfman.LinkPath) bpfman.LinkRecord {
	t.Helper()
	record, err := store.CreateLink(ctx, bpfman.NewPinnedLinkSpec(programID, kernelLinkID, details, pin))
	require.NoError(t, err)
	return record
}

func findLinkByID(t *testing.T, links []bpfman.LinkRecord, id bpfman.LinkID) bpfman.LinkRecord {
	t.Helper()
	for _, l := range links {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("link %d not found among %d links", id, len(links))
	return bpfman.LinkRecord{}
}

func TestNewRequiresWriterLock(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := sqlite.New(context.Background(), dbPath, testLogger(), nil)
	require.Error(t, err)
	require.Nil(t, store)
	assert.Contains(t, err.Error(), "writer lock required")
}

func TestNewFileBackedStoreUnderWriterLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := lock.Run(context.Background(), filepath.Join(dir, ".lock"), func(ctx context.Context, writeLock lock.WriterScope) error {
		store, err := sqlite.New(ctx, filepath.Join(dir, "store.db"), testLogger(), writeLock)
		require.NoError(t, err)
		defer store.Close()
		return nil
	})
	require.NoError(t, err)
}

func TestOpenExistingStoreRequiresExistingDatabase(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := sqlite.OpenExistingStore(context.Background(), dbPath, testLogger())
	require.Error(t, err)
	require.Nil(t, store)
	_, statErr := os.Stat(dbPath)
	require.True(t, os.IsNotExist(statErr), "OpenExistingStore must not create the database")
}

func TestOpenExistingStoreCurrentStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "store.db")
	err := lock.Run(context.Background(), filepath.Join(dir, ".lock"), func(ctx context.Context, writeLock lock.WriterScope) error {
		store, err := sqlite.New(ctx, dbPath, testLogger(), writeLock)
		require.NoError(t, err)
		return store.Close()
	})
	require.NoError(t, err)

	store, err := sqlite.OpenExistingStore(context.Background(), dbPath, testLogger())
	require.NoError(t, err)
	defer store.Close()
	programs, err := store.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, programs)
}

func TestForeignKey_LinkRequiresProgram(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Attempt to create a link referencing a non-existent program.
	details := bpfman.TracepointDetails{
		Group: "syscalls",
		Name:  "sys_enter_openat",
	}
	kernelLinkID := kernel.LinkID(1)
	spec := bpfman.NewEphemeralLinkSpec(kernel.ProgramID(999), &kernelLinkID, details) // program 999 does not exist

	_, err = store.CreateLink(ctx, spec)
	require.Error(t, err, "expected FK constraint violation")
	assert.True(t, strings.Contains(err.Error(), "FOREIGN KEY constraint failed"), "expected FK constraint error, got: %v", err)
}

func TestForeignKey_CascadeDeleteRemovesLinks(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program directly.
	programID := kernel.ProgramID(42)
	prog := testProgram()

	require.NoError(t, store.Save(ctx, programID, prog), "Save failed")

	// Create two links for that program.
	for i := range 2 {
		details := bpfman.KprobeDetails{
			FnName:   "test_fn",
			Offset:   0,
			Retprobe: false,
		}
		kernelLinkID := kernel.LinkID(100 + i)
		createEphemeralLink(t, ctx, store, programID, &kernelLinkID, details)
	}

	// Verify links exist.
	links, err := store.ListLinksByProgram(ctx, programID)
	require.NoError(t, err, "ListLinksByProgram failed")
	require.Len(t, links, 2, "expected 2 links")

	// Delete the program.
	require.NoError(t, store.Delete(ctx, programID), "Delete failed")

	// Verify CASCADE removed the links.
	links, err = store.ListLinksByProgram(ctx, programID)
	require.NoError(t, err, "ListLinksByProgram after delete failed")
	assert.Empty(t, links, "expected 0 links after CASCADE delete")
}

func TestMetadata_StoredAsJSON(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program with metadata.
	programID := kernel.ProgramID(42)
	prog := testProgram()
	prog.Meta.Metadata = map[string]string{
		"app":     "test",
		"version": "1.0",
	}

	require.NoError(t, store.Save(ctx, programID, prog), "Save failed")

	// Verify metadata is stored and retrieved correctly.
	found, err := store.Get(ctx, programID)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, "test", found.Meta.Metadata["app"], "metadata app mismatch")
	assert.Equal(t, "1.0", found.Meta.Metadata["version"], "metadata version mismatch")

	// Delete the program.
	require.NoError(t, store.Delete(ctx, programID), "Delete failed")

	// Verify program is gone.
	_, err = store.Get(ctx, programID)
	assert.Error(t, err, "expected error after delete")
}

func TestProgramName_DuplicatesAllowed(t *testing.T) {
	t.Parallel()

	// Multiple programs can share the same bpfman.io/ProgramName, e.g., when
	// loading multiple BPF programs from a single OCI image via the operator.
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create first program with a name.
	prog1 := testProgram()
	prog1.Meta.Metadata = map[string]string{
		"bpfman.io/ProgramName": "my-program",
	}

	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog1), "Save prog1 failed")

	// Create second program with the same name - this should succeed.
	prog2 := testProgram()
	prog2.Meta.Metadata = map[string]string{
		"bpfman.io/ProgramName": "my-program", // same name, allowed
	}

	err = store.Save(ctx, kernel.ProgramID(200), prog2)
	require.NoError(t, err, "duplicate program names should be allowed")
}

func TestUniqueIndex_DifferentNamesAllowed(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create two programs with different names.
	for i, name := range []string{"program-a", "program-b"} {
		prog := testProgram()
		prog.Meta.Metadata = map[string]string{
			"bpfman.io/ProgramName": name,
		}

		require.NoError(t, store.Save(ctx, kernel.ProgramID(100+i), prog), "Save %s failed", name)
	}

	// Verify both exist.
	programs, err := store.List(ctx)
	require.NoError(t, err, "List failed")
	assert.Len(t, programs, 2, "expected 2 programs")
}

func TestUniqueIndex_NameCanBeReusedAfterDelete(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program with a name.
	prog := testProgram()
	prog.Meta.Metadata = map[string]string{
		"bpfman.io/ProgramName": "reusable-name",
	}

	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog), "Save failed")

	// Delete it.
	require.NoError(t, store.Delete(ctx, kernel.ProgramID(100)), "Delete failed")

	// Create a new program with the same name.
	prog2 := testProgram()
	prog2.Meta.Metadata = map[string]string{
		"bpfman.io/ProgramName": "reusable-name", // same name, should work
	}

	require.NoError(t, store.Save(ctx, kernel.ProgramID(200), prog2), "Save prog2 failed")

	// Verify it exists.
	found, err := store.Get(ctx, kernel.ProgramID(200))
	require.NoError(t, err, "Get failed")
	assert.Equal(t, "reusable-name", found.Meta.Metadata["bpfman.io/ProgramName"], "name mismatch")
}

func TestLinkRegistry_TracepointRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program first
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), prog), "Save failed")

	// Create a tracepoint link
	kernelLinkID := kernel.LinkID(100)
	details := bpfman.TracepointDetails{
		Group: "syscalls",
		Name:  "sys_enter_openat",
	}
	pinPath := bpfman.LinkPath("/sys/fs/bpf/bpfman/test/link")
	record := createPinnedLink(t, ctx, store, kernel.ProgramID(42), &kernelLinkID, details, pinPath)

	// Retrieve and verify
	gotSpec, err := store.GetLink(ctx, record.ID)
	require.NoError(t, err, "GetLink failed")

	assert.Equal(t, bpfman.LinkKindTracepoint, gotSpec.Kind)
	assert.Equal(t, record.ID, gotSpec.ID)
	require.NotNil(t, gotSpec.KernelLinkID)
	assert.Equal(t, kernelLinkID, *gotSpec.KernelLinkID)
	assert.Equal(t, kernel.ProgramID(42), gotSpec.ProgramID, "ProgramID should match the program kernel ID passed to CreateLink")
	require.NotNil(t, gotSpec.PinPath)
	assert.Equal(t, pinPath, *gotSpec.PinPath)

	tpDetails, ok := gotSpec.Details.(bpfman.TracepointDetails)
	require.True(t, ok, "expected TracepointDetails")
	assert.Equal(t, details.Group, tpDetails.Group)
	assert.Equal(t, details.Name, tpDetails.Name)
}

func TestLinkRegistry_MetadataRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), prog), "Save failed")

	// A link carrying user metadata.
	klid := kernel.LinkID(100)
	spec := bpfman.NewPinnedLinkSpec(
		kernel.ProgramID(42), &klid,
		bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_openat"},
		bpfman.LinkPath("/sys/fs/bpf/bpfman/test/link"),
	)
	spec.Metadata = map[string]string{"owner": "acme", "env": "test"}

	record, err := store.CreateLink(ctx, spec)
	require.NoError(t, err, "CreateLink failed")
	assert.Equal(t, spec.Metadata, record.Metadata, "CreateLink should return the metadata it persisted")

	want := map[string]string{"owner": "acme", "env": "test"}

	got, err := store.GetLink(ctx, record.ID)
	require.NoError(t, err, "GetLink failed")
	assert.Equal(t, want, got.Metadata, "metadata must round-trip through GetLink within the link's own transaction")

	// scanLinkRecords backs both list read paths; assert metadata survives each.
	listed, err := store.ListLinks(ctx)
	require.NoError(t, err, "ListLinks failed")
	assert.Equal(t, want, findLinkByID(t, listed, record.ID).Metadata, "metadata must survive ListLinks")

	byProgram, err := store.ListLinksByProgram(ctx, kernel.ProgramID(42))
	require.NoError(t, err, "ListLinksByProgram failed")
	assert.Equal(t, want, findLinkByID(t, byProgram, record.ID).Metadata, "metadata must survive ListLinksByProgram")

	// A link with no metadata round-trips as empty, not an error.
	klid2 := kernel.LinkID(101)
	bare := bpfman.NewPinnedLinkSpec(
		kernel.ProgramID(42), &klid2,
		bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_close"},
		bpfman.LinkPath("/sys/fs/bpf/bpfman/test/link2"),
	)
	bareRec, err := store.CreateLink(ctx, bare)
	require.NoError(t, err, "CreateLink (no metadata) failed")
	gotBare, err := store.GetLink(ctx, bareRec.ID)
	require.NoError(t, err, "GetLink (no metadata) failed")
	assert.Empty(t, gotBare.Metadata, "absent metadata round-trips as empty")
}

func TestLinkRegistry_UpsertUpdatesPinPath(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program for XDP link details.
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), prog), "Save failed")

	// Create dispatcher via snapshot API.
	dispLinkID := kernel.LinkID(500)
	memberKernelLinkID := kernel.LinkID(600)
	snap := platform.DispatcherSnapshotSpec{
		Key:      dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 0, Ifindex: 2},
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    kernel.ProgramID(900),
			KernelLinkID: &dispLinkID,
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(42),
				ProgramName:  "test_prog",
				ProgPinPath:  "/sys/fs/bpf/test_prog",
				KernelLinkID: &memberKernelLinkID,
				LinkPinPath:  "/old/rev/link_0",
				Position:     0,
				Priority:     50,
				ProceedOn:    0x04,
				Ifname:       "eth0",
			},
		},
	}
	completed, err := store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err, "ReplaceDispatcherSnapshot failed")
	require.Len(t, completed.Members, 1)

	// Simulate dispatcher rebuild: replace snapshot with updated
	// pin path and position.
	snap2 := snap
	snap2.Revision = 2
	rebuiltKernelLinkID := kernel.LinkID(601)
	snap2.Members = []platform.DispatcherMemberSpec{
		{
			ExistingLinkID: &completed.Members[0].LinkID,
			ProgramID:      kernel.ProgramID(42),
			ProgramName:    "test_prog",
			ProgPinPath:    "/sys/fs/bpf/test_prog",
			KernelLinkID:   &rebuiltKernelLinkID,
			LinkPinPath:    "/new/rev/link_1",
			Position:       1,
			Priority:       50,
			ProceedOn:      0x04,
			Ifname:         "eth0",
		},
	}
	completed2, err := store.ReplaceDispatcherSnapshot(ctx, snap2)
	require.NoError(t, err, "ReplaceDispatcherSnapshot (rebuild) failed")
	assert.Equal(t, completed.Members[0].LinkID, completed2.Members[0].LinkID)

	// Verify pin path was updated in registry.
	record, err := store.GetLink(ctx, completed.Members[0].LinkID)
	require.NoError(t, err, "GetLink failed")
	require.NotNil(t, record.PinPath, "pin path should not be nil")
	assert.Equal(t, "/new/rev/link_1", record.PinPath.String(), "pin path should be updated to new value")

	// Verify detail record has new position and revision.
	xdp, ok := record.Details.(bpfman.XDPDetails)
	require.True(t, ok, "expected XDPDetails")
	assert.Equal(t, int32(1), xdp.Position, "position should be updated")
	assert.Equal(t, uint32(2), xdp.Revision, "revision should be updated")
}

func TestLinkRegistry_CascadeDeleteFromRegistry(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program first
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), prog), "Save failed")

	// Create a tracepoint link
	kernelLinkID := kernel.LinkID(100)
	details := bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_openat"}
	record := createEphemeralLink(t, ctx, store, kernel.ProgramID(42), &kernelLinkID, details)

	// Delete the link via registry
	require.NoError(t, store.DeleteLink(ctx, record.ID), "DeleteLink failed")

	// Verify link is gone
	_, err = store.GetLink(ctx, record.ID)
	require.Error(t, err, "expected link to be deleted")
}

func TestLinkRegistry_KernelLinkIDPartialUniqueIndex(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), testProgram()), "Save failed")

	kernelLinkID := kernel.LinkID(100)
	createEphemeralLink(t, ctx, store, kernel.ProgramID(42), &kernelLinkID,
		bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_openat"})

	_, err = store.CreateLink(ctx, bpfman.NewEphemeralLinkSpec(
		kernel.ProgramID(42),
		&kernelLinkID,
		bpfman.TracepointDetails{Group: "syscalls", Name: "sys_exit_openat"},
	))
	require.Error(t, err, "duplicate non-null kernel_link_id should fail")
	assert.Contains(t, err.Error(), "UNIQUE constraint failed")

	createEphemeralLink(t, ctx, store, kernel.ProgramID(42), nil,
		bpfman.TracepointDetails{Group: "syscalls", Name: "no_kernel_id_a"})
	createEphemeralLink(t, ctx, store, kernel.ProgramID(42), nil,
		bpfman.TracepointDetails{Group: "syscalls", Name: "no_kernel_id_b"})

	links, err := store.ListLinks(ctx)
	require.NoError(t, err)
	require.Len(t, links, 3, "partial unique index should allow multiple NULL kernel_link_id rows")
}

func TestDeleteLink_RejectsDispatcherBackedLinks(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a managed program for the extension.
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), prog), "Save failed")

	// Create an XDP dispatcher with one member.
	dispLinkID := kernel.LinkID(500)
	memberKernelLinkID := kernel.LinkID(501)
	snap := platform.DispatcherSnapshotSpec{
		Key:      dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 0, Ifindex: 2},
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    kernel.ProgramID(900),
			KernelLinkID: &dispLinkID,
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(42),
				ProgramName:  "test_program",
				ProgPinPath:  "/sys/fs/bpf/test",
				KernelLinkID: &memberKernelLinkID,
				Position:     0,
				Priority:     50,
				ProceedOn:    0x04,
				Ifname:       "eth0",
			},
		},
	}
	completed, err := store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err, "ReplaceDispatcherSnapshot failed")
	require.Len(t, completed.Members, 1)
	memberLinkID := completed.Members[0].LinkID

	// Attempting to delete the dispatcher-backed XDP link should fail.
	err = store.DeleteLink(ctx, memberLinkID)
	require.Error(t, err, "expected DeleteLink to reject dispatcher-backed link")
	assert.Contains(t, err.Error(), "dispatcher-backed")

	// The link should still exist.
	_, err = store.GetLink(ctx, memberLinkID)
	require.NoError(t, err, "link should still exist after rejected delete")
}

func TestCreateLink_RejectsDispatcherBackedKinds(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), prog), "Save failed")

	_, err = store.CreateLink(ctx, bpfman.NewPinnedLinkSpec(
		kernel.ProgramID(42),
		ptr(kernel.LinkID(500)),
		bpfman.XDPDetails{Interface: "eth0", Ifindex: 2},
		"/sys/fs/bpf/xdp",
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatcher-backed")

	_, err = store.CreateLink(ctx, bpfman.NewPinnedLinkSpec(
		kernel.ProgramID(42),
		ptr(kernel.LinkID(501)),
		bpfman.TCDetails{Interface: "eth0", Ifindex: 2, Direction: bpfman.TCDirectionIngress},
		"/sys/fs/bpf/tc",
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatcher-backed")
}

// ----------------------------------------------------------------------------
// Map Ownership Tests
// ----------------------------------------------------------------------------

func TestMapOwnership_MapSetSurvivesDeletingOwner(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create the owner program.
	ownerID := kernel.ProgramID(100)
	ownerProg := testProgram()
	ownerProg.Meta.Name = "owner"
	ownerProg.Handles.MapsDir = "/sys/fs/bpf/bpfman/100"
	require.NoError(t, store.Save(ctx, ownerID, ownerProg), "Save owner failed")

	// Create a dependent program.
	depProg := testProgram()
	depProg.Meta.Name = "dependent"
	depProg.Handles.MapOwnerID = &ownerID
	depProg.Handles.MapsDir = "/sys/fs/bpf/bpfman/100"
	require.NoError(t, store.Save(ctx, kernel.ProgramID(101), depProg), "Save dependent failed")

	// Deleting the owner row is allowed. Dependents reference the durable map
	// set, not the owner program row.
	require.NoError(t, store.Delete(ctx, ownerID), "Delete owner failed")

	got, err := store.Get(ctx, kernel.ProgramID(101))
	require.NoError(t, err, "Get dependent failed")
	require.NotNil(t, got.Handles.MapOwnerID)
	assert.Equal(t, ownerID, *got.Handles.MapOwnerID)

	users, err := store.CountMapSetUsers(ctx, ownerID)
	require.NoError(t, err, "CountMapSetUsers failed")
	assert.Equal(t, 1, users)

	err = store.DeleteMapSet(ctx, ownerID)
	require.Error(t, err, "expected FK constraint violation while dependent uses map set")
	assert.Contains(t, err.Error(), "FOREIGN KEY constraint failed", "expected FK constraint error, got: %v", err)

	require.NoError(t, store.Delete(ctx, kernel.ProgramID(101)), "Delete dependent failed")
	require.NoError(t, store.DeleteMapSet(ctx, ownerID), "Delete map set failed after users removed")
}

func TestMapOwnership_MapPinPathPersisted(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program with MapPinPath set.
	programID := kernel.ProgramID(42)
	prog := testProgram()
	prog.Handles.MapsDir = "/sys/fs/bpf/bpfman/42"

	require.NoError(t, store.Save(ctx, programID, prog), "Save failed")

	// Retrieve and verify MapPinPath is persisted.
	got, err := store.Get(ctx, programID)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, "/sys/fs/bpf/bpfman/42", got.Handles.MapsDir.String(), "MapPinPath mismatch")
}

func TestMapOwnership_MapOwnerIDPersisted(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create the owner program first.
	ownerID := kernel.ProgramID(100)
	ownerProg := testProgram()
	ownerProg.Meta.Name = "owner"
	require.NoError(t, store.Save(ctx, ownerID, ownerProg), "Save owner failed")

	// Create a dependent program with MapOwnerID set.
	depID := kernel.ProgramID(101)
	depProg := testProgram()
	depProg.Meta.Name = "dependent"
	depProg.Handles.MapOwnerID = &ownerID
	depProg.Handles.MapsDir = "/sys/fs/bpf/bpfman/100"

	require.NoError(t, store.Save(ctx, depID, depProg), "Save dependent failed")

	// Retrieve and verify MapOwnerID is persisted.
	got, err := store.Get(ctx, depID)
	require.NoError(t, err, "Get failed")
	require.NotNil(t, got.Handles.MapOwnerID, "MapOwnerID should not be nil")
	assert.Equal(t, ownerID, *got.Handles.MapOwnerID, "MapOwnerID mismatch")
}

func TestMapOwnership_ListIncludesMapFields(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create owner.
	ownerID := kernel.ProgramID(100)
	ownerProg := testProgram()
	ownerProg.Meta.Name = "owner"
	ownerProg.Handles.MapsDir = "/sys/fs/bpf/bpfman/100"
	require.NoError(t, store.Save(ctx, ownerID, ownerProg), "Save owner failed")

	// Create dependent.
	depID := kernel.ProgramID(101)
	depProg := testProgram()
	depProg.Meta.Name = "dependent"
	depProg.Handles.MapOwnerID = &ownerID
	depProg.Handles.MapsDir = "/sys/fs/bpf/bpfman/100"
	require.NoError(t, store.Save(ctx, depID, depProg), "Save dependent failed")

	// List all programs.
	programs, err := store.List(ctx)
	require.NoError(t, err, "List failed")
	require.Len(t, programs, 2, "expected 2 programs")

	// Verify owner has MapPinPath but no MapOwnerID.
	owner := programs[ownerID]
	assert.Equal(t, "/sys/fs/bpf/bpfman/100", owner.Handles.MapsDir.String(), "owner MapPinPath mismatch")
	assert.Nil(t, owner.Handles.MapOwnerID, "owner should have no MapOwnerID")

	// Verify dependent has both fields.
	dep := programs[depID]
	assert.Equal(t, "/sys/fs/bpf/bpfman/100", dep.Handles.MapsDir.String(), "dependent MapPinPath mismatch")
	require.NotNil(t, dep.Handles.MapOwnerID, "dependent should have MapOwnerID set")
	assert.Equal(t, ownerID, *dep.Handles.MapOwnerID, "dependent MapOwnerID mismatch")
}

// TestListTCXLinksByInterface_OrderByPriority verifies that TCX links are
// returned in priority order (ascending), which is critical for correctly
// computing attach order when inserting new TCX programs.
func TestListTCXLinksByInterface_OrderByPriority(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program for the links to reference.
	progID := kernel.ProgramID(100)
	prog := testProgram()
	prog.Load = bpfman.TestLoadSpec(bpfman.ProgramTypeTCX)
	require.NoError(t, store.Save(ctx, progID, prog), "Save program failed")

	// Create TCX links with varying priorities (insert out of order).
	const (
		nsid    = uint64(4026531840)
		ifindex = uint32(2)
	)
	direction := bpfman.TCDirectionIngress

	// Insert links with priorities: 300, 100, 500, 200 (intentionally unordered)
	linksToCreate := []struct {
		linkID   uint32
		priority int32
	}{
		{linkID: 1001, priority: 300},
		{linkID: 1002, priority: 100},
		{linkID: 1003, priority: 500},
		{linkID: 1004, priority: 200},
	}

	for _, link := range linksToCreate {
		details := bpfman.TCXDetails{
			Interface: "eth0",
			Ifindex:   ifindex,
			Direction: direction,
			Priority:  link.priority,
			Nsid:      nsid,
		}
		kernelLinkID := kernel.LinkID(link.linkID)
		createPinnedLink(t, ctx, store, progID, &kernelLinkID, details, bpfman.LinkPath("/sys/fs/bpf/link_"+string(rune(link.linkID))))
	}

	// Query links - they should be ordered by priority ASC.
	links, err := store.ListTCXLinksByInterface(ctx, nsid, ifindex, direction.String())
	require.NoError(t, err, "ListTCXLinksByInterface failed")
	require.Len(t, links, 4, "expected 4 links")

	// Verify order: priorities should be 100, 200, 300, 500
	expectedPriorities := []int32{100, 200, 300, 500}
	for i, link := range links {
		assert.Equal(t, expectedPriorities[i], link.Priority, "link at position %d has wrong priority", i)
	}

	// Verify the correct kernel link IDs are in order
	expectedKernelLinkIDs := []kernel.LinkID{1002, 1004, 1001, 1003}
	for i, link := range links {
		assert.Equal(t, expectedKernelLinkIDs[i], link.KernelLinkID, "link at position %d has wrong kernel_link_id", i)
	}
}

// TestListTCXLinksByInterface_FiltersByInterfaceAndDirection verifies that
// only links matching the specified nsid, ifindex, and direction are returned.
func TestListTCXLinksByInterface_FiltersByInterfaceAndDirection(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program for the links to reference.
	progID := kernel.ProgramID(100)
	prog := testProgram()
	prog.Load = bpfman.TestLoadSpec(bpfman.ProgramTypeTCX)
	require.NoError(t, store.Save(ctx, progID, prog), "Save program failed")

	const nsid = uint64(4026531840)

	// Create links on different interfaces and directions.
	testLinks := []struct {
		linkID    uint32
		ifindex   uint32
		direction bpfman.TCDirection
		priority  int32
	}{
		{linkID: 1001, ifindex: 2, direction: bpfman.TCDirectionIngress, priority: 100},
		{linkID: 1002, ifindex: 2, direction: bpfman.TCDirectionIngress, priority: 200},
		{linkID: 1003, ifindex: 2, direction: bpfman.TCDirectionEgress, priority: 100},  // different direction
		{linkID: 1004, ifindex: 3, direction: bpfman.TCDirectionIngress, priority: 100}, // different interface
	}

	for _, link := range testLinks {
		details := bpfman.TCXDetails{
			Interface: "eth0",
			Ifindex:   link.ifindex,
			Direction: link.direction,
			Priority:  link.priority,
			Nsid:      nsid,
		}
		kernelLinkID := kernel.LinkID(link.linkID)
		createEphemeralLink(t, ctx, store, progID, &kernelLinkID, details)
	}

	// Query for ifindex=2, ingress - should return only 2 links.
	links, err := store.ListTCXLinksByInterface(ctx, nsid, 2, "ingress")
	require.NoError(t, err)
	require.Len(t, links, 2, "expected 2 links for ifindex=2, ingress")
	assert.Equal(t, kernel.LinkID(1001), links[0].KernelLinkID)
	assert.Equal(t, kernel.LinkID(1002), links[1].KernelLinkID)

	// Query for ifindex=2, egress - should return only 1 link.
	links, err = store.ListTCXLinksByInterface(ctx, nsid, 2, "egress")
	require.NoError(t, err)
	require.Len(t, links, 1, "expected 1 link for ifindex=2, egress")
	assert.Equal(t, kernel.LinkID(1003), links[0].KernelLinkID)

	// Query for ifindex=3, ingress - should return only 1 link.
	links, err = store.ListTCXLinksByInterface(ctx, nsid, 3, "ingress")
	require.NoError(t, err)
	require.Len(t, links, 1, "expected 1 link for ifindex=3, ingress")
	assert.Equal(t, kernel.LinkID(1004), links[0].KernelLinkID)

	// Query for non-existent interface - should return empty.
	links, err = store.ListTCXLinksByInterface(ctx, nsid, 99, "ingress")
	require.NoError(t, err)
	require.Len(t, links, 0, "expected 0 links for non-existent interface")
}

// TestListTCXLinksByInterface_EmptyResult verifies that querying for
// an interface with no TCX links returns an empty slice, not nil.
func TestListTCXLinksByInterface_EmptyResult(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	links, err := store.ListTCXLinksByInterface(ctx, 4026531840, 2, "ingress")
	require.NoError(t, err, "ListTCXLinksByInterface should not error for empty result")
	assert.NotNil(t, links, "result should not be nil")
	assert.Empty(t, links, "result should be empty")
}

// -----------------------------------------------------------------------------
// Store GC Schema Tests
//
// These tests exercise the store operations that garbage collection
// relies on, verifying that FK constraints, deletion ordering, and
// CountDispatcherLinks behave correctly against the real schema.
// The GC decision logic itself is tested separately as a pure
// function in manager/gc_test.go.
// -----------------------------------------------------------------------------

func TestStoreGC_DependentBeforeOwnerDeletion(t *testing.T) {
	t.Parallel()

	// Deleting dependents before owners must succeed under FK constraints.
	// Deleting an owner while dependents still reference it must fail.
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	owner := testProgram()
	owner.Meta.Name = "owner"
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), owner))

	ownerID := kernel.ProgramID(100)
	dep1 := testProgram()
	dep1.Meta.Name = "dep1"
	dep1.Handles.MapOwnerID = &ownerID
	require.NoError(t, store.Save(ctx, kernel.ProgramID(101), dep1))

	dep2 := testProgram()
	dep2.Meta.Name = "dep2"
	dep2.Handles.MapOwnerID = &ownerID
	require.NoError(t, store.Save(ctx, kernel.ProgramID(102), dep2))

	// Correct order: dependents first, then owner.
	err = store.RunInTransaction(ctx, "test", func(tx platform.Store) error {
		if err := tx.Delete(ctx, kernel.ProgramID(101)); err != nil {
			return err
		}
		if err := tx.Delete(ctx, kernel.ProgramID(102)); err != nil {
			return err
		}
		return tx.Delete(ctx, kernel.ProgramID(100))
	})
	require.NoError(t, err, "deleting dependents then owner should succeed")

	programs, err := store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, programs)
}

func TestStoreGC_StaleProgramDeletion(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog))
	require.NoError(t, store.Save(ctx, kernel.ProgramID(101), prog))
	require.NoError(t, store.Save(ctx, kernel.ProgramID(102), prog))

	// Delete 101 and 102, keep 100.
	require.NoError(t, store.Delete(ctx, kernel.ProgramID(101)))
	require.NoError(t, store.Delete(ctx, kernel.ProgramID(102)))

	programs, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, programs, 1)
	_, exists := programs[100]
	assert.True(t, exists, "program 100 should survive")
}

func TestStoreGC_StaleDispatcherDeletion(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	xdpKey := dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 4026531840, Ifindex: 2}
	xdpLinkID := kernel.LinkID(200)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: xdpKey, Revision: 1,
		Runtime: platform.DispatcherRuntime{ProgramID: 100, KernelLinkID: &xdpLinkID},
	})
	require.NoError(t, err)

	tcKey := dispatcher.Key{Type: dispatcher.DispatcherTypeTCIngress, Nsid: 4026531840, Ifindex: 3}
	tcPri := uint16(50)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: tcKey, Revision: 1,
		Runtime: platform.DispatcherRuntime{ProgramID: 101, FilterPriority: &tcPri, FilterHandle: ptr(uint32(1))},
	})
	require.NoError(t, err)

	// Delete the TC dispatcher, keep the XDP one.
	require.NoError(t, store.DeleteDispatcherSnapshot(ctx, tcKey))

	_, err = store.GetDispatcherSnapshot(ctx, xdpKey)
	require.NoError(t, err, "XDP dispatcher should survive")

	_, err = store.GetDispatcherSnapshot(ctx, tcKey)
	require.Error(t, err, "TC dispatcher should be deleted")
}

func TestStoreGC_StaleLinkDeletion(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog))

	details1 := bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_openat"}
	link1 := createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(200)), details1)

	details2 := bpfman.TracepointDetails{Group: "syscalls", Name: "sys_exit_openat"}
	link2 := createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(201)), details2)

	// Delete link2, keep link1.
	require.NoError(t, store.DeleteLink(ctx, link2.ID))

	links, err := store.ListLinks(ctx)
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, link1.ID, links[0].ID)
}

func TestStoreGC_OrphanedDispatcherAfterLinkDeletion(t *testing.T) {
	t.Parallel()

	// After deleting extension links via snapshot replace with no
	// members, the dispatcher should have zero members.
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog))

	key := dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 4026531840, Ifindex: 2}
	dispLinkID := kernel.LinkID(501)
	memberKernelLinkID := kernel.LinkID(601)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: key, Revision: 1,
		Runtime: platform.DispatcherRuntime{ProgramID: 500, KernelLinkID: &dispLinkID},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(100),
				ProgramName:  "test_prog",
				ProgPinPath:  "/sys/fs/bpf/test_prog",
				KernelLinkID: &memberKernelLinkID,
				Position:     0,
				Priority:     50,
				ProceedOn:    0x04,
				Ifname:       "eth0",
			},
		},
	})
	require.NoError(t, err)

	// Before removal, dispatcher has one member.
	snap, err := store.GetDispatcherSnapshot(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, 1, len(snap.Members))

	// Replace with zero members (simulating detach of last extension).
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: key, Revision: 2,
		Runtime: platform.DispatcherRuntime{ProgramID: 500, KernelLinkID: &dispLinkID},
	})
	require.NoError(t, err)

	// After replacement, dispatcher has zero members.
	snap, err = store.GetDispatcherSnapshot(ctx, key)
	require.NoError(t, err)
	assert.Empty(t, snap.Members, "dispatcher should have no remaining members")

	// Deleting the now-orphaned dispatcher should succeed.
	require.NoError(t, store.DeleteDispatcherSnapshot(ctx, key))

	summaries, err := store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, summaries)
}

func TestStoreGC_TransactionalAtomicity(t *testing.T) {
	t.Parallel()

	// All GC deletions within a transaction commit together.
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog))
	require.NoError(t, store.Save(ctx, kernel.ProgramID(101), prog))

	tcKey := dispatcher.Key{Type: dispatcher.DispatcherTypeTCIngress, Nsid: 4026531840, Ifindex: 3}
	tcPri := uint16(50)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: tcKey, Revision: 1,
		Runtime: platform.DispatcherRuntime{ProgramID: 101, FilterPriority: &tcPri, FilterHandle: ptr(uint32(1))},
	})
	require.NoError(t, err)

	details := bpfman.TracepointDetails{Group: "syscalls", Name: "test"}
	link := createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(400)), details)

	err = store.RunInTransaction(ctx, "test", func(tx platform.Store) error {
		if err := tx.Delete(ctx, kernel.ProgramID(101)); err != nil {
			return err
		}
		if err := tx.DeleteDispatcherSnapshot(ctx, tcKey); err != nil {
			return err
		}
		return tx.DeleteLink(ctx, link.ID)
	})
	require.NoError(t, err)

	programs, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, programs, 1, "program 100 should survive")

	summaries, err := store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, summaries)

	links, err := store.ListLinks(ctx)
	require.NoError(t, err)
	assert.Empty(t, links)
}

func TestStoreGC_ComprehensiveFourPhaseTransaction(t *testing.T) {
	t.Parallel()

	// Exercises all four GC phases within a single transaction,
	// matching production behaviour. Tests the interaction between
	// FK-ordered program deletion, stale dispatcher removal, link
	// deletion, and orphaned dispatcher detection.
	//
	// Setup:
	//   Programs: 100 (alive), 101 (stale owner), 102 (stale dependent of 101)
	//   Dispatchers: XDP ifindex=2 (programID=100, alive), TC ifindex=3 (programID=101, stale)
	//   Links: 400 (alive tracepoint), 401 (stale tracepoint)
	//
	// After GC:
	//   Programs removed: 102 (dependent), 101 (owner)
	//   Dispatchers removed: TC (stale), XDP (orphaned -- zero extension links)
	//   Links removed: 401 (stale)
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Programs.
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog))

	ownerProg := testProgram()
	ownerProg.Meta.Name = "stale_owner"
	require.NoError(t, store.Save(ctx, kernel.ProgramID(101), ownerProg))

	staleOwnerID := kernel.ProgramID(101)
	depProg := testProgram()
	depProg.Meta.Name = "stale_dep"
	depProg.Handles.MapOwnerID = &staleOwnerID
	require.NoError(t, store.Save(ctx, kernel.ProgramID(102), depProg))

	// Dispatchers.
	xdpKey := dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 4026531840, Ifindex: 2}
	xdpLinkID := kernel.LinkID(300)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: xdpKey, Revision: 1,
		Runtime: platform.DispatcherRuntime{ProgramID: 100, KernelLinkID: &xdpLinkID},
	})
	require.NoError(t, err)

	tcKey := dispatcher.Key{Type: dispatcher.DispatcherTypeTCIngress, Nsid: 4026531840, Ifindex: 3}
	tcPri := uint16(50)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key: tcKey, Revision: 1,
		Runtime: platform.DispatcherRuntime{ProgramID: 101, FilterPriority: &tcPri, FilterHandle: ptr(uint32(1))},
	})
	require.NoError(t, err)

	// Links (tracepoints, not XDP extensions -- so the XDP
	// dispatcher has zero extension links from the start).
	aliveLink := createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(400)),
		bpfman.TracepointDetails{Group: "syscalls", Name: "test"})

	staleLink := createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(401)),
		bpfman.TracepointDetails{Group: "syscalls", Name: "test2"})

	// Execute all four phases in a single transaction.
	err = store.RunInTransaction(ctx, "test", func(tx platform.Store) error {
		// Phase 1: delete dependent then owner.
		if err := tx.Delete(ctx, kernel.ProgramID(102)); err != nil {
			return err
		}
		if err := tx.Delete(ctx, kernel.ProgramID(101)); err != nil {
			return err
		}

		// Phase 2: delete stale dispatcher.
		if err := tx.DeleteDispatcherSnapshot(ctx, tcKey); err != nil {
			return err
		}

		// Phase 3: delete stale link.
		if err := tx.DeleteLink(ctx, staleLink.ID); err != nil {
			return err
		}

		// Phase 4: check for orphaned dispatchers.
		snap, err := tx.GetDispatcherSnapshot(ctx, xdpKey)
		if err != nil {
			return err
		}
		if len(snap.Members) == 0 {
			if err := tx.DeleteDispatcherSnapshot(ctx, xdpKey); err != nil {
				return err
			}
		}

		return nil
	})
	require.NoError(t, err)

	// Verify final state.
	programs, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, programs, 1, "should have 1 program remaining")
	_, exists := programs[100]
	assert.True(t, exists, "program 100 should exist")

	summaries, err := store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, summaries, "all dispatchers should be removed")

	links, err := store.ListLinks(ctx)
	require.NoError(t, err)
	assert.Len(t, links, 1, "should have 1 link remaining")
	assert.Equal(t, aliveLink.ID, links[0].ID)
}

func TestListLinks_ReturnsDetails(t *testing.T) {
	t.Parallel()

	// Verify that ListLinks() returns LinkSpec with Details populated
	// for ALL link detail types. This is critical for inspect.Snapshot()
	// to build a complete Observation where the ATTACH column can display
	// meaningful information.
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create a program first (FK requirement for links)
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog), "Save program failed")

	// Create dispatchers for XDP and TC links (FK requirement for their details)
	xdpLinkID := kernel.LinkID(501)
	xdpMemberLinkID := kernel.LinkID(60)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key:      dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 4026531840, Ifindex: 2},
		Revision: 1,
		Runtime:  platform.DispatcherRuntime{ProgramID: 500, KernelLinkID: &xdpLinkID},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(100),
				ProgramName:  "xdp_prog",
				ProgPinPath:  "/sys/fs/bpf/xdp_prog",
				KernelLinkID: &xdpMemberLinkID,
				LinkPinPath:  "/sys/fs/bpf/xdp_link",
				Position:     1,
				Priority:     50,
				ProceedOn:    uint32(1<<2 | 1<<31),
				Ifname:       "eth0",
			},
		},
	})
	require.NoError(t, err, "ReplaceDispatcherSnapshot XDP failed")

	tcFilterPriority := uint16(50)
	tcMemberLinkID := kernel.LinkID(70)
	_, err = store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key:      dispatcher.Key{Type: dispatcher.DispatcherTypeTCIngress, Nsid: 4026531840, Ifindex: 3},
		Revision: 1,
		Runtime:  platform.DispatcherRuntime{ProgramID: 502, FilterPriority: &tcFilterPriority, FilterHandle: ptr(uint32(1))},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(100),
				ProgramName:  "tc_prog",
				ProgPinPath:  "/sys/fs/bpf/tc_prog",
				KernelLinkID: &tcMemberLinkID,
				LinkPinPath:  "/sys/fs/bpf/tc_link",
				Position:     1,
				Priority:     100,
				ProceedOn:    uint32(1<<1 | 1<<4),
				Ifname:       "eth1",
			},
		},
	})
	require.NoError(t, err, "ReplaceDispatcherSnapshot TC failed")

	// Create links with ALL detail types
	testCases := []struct {
		linkID  kernel.LinkID
		details bpfman.LinkDetails
		check   func(t *testing.T, got bpfman.LinkDetails)
	}{
		{
			linkID:  10,
			details: bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.TracepointDetails)
				require.True(t, ok, "expected TracepointDetails, got %T", got)
				assert.Equal(t, "sched", d.Group)
				assert.Equal(t, "sched_switch", d.Name)
			},
		},
		{
			linkID:  20,
			details: bpfman.KprobeDetails{FnName: "do_sys_open", Offset: 64, Retprobe: true},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.KprobeDetails)
				require.True(t, ok, "expected KprobeDetails, got %T", got)
				assert.Equal(t, "do_sys_open", d.FnName)
				assert.Equal(t, uint64(64), d.Offset)
				assert.True(t, d.Retprobe)
			},
		},
		{
			linkID:  30,
			details: bpfman.UprobeDetails{Target: "/usr/bin/test", FnName: "main", Offset: 128, PID: 1234, Retprobe: false, ContainerPid: 5678},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.UprobeDetails)
				require.True(t, ok, "expected UprobeDetails, got %T", got)
				assert.Equal(t, "/usr/bin/test", d.Target)
				assert.Equal(t, "main", d.FnName)
				assert.Equal(t, uint64(128), d.Offset)
				assert.Equal(t, int32(1234), d.PID)
				assert.Equal(t, int32(5678), d.ContainerPid)
				assert.False(t, d.Retprobe)
			},
		},
		{
			linkID:  40,
			details: bpfman.FentryDetails{FnName: "tcp_connect"},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.FentryDetails)
				require.True(t, ok, "expected FentryDetails, got %T", got)
				assert.Equal(t, "tcp_connect", d.FnName)
			},
		},
		{
			linkID:  50,
			details: bpfman.FexitDetails{FnName: "tcp_disconnect"},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.FexitDetails)
				require.True(t, ok, "expected FexitDetails, got %T", got)
				assert.Equal(t, "tcp_disconnect", d.FnName)
			},
		},
		{
			linkID: 60,
			details: bpfman.XDPDetails{
				Interface:    "eth0",
				Ifindex:      2,
				Priority:     50,
				Position:     1,
				ProceedOn:    []int32{2, 31}, // XDP_PASS=2, XDP_DISPATCHER_RETURN=31
				Netns:        "/proc/1/ns/net",
				Nsid:         4026531840,
				DispatcherID: 500, // References XDP dispatcher created above
				Revision:     1,
			},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.XDPDetails)
				require.True(t, ok, "expected XDPDetails, got %T", got)
				assert.Equal(t, "eth0", d.Interface)
				assert.Equal(t, uint32(2), d.Ifindex)
				assert.Equal(t, int32(50), d.Priority)
				assert.Equal(t, int32(1), d.Position)
				assert.Equal(t, []int32{2, 31}, d.ProceedOn)
				assert.Equal(t, "", d.Netns)
				assert.Equal(t, uint64(4026531840), d.Nsid)
				assert.Equal(t, kernel.ProgramID(500), d.DispatcherID)
				assert.Equal(t, uint32(1), d.Revision)
			},
		},
		{
			linkID: 70,
			details: bpfman.TCDetails{
				Interface:    "eth1",
				Ifindex:      3,
				Direction:    bpfman.TCDirectionIngress,
				Priority:     100,
				Position:     1,
				ProceedOn:    []int32{0, 3}, // TC_ACT_OK=0, TC_ACT_PIPE=3
				Netns:        "/proc/1/ns/net",
				Nsid:         4026531840,
				DispatcherID: 502, // References TC dispatcher created above
				Revision:     1,
			},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.TCDetails)
				require.True(t, ok, "expected TCDetails, got %T", got)
				assert.Equal(t, "eth1", d.Interface)
				assert.Equal(t, uint32(3), d.Ifindex)
				assert.Equal(t, bpfman.TCDirectionIngress, d.Direction)
				assert.Equal(t, int32(100), d.Priority)
				assert.Equal(t, int32(1), d.Position)
				assert.Equal(t, []int32{0, 3}, d.ProceedOn)
				assert.Equal(t, "", d.Netns)
				assert.Equal(t, uint64(4026531840), d.Nsid)
			},
		},
		{
			linkID: 80,
			details: bpfman.TCXDetails{
				Interface: "eth2",
				Ifindex:   4,
				Direction: bpfman.TCDirectionEgress,
				Priority:  200,
				Netns:     "/proc/1/ns/net",
				Nsid:      4026531840,
			},
			check: func(t *testing.T, got bpfman.LinkDetails) {
				d, ok := got.(bpfman.TCXDetails)
				require.True(t, ok, "expected TCXDetails, got %T", got)
				assert.Equal(t, "eth2", d.Interface)
				assert.Equal(t, uint32(4), d.Ifindex)
				assert.Equal(t, bpfman.TCDirectionEgress, d.Direction)
				assert.Equal(t, int32(200), d.Priority)
				assert.Equal(t, "/proc/1/ns/net", d.Netns)
				assert.Equal(t, uint64(4026531840), d.Nsid)
			},
		},
	}

	// Save all links
	for _, tc := range testCases {
		switch tc.details.Kind() {
		case bpfman.LinkKindXDP, bpfman.LinkKindTC:
			continue
		}
		kernelLinkID := tc.linkID
		createEphemeralLink(t, ctx, store, kernel.ProgramID(100), &kernelLinkID, tc.details)
	}

	// ListLinks should return links WITH details populated
	links, err := store.ListLinks(ctx)
	require.NoError(t, err, "ListLinks failed")
	require.Len(t, links, len(testCases), "expected %d links", len(testCases))

	// Build a map for easier lookup
	linksByID := make(map[kernel.LinkID]bpfman.LinkRecord)
	for _, l := range links {
		require.NotNil(t, l.KernelLinkID)
		linksByID[*l.KernelLinkID] = l
	}

	// Verify each link's details
	for _, tc := range testCases {
		t.Run(tc.details.Kind().String(), func(t *testing.T) {
			t.Parallel()
			link, ok := linksByID[tc.linkID]
			require.True(t, ok, "link %d not found", tc.linkID)
			require.NotNil(t, link.Details, "link %d Details should not be nil", tc.linkID)
			tc.check(t, link.Details)
		})
	}
}

func TestListLinksByProgram_ReturnsDetails(t *testing.T) {
	t.Parallel()

	// Verify that ListLinksByProgram() also returns details.
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	// Create two programs
	prog := testProgram()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(100), prog), "Save program 100 failed")
	require.NoError(t, store.Save(ctx, kernel.ProgramID(200), prog), "Save program 200 failed")

	// Create links for program 100
	tp1 := bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_read"}
	createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(10)), tp1)

	tp2 := bpfman.TracepointDetails{Group: "syscalls", Name: "sys_exit_read"}
	createEphemeralLink(t, ctx, store, kernel.ProgramID(100), ptr(kernel.LinkID(11)), tp2)

	// Create link for program 200
	tp3 := bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_write"}
	createEphemeralLink(t, ctx, store, kernel.ProgramID(200), ptr(kernel.LinkID(20)), tp3)

	// ListLinksByProgram for program 100 should return 2 links with details
	links, err := store.ListLinksByProgram(ctx, kernel.ProgramID(100))
	require.NoError(t, err)
	require.Len(t, links, 2)

	for _, link := range links {
		require.NotNil(t, link.Details, "link %d Details should not be nil", link.ID)
		_, ok := link.Details.(bpfman.TracepointDetails)
		require.True(t, ok, "expected TracepointDetails for link %d", link.ID)
	}
}

// TestCreateLink_DuplicatePinPathRejected pins the schema backstop
// behind the manager's duplicate-attach rejection: pin paths are
// deterministic per attachment key, so two records sharing one pin
// is the corrupted state, and the unique index refuses it even if
// a future code path skips the manager check. Ephemeral links
// carry NULL pins and stay exempt.
func TestCreateLink_DuplicatePinPathRejected(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := sqlite.NewInMemory(ctx, testLogger())
	require.NoError(t, err)
	defer store.Close()

	prog := testProgram()
	require.NoError(t, store.Save(ctx, 4001, prog))

	pin := bpfman.LinkPath("/run/bpfman/fs/tcx-ingress/link_1_2_4001")
	details := bpfman.TCXDetails{Interface: "eth0", Ifindex: 2, Direction: bpfman.TCDirectionIngress, Priority: 50, Nsid: 1}

	_, err = store.CreateLink(ctx, bpfman.NewPinnedLinkSpec(4001, ptr(kernel.LinkID(11)), details, pin))
	require.NoError(t, err)

	_, err = store.CreateLink(ctx, bpfman.NewPinnedLinkSpec(4001, ptr(kernel.LinkID(12)), details, pin))
	require.Error(t, err, "a second record sharing the pin path must be rejected")

	// NULL pins remain exempt: two ephemeral links coexist.
	_, err = store.CreateLink(ctx, bpfman.NewEphemeralLinkSpec(4001, ptr(kernel.LinkID(13)), details))
	require.NoError(t, err)
	_, err = store.CreateLink(ctx, bpfman.NewEphemeralLinkSpec(4001, ptr(kernel.LinkID(14)), details))
	require.NoError(t, err)
}

// TestRunInTransaction_DuplicatePinPathRollsBack proves a unique
// index violation inside a transaction leaves no partial writes:
// the first insert in the same transaction rolls back with the
// failed second, so a constraint error cannot strand half a
// multi-link write.
func TestRunInTransaction_DuplicatePinPathRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := sqlite.NewInMemory(ctx, testLogger())
	require.NoError(t, err)
	defer store.Close()

	prog := testProgram()
	require.NoError(t, store.Save(ctx, 4002, prog))

	pin := bpfman.LinkPath("/run/bpfman/fs/tcx-ingress/link_1_3_4002")
	details := bpfman.TCXDetails{Interface: "eth1", Ifindex: 3, Direction: bpfman.TCDirectionIngress, Priority: 50, Nsid: 1}

	err = store.RunInTransaction(ctx, "dup-pin-test", func(tx platform.Store) error {
		if _, err := tx.CreateLink(ctx, bpfman.NewPinnedLinkSpec(4002, ptr(kernel.LinkID(21)), details, pin)); err != nil {
			return err
		}

		_, err := tx.CreateLink(ctx, bpfman.NewPinnedLinkSpec(4002, ptr(kernel.LinkID(22)), details, pin))
		require.Error(t, err, "duplicate pin inside the transaction must fail")
		return err
	})
	require.Error(t, err)

	links, err := store.ListLinks(ctx)
	require.NoError(t, err)
	assert.Empty(t, links, "the constraint failure must roll back the whole transaction")
}

// The caller's file-load path operand round-trips through Save and Get
// verbatim, and a record saved without one reads back empty rather
// than inheriting the stored-copy object path.
func TestSourcePath_RoundTrip(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()

	withSource := testProgram()
	withSource.Load = withSource.Load.WithSourcePath("e2e/testdata/bpf/xdp_pass.bpf.o")
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), withSource), "Save failed")

	found, err := store.Get(ctx, kernel.ProgramID(42))
	require.NoError(t, err, "Get failed")
	assert.Equal(t, "e2e/testdata/bpf/xdp_pass.bpf.o", found.Load.SourcePath(), "source path mismatch")

	require.NoError(t, store.Save(ctx, kernel.ProgramID(43), testProgram()), "Save failed")

	found, err = store.Get(ctx, kernel.ProgramID(43))
	require.NoError(t, err, "Get failed")
	assert.Empty(t, found.Load.SourcePath(), "record saved without a source path must read back empty")
}
