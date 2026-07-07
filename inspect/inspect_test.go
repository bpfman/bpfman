package inspect_test

import (
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/inspect"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/store/sqlite"
)

func ptr[T any](v T) *T { return &v }

func newRealStore(t *testing.T) platform.Store {
	t.Helper()
	store, err := sqlite.NewInMemory(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func saveInspectProgram(t *testing.T, store platform.Store, id kernel.ProgramID, typ bpfman.ProgramType, name string, pinPath bpfman.ProgPinPath) {
	t.Helper()
	rec := bpfman.ProgramRecord{
		Load:      bpfman.TestLoadSpec(typ),
		Handles:   bpfman.ProgramHandles{PinPath: pinPath},
		Meta:      bpfman.ProgramMeta{Name: name},
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.Save(context.Background(), id, rec))
}

func createInspectLink(t *testing.T, store platform.Store, spec bpfman.LinkSpec) bpfman.LinkRecord {
	t.Helper()
	record, err := store.CreateLink(context.Background(), spec)
	require.NoError(t, err)
	require.NotZero(t, record.ID)
	return record
}

// fakeKernelSource implements KernelLister for testing.
type fakeKernelSource struct {
	programs []kernel.Program
	links    []kernel.Link
}

func (k *fakeKernelSource) Programs(ctx context.Context) iter.Seq2[kernel.Program, error] {
	return func(yield func(kernel.Program, error) bool) {
		for _, p := range k.programs {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (k *fakeKernelSource) GetProgramByID(ctx context.Context, id kernel.ProgramID) (kernel.Program, error) {
	for _, p := range k.programs {
		if p.ID == id {
			return p, nil
		}
	}
	return kernel.Program{}, errors.New("program not found")
}

func (k *fakeKernelSource) Links(ctx context.Context) iter.Seq2[kernel.Link, error] {
	return func(yield func(kernel.Link, error) bool) {
		for _, l := range k.links {
			if !yield(l, nil) {
				return
			}
		}
	}
}

func (k *fakeKernelSource) GetLinkByID(ctx context.Context, id kernel.LinkID) (kernel.Link, error) {
	for _, l := range k.links {
		if l.ID == id {
			return l, nil
		}
	}
	return kernel.Link{}, errors.New("link not found")
}

// testBPFFS creates a BPFFS for testing with a temporary directory.
// Returns the BPFFS and a struct with convenient path accessors.
func testBPFFS(t *testing.T) fs.BPFFS {
	t.Helper()
	layout, err := fs.New(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create layout: %v", err)
	}

	return layout.BPFFS()
}

func TestSnapshot_FSOnlyPrograms(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)

	// Create an orphan prog pin on FS
	require.NoError(t, os.MkdirAll(bpfFS.MountPoint(), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bpfFS.MountPoint(), "prog_888"), nil, 0644))

	scanner := bpfFS.Scanner()
	store := newRealStore(t)
	kern := &fakeKernelSource{}

	w, err := inspect.Snapshot(context.Background(), store, kern, scanner)
	require.NoError(t, err)

	assert.Len(t, w.Programs, 1)
	assert.Equal(t, kernel.ProgramID(888), w.Programs[0].ProgramID)
	assert.True(t, w.Programs[0].Presence.OrphanFS())
	assert.False(t, w.Programs[0].Presence.InStore)
	assert.False(t, w.Programs[0].Presence.InKernel)
	assert.True(t, w.Programs[0].Presence.InFS)
}

func TestSnapshot_Links(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)
	scanner := bpfFS.Scanner()

	store := newRealStore(t)
	saveInspectProgram(t, store, 100, bpfman.ProgramTypeKprobe, "kprobe_prog", "/run/bpfman/fs/prog_100")
	saveInspectProgram(t, store, 200, bpfman.ProgramTypeTracepoint, "tracepoint_prog", "/run/bpfman/fs/prog_200")
	createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID:    100,
		KernelLinkID: ptr(kernel.LinkID(10)),
		Kind:         bpfman.LinkKindKprobe,
		Details:      bpfman.KprobeDetails{FnName: "do_sys_open"},
	})
	createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID:    200,
		KernelLinkID: ptr(kernel.LinkID(20)),
		Kind:         bpfman.LinkKindTracepoint,
		Details:      bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
	})

	kern := &fakeKernelSource{
		links: []kernel.Link{
			{ID: 10, ProgramID: 100},
			{ID: 20, ProgramID: 200},
			{ID: 999}, // kernel-only link
		},
	}

	w, err := inspect.Snapshot(context.Background(), store, kern, scanner)
	require.NoError(t, err)

	assert.Len(t, w.Links, 3)

	managed := w.ManagedLinks()
	assert.Len(t, managed, 2)

	// Check kernel-only link
	var kernelOnly *inspect.LinkRow
	for i := range w.Links {
		if w.Links[i].Presence.KernelOnly() {
			kernelOnly = &w.Links[i]
			break
		}
	}
	require.NotNil(t, kernelOnly)
	require.NotNil(t, kernelOnly.Kernel)
	assert.Equal(t, kernel.LinkID(999), kernelOnly.Kernel.ID)
}

func TestSnapshot_Dispatchers(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)
	require.NoError(t, os.MkdirAll(bpfFS.XDP(), 0755))

	// Create dispatcher dir on FS
	dispDir := filepath.Join(bpfFS.XDP(), "dispatcher_1_1_5")
	require.NoError(t, os.Mkdir(dispDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dispDir, "link_0"), nil, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dispDir, "link_1"), nil, 0644))

	scanner := bpfFS.Scanner()

	linkID := kernel.LinkID(50)
	store := newRealStore(t)
	saveInspectProgram(t, store, 100, bpfman.ProgramTypeXDP, "xdp_prog", "/run/bpfman/fs/prog_100")
	memberKernelLinkID := kernel.LinkID(51)
	_, err := store.ReplaceDispatcherSnapshot(context.Background(), platform.DispatcherSnapshotSpec{
		Key:      dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 1, Ifindex: 1},
		Revision: 5,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    500,
			KernelLinkID: &linkID,
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    100,
				ProgramName:  "xdp_prog",
				ProgPinPath:  "/run/bpfman/fs/prog_100",
				KernelLinkID: &memberKernelLinkID,
				LinkPinPath:  "/run/bpfman/fs/dispatch/link_0",
				Position:     0,
				Priority:     50,
				ProceedOn:    0x04,
				Ifname:       "eth0",
			},
		},
	})
	require.NoError(t, err)

	kern := &fakeKernelSource{
		programs: []kernel.Program{{ID: 500}},
		links:    []kernel.Link{{ID: 50}},
	}

	w, err := inspect.Snapshot(context.Background(), store, kern, scanner)
	require.NoError(t, err)

	assert.Len(t, w.Dispatchers, 1)

	d := w.Dispatchers[0]
	assert.Equal(t, "xdp", d.DispType)
	assert.Equal(t, uint64(1), d.Nsid)
	assert.Equal(t, uint32(1), d.Ifindex)
	assert.Equal(t, uint32(5), d.Revision)
	assert.Equal(t, 2, d.FSLinkCount)
	assert.True(t, d.ProgPresence.InStore)
	assert.True(t, d.ProgPresence.InKernel)
	assert.True(t, d.ProgPresence.InFS)
	require.NotNil(t, d.Managed, "store dispatcher should have Managed set")
	assert.Equal(t, kernel.ProgramID(500), d.Managed.Runtime.ProgramID)
}

func TestSnapshot_OrphanDispatcher(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)
	require.NoError(t, os.MkdirAll(bpfFS.XDP(), 0755))

	// Create orphan dispatcher dir on FS (not in store)
	dispDir := filepath.Join(bpfFS.XDP(), "dispatcher_99_2_1")
	require.NoError(t, os.Mkdir(dispDir, 0755))

	scanner := bpfFS.Scanner()
	store := newRealStore(t)
	kern := &fakeKernelSource{}

	w, err := inspect.Snapshot(context.Background(), store, kern, scanner)
	require.NoError(t, err)

	assert.Len(t, w.Dispatchers, 1)

	d := w.Dispatchers[0]
	assert.Equal(t, "xdp", d.DispType)
	assert.Equal(t, uint64(99), d.Nsid)
	assert.Equal(t, uint32(2), d.Ifindex)
	assert.False(t, d.ProgPresence.InStore)
	assert.False(t, d.ProgPresence.InKernel)
	assert.True(t, d.ProgPresence.InFS)
	assert.Nil(t, d.Managed, "orphan dispatcher should have nil Managed")
}

func TestPresence_Methods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		p          inspect.Presence
		managed    bool
		orphanFS   bool
		kernelOnly bool
	}{
		{
			name:       "in store only",
			p:          inspect.Presence{InStore: true, InKernel: false, InFS: false},
			managed:    true,
			orphanFS:   false,
			kernelOnly: false,
		},
		{
			name:       "fully present",
			p:          inspect.Presence{InStore: true, InKernel: true, InFS: true},
			managed:    true,
			orphanFS:   false,
			kernelOnly: false,
		},
		{
			name:       "kernel only",
			p:          inspect.Presence{InStore: false, InKernel: true, InFS: false},
			managed:    false,
			orphanFS:   false,
			kernelOnly: true,
		},
		{
			name:       "kernel and fs, not store",
			p:          inspect.Presence{InStore: false, InKernel: true, InFS: true},
			managed:    false,
			orphanFS:   false,
			kernelOnly: true,
		},
		{
			name:       "fs only (orphan)",
			p:          inspect.Presence{InStore: false, InKernel: false, InFS: true},
			managed:    false,
			orphanFS:   true,
			kernelOnly: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.managed, tt.p.Managed())
			assert.Equal(t, tt.orphanFS, tt.p.OrphanFS())
			assert.Equal(t, tt.kernelOnly, tt.p.KernelOnly())
		})
	}
}

func TestGetLink_FullyPresent(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)

	// Create a pin file on FS
	pinPath := filepath.Join(bpfFS.Links(), "100", "link_10")
	require.NoError(t, os.MkdirAll(filepath.Dir(pinPath), 0755))
	require.NoError(t, os.WriteFile(pinPath, nil, 0644))

	scanner := bpfFS.Scanner()

	store := newRealStore(t)
	saveInspectProgram(t, store, 100, bpfman.ProgramTypeKprobe, "kprobe_prog", "/run/bpfman/fs/prog_100")
	record := createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID:    100,
		KernelLinkID: ptr(kernel.LinkID(10)),
		Kind:         bpfman.LinkKindKprobe,
		PinPath:      bpfman.NewLinkPath(pinPath),
		Details:      bpfman.KprobeDetails{FnName: "do_sys_open"},
	})

	kern := &fakeKernelSource{
		links: []kernel.Link{{ID: 10, ProgramID: 100}},
	}

	info, err := inspect.GetLink(context.Background(), store, kern, scanner, record.ID)
	require.NoError(t, err)

	assert.Equal(t, record.ID, info.Record.ID)
	require.NotNil(t, info.Record.KernelLinkID)
	assert.Equal(t, kernel.LinkID(10), *info.Record.KernelLinkID)
	assert.True(t, info.Presence.InStore)
	assert.True(t, info.Presence.InKernel)
	assert.True(t, info.Presence.InFS)
	assert.NotNil(t, info.Record.Details)
}

func TestGetLink_StoreOnly(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)
	scanner := bpfFS.Scanner()

	store := newRealStore(t)
	saveInspectProgram(t, store, 100, bpfman.ProgramTypeTracepoint, "tracepoint_prog", "/run/bpfman/fs/prog_100")
	record := createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID: 100,
		Kind:      bpfman.LinkKindTracepoint,
		Details:   bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
	})

	kern := &fakeKernelSource{}

	info, err := inspect.GetLink(context.Background(), store, kern, scanner, record.ID)
	require.NoError(t, err)

	assert.Equal(t, record.ID, info.Record.ID)
	assert.Nil(t, info.Record.KernelLinkID)
	assert.Nil(t, info.Record.PinPath)
	assert.True(t, info.Presence.InStore)
	assert.False(t, info.Presence.InKernel)
	assert.False(t, info.Presence.InFS)
}

func TestGetLink_NotInStore(t *testing.T) {
	t.Parallel()

	// GetLink requires the link to be in the store (it takes a durable LinkID).
	// If the link is not in the store, it returns inspect.ErrNotFound.
	bpfFS := testBPFFS(t)
	scanner := bpfFS.Scanner()

	store := newRealStore(t) // Not in store

	kern := &fakeKernelSource{
		links: []kernel.Link{{ID: 999}},
	}

	// Even though link 999 exists in kernel, we can't look it up by LinkID 999
	// because LinkID is a store-assigned durable ID, not a kernel link ID.
	_, err := inspect.GetLink(context.Background(), store, kern, scanner, bpfman.LinkID(999))
	require.Error(t, err)
	assert.ErrorIs(t, err, inspect.ErrNotFound)
}

func TestGetLink_NotFound(t *testing.T) {
	t.Parallel()

	bpfFS := testBPFFS(t)
	scanner := bpfFS.Scanner()

	store := newRealStore(t)
	kern := &fakeKernelSource{}

	_, err := inspect.GetLink(context.Background(), store, kern, scanner, bpfman.LinkID(12345))
	require.Error(t, err)
	assert.ErrorIs(t, err, inspect.ErrNotFound)
}

func TestSnapshot_LinksHaveDetails(t *testing.T) {
	t.Parallel()

	// Verify that Snapshot() returns an Observation where links have Details populated.
	// This is critical for the ATTACH column in CLI output.
	bpfFS := testBPFFS(t)
	scanner := bpfFS.Scanner()

	store := newRealStore(t)
	saveInspectProgram(t, store, 100, bpfman.ProgramTypeTracepoint, "test_prog", "/run/bpfman/fs/prog_100")
	tpRecord := createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID:    100,
		KernelLinkID: ptr(kernel.LinkID(10)),
		Kind:         bpfman.LinkKindTracepoint,
		Details:      bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
	})
	kpRecord := createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID:    100,
		KernelLinkID: ptr(kernel.LinkID(20)),
		Kind:         bpfman.LinkKindKprobe,
		Details:      bpfman.KprobeDetails{FnName: "do_sys_open"},
	})

	kern := &fakeKernelSource{
		programs: []kernel.Program{{ID: 100}},
		links: []kernel.Link{
			{ID: 10, ProgramID: 100},
			{ID: 20, ProgramID: 100},
		},
	}

	w, err := inspect.Snapshot(context.Background(), store, kern, scanner)
	require.NoError(t, err)

	// Verify links in Observation have details
	managed := w.ManagedLinks()
	require.Len(t, managed, 2)

	for _, linkRow := range managed {
		require.NotNil(t, linkRow.Managed, "Managed should not be nil")
		require.NotNil(t, linkRow.Managed.Details, "link %d Details should not be nil", linkRow.ID())
	}

	// Verify details are correct types
	linksByID := make(map[bpfman.LinkID]inspect.LinkRow)
	for _, l := range managed {
		linksByID[l.ID()] = l
	}

	tpLink := linksByID[tpRecord.ID]
	tpDetails, ok := tpLink.Managed.Details.(bpfman.TracepointDetails)
	require.True(t, ok, "expected TracepointDetails")
	assert.Equal(t, "sched", tpDetails.Group)
	assert.Equal(t, "sched_switch", tpDetails.Name)

	kpLink := linksByID[kpRecord.ID]
	kpDetails, ok := kpLink.Managed.Details.(bpfman.KprobeDetails)
	require.True(t, ok, "expected KprobeDetails")
	assert.Equal(t, "do_sys_open", kpDetails.FnName)
}

func TestSnapshot_ProgramLinksHaveDetails(t *testing.T) {
	t.Parallel()

	// Verify that links correlated to programs also have details populated.
	bpfFS := testBPFFS(t)
	scanner := bpfFS.Scanner()

	store := newRealStore(t)
	saveInspectProgram(t, store, 100, bpfman.ProgramTypeTracepoint, "test_prog", "/run/bpfman/fs/prog_100")
	createInspectLink(t, store, bpfman.LinkSpec{
		ProgramID:    100,
		KernelLinkID: ptr(kernel.LinkID(10)),
		Kind:         bpfman.LinkKindTracepoint,
		Details:      bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
	})

	kern := &fakeKernelSource{
		programs: []kernel.Program{{ID: 100}},
		links:    []kernel.Link{{ID: 10, ProgramID: 100}},
	}

	w, err := inspect.Snapshot(context.Background(), store, kern, scanner)
	require.NoError(t, err)

	// Find the program and verify its correlated links have details
	managed := w.ManagedPrograms()
	require.Len(t, managed, 1)

	prog := managed[0]
	require.Len(t, prog.Links, 1, "program should have 1 correlated link")

	linkRow := prog.Links[0]
	require.NotNil(t, linkRow.Managed)
	require.NotNil(t, linkRow.Managed.Details, "correlated link Details should not be nil")

	tpDetails, ok := linkRow.Managed.Details.(bpfman.TracepointDetails)
	require.True(t, ok)
	assert.Equal(t, "sched", tpDetails.Group)
	assert.Equal(t, "sched_switch", tpDetails.Name)
}
