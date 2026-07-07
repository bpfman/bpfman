package manager_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

func TestDeleteProgramsRecursiveTreatsBatchDependentsAsDeleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ids  func(sharedMapDeleteFixture) []kernel.ProgramID
	}{
		{
			name: "owner first",
			ids: func(f sharedMapDeleteFixture) []kernel.ProgramID {
				return []kernel.ProgramID{f.ownerID, f.dependentID}
			},
		},
		{
			name: "dependent first",
			ids: func(f sharedMapDeleteFixture) []kernel.ProgramID {
				return []kernel.ProgramID{f.dependentID, f.ownerID}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			f := newSharedMapDeleteFixture(t, ctx)
			ids := tt.ids(f)

			var results []manager.DeleteProgramResult
			err := lock.Run(ctx, f.fixture.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
				results = f.fixture.Manager.DeletePrograms(ctx, writeLock, ids, manager.DeleteProgramsOpts{Recursive: true})
				return nil
			})
			require.NoError(t, err)
			require.Len(t, results, 2)
			assert.NoError(t, results[0].Err)
			assert.NoError(t, results[1].Err)

			assertProgramRecordDeleted(t, ctx, f.fixture, f.ownerID)
			assertProgramRecordDeleted(t, ctx, f.fixture, f.dependentID)
		})
	}
}

func TestDeleteProgramsAllRecursiveSucceedsWithSharedMapDependents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newSharedMapDeleteFixture(t, ctx)

	ids, err := f.fixture.Manager.ResolveDeleteProgramIDs(ctx, true, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []kernel.ProgramID{f.ownerID, f.dependentID}, ids)

	var results []manager.DeleteProgramResult
	err = lock.Run(ctx, f.fixture.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
		results = f.fixture.Manager.DeletePrograms(ctx, writeLock, ids, manager.DeleteProgramsOpts{Recursive: true})
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		assert.NoError(t, result.Err)
	}

	assertProgramRecordDeleted(t, ctx, f.fixture, f.ownerID)
	assertProgramRecordDeleted(t, ctx, f.fixture, f.dependentID)
}

func TestDeleteProgramsAllOrdersDependentsWithoutRecursive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newSharedMapDeleteFixture(t, ctx)

	ids, err := f.fixture.Manager.ResolveDeleteProgramIDs(ctx, true, nil)
	require.NoError(t, err)
	require.Equal(t, []kernel.ProgramID{f.ownerID, f.dependentID}, ids)

	var results []manager.DeleteProgramResult
	err = lock.Run(ctx, f.fixture.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
		results = f.fixture.Manager.DeletePrograms(ctx, writeLock, ids, manager.DeleteProgramsOpts{All: true})
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		assert.NoError(t, result.Err)
	}

	assertProgramRecordDeleted(t, ctx, f.fixture, f.ownerID)
	assertProgramRecordDeleted(t, ctx, f.fixture, f.dependentID)
}

func TestDeleteProgramsOrdersExplicitDependentsWithoutRecursive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newSharedMapDeleteFixture(t, ctx)

	var results []manager.DeleteProgramResult
	err := lock.Run(ctx, f.fixture.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
		results = f.fixture.Manager.DeletePrograms(ctx, writeLock, []kernel.ProgramID{f.ownerID, f.dependentID}, manager.DeleteProgramsOpts{})
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		assert.NoError(t, result.Err)
	}

	assertProgramRecordDeleted(t, ctx, f.fixture, f.ownerID)
	assertProgramRecordDeleted(t, ctx, f.fixture, f.dependentID)
}

func TestDeleteLinksRecursiveTreatsBatchDependentsAsDeleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ids  func(ownerLink, dependentLink bpfman.LinkID) []bpfman.LinkID
	}{
		{
			name: "owner first",
			ids: func(ownerLink, dependentLink bpfman.LinkID) []bpfman.LinkID {
				return []bpfman.LinkID{ownerLink, dependentLink}
			},
		},
		{
			name: "dependent first",
			ids: func(ownerLink, dependentLink bpfman.LinkID) []bpfman.LinkID {
				return []bpfman.LinkID{dependentLink, ownerLink}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			f := newSharedMapDeleteFixture(t, ctx)
			ownerSpec, err := bpfman.NewTracepointAttachSpecFromString(f.ownerID, "syscalls/sys_enter_read")
			require.NoError(t, err)
			ownerLink, err := f.fixture.Attach(ctx, ownerSpec)
			require.NoError(t, err)
			dependentSpec, err := bpfman.NewTracepointAttachSpecFromString(f.dependentID, "syscalls/sys_enter_write")
			require.NoError(t, err)
			dependentLink, err := f.fixture.Attach(ctx, dependentSpec)
			require.NoError(t, err)

			var results []manager.DeleteLinkResult
			err = lock.Run(ctx, f.fixture.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
				results = f.fixture.Manager.DeleteLinks(ctx, writeLock, tt.ids(ownerLink.Record.ID, dependentLink.Record.ID), manager.DeleteLinksOpts{Recursive: true})
				return nil
			})
			require.NoError(t, err)
			require.Len(t, results, 2)
			assert.NoError(t, results[0].Err)
			assert.NoError(t, results[1].Err)

			assertProgramRecordDeleted(t, ctx, f.fixture, f.ownerID)
			assertProgramRecordDeleted(t, ctx, f.fixture, f.dependentID)
		})
	}
}

type sharedMapDeleteFixture struct {
	fixture     *testFixture
	ownerID     kernel.ProgramID
	dependentID kernel.ProgramID
}

func newSharedMapDeleteFixture(t *testing.T, ctx context.Context) sharedMapDeleteFixture {
	t.Helper()

	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)

	obj := f.BytecodeFile("shared-delete.o")
	validator.SetPrograms(obj, []fakeProgramInfo{
		{Name: "owner", SectionName: "tracepoint", Type: bpfman.ProgramTypeTracepoint},
		{Name: "dependent", SectionName: "tracepoint", Type: bpfman.ProgramTypeTracepoint},
	})

	owner, err := f.LoadDirect(ctx, manager.LoadSource{FilePath: obj}, []manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeTracepoint}}, manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	ownerID := owner[0].Record.ProgramID
	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: obj},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeTracepoint,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, dependent, 1)
	require.NotNil(t, dependent[0].Record.Handles.MapOwnerID)
	require.Equal(t, ownerID, *dependent[0].Record.Handles.MapOwnerID)

	return sharedMapDeleteFixture{
		fixture:     f,
		ownerID:     ownerID,
		dependentID: dependent[0].Record.ProgramID,
	}
}

func assertProgramRecordDeleted(t *testing.T, ctx context.Context, f *testFixture, id kernel.ProgramID) {
	t.Helper()
	_, err := f.Store.Get(ctx, id)
	assert.ErrorIs(t, err, platform.ErrRecordNotFound)
}
