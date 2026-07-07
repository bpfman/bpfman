package manager_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

type failProgramSaveStore struct {
	platform.Store
	name string
	err  error
}

func (s *failProgramSaveStore) RunInTransaction(ctx context.Context, name string, fn func(platform.Store) error) error {
	return s.Store.RunInTransaction(ctx, name, func(tx platform.Store) error {
		return fn(&failProgramSaveTx{Store: tx, name: s.name, err: s.err})
	})
}

type failProgramSaveTx struct {
	platform.Store
	name string
	err  error
}

func (s *failProgramSaveTx) Save(ctx context.Context, programID kernel.ProgramID, metadata bpfman.ProgramRecord) error {
	if metadata.Meta.Name == s.name {
		return s.err
	}
	return s.Store.Save(ctx, programID, metadata)
}

type recordDeleteMapSetStore struct {
	platform.Store
	deleted []kernel.ProgramID
}

func (s *recordDeleteMapSetStore) DeleteMapSet(ctx context.Context, mapSetID kernel.ProgramID) error {
	s.deleted = append(s.deleted, mapSetID)
	return s.Store.DeleteMapSet(ctx, mapSetID)
}

type failListInTxStore struct {
	platform.Store
	err error
}

func (s *failListInTxStore) RunInTransaction(ctx context.Context, name string, fn func(platform.Store) error) error {
	return s.Store.RunInTransaction(ctx, name, func(tx platform.Store) error {
		return fn(&failListInTxTx{Store: tx, err: s.err})
	})
}

type failListInTxTx struct {
	platform.Store
	err error
}

func (s *failListInTxTx) List(ctx context.Context) (map[kernel.ProgramID]bpfman.ProgramRecord, error) {
	return nil, s.err
}

func newTestImageRef() *platform.ImageRef {
	return &platform.ImageRef{
		URL:        "test.io/image:latest",
		PullPolicy: bpfman.PullIfNotPresent,
	}
}

func TestLoad_MultiProgramDoesNotFabricateMapOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_b", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_c", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	programs, err := f.LoadDirect(ctx, manager.LoadSource{FilePath: objPath}, validator.specsFor(objPath), manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, programs, 3)

	for _, prog := range programs {
		assert.Nil(t, prog.Record.Handles.MapOwnerID, "%s load response should not fabricate a map owner", prog.Record.Meta.Name)

		got, err := f.Store.Get(ctx, prog.Record.ProgramID)
		require.NoError(t, err)
		assert.Nil(t, got.Handles.MapOwnerID, "%s persisted record should not fabricate a map owner", prog.Record.Meta.Name)
	}

	for _, prog := range programs {
		require.NoError(t, f.Unload(ctx, prog.Record.ProgramID), "unload in load order should succeed for %s", prog.Record.Meta.Name)
	}
	f.AssertCleanState()
}

func TestFindLoadedProgramByMetadataUsesFirstMatchForMultiProgramLoad(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_b", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	programs, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{
			{Name: "prog_a", Type: bpfman.ProgramTypeXDP},
			{Name: "prog_b", Type: bpfman.ProgramTypeXDP},
		},
		manager.LoadOpts{UserMetadata: map[string]string{
			"bpfman.io/ProgramName": "shared-app",
		}})
	require.NoError(t, err)
	require.Len(t, programs, 2)
	require.Nil(t, programs[0].Record.Handles.MapOwnerID)
	require.Nil(t, programs[1].Record.Handles.MapOwnerID)

	record, id, err := f.Manager.FindLoadedProgramByMetadata(ctx, "bpfman.io/ProgramName", "shared-app")
	require.NoError(t, err)
	assert.Equal(t, programs[0].Record.ProgramID, id)
	assert.Equal(t, programs[0].Record.ProgramID, record.ProgramID)
	assert.Equal(t, programs[0].Record.Handles.MapsDir, record.Handles.MapsDir)
}

func TestLoad_MapSetSurvivesCreatorUnload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	ownerID := owner[0].Record.ProgramID
	mapDir := owner[0].Record.Handles.MapsDir

	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, dependent, 1)
	dependentID := dependent[0].Record.ProgramID
	require.NotNil(t, dependent[0].Record.Handles.MapOwnerID)
	assert.Equal(t, ownerID, *dependent[0].Record.Handles.MapOwnerID)
	assert.Equal(t, mapDir, dependent[0].Record.Handles.MapsDir)

	require.NoError(t, f.Unload(ctx, ownerID))
	_, err = f.Store.Get(ctx, ownerID)
	assert.ErrorIs(t, err, platform.ErrRecordNotFound)

	gotDependent, err := f.Store.Get(ctx, dependentID)
	require.NoError(t, err)
	require.NotNil(t, gotDependent.Handles.MapOwnerID)
	assert.Equal(t, ownerID, *gotDependent.Handles.MapOwnerID)
	assert.Equal(t, mapDir, gotDependent.Handles.MapsDir)

	require.NoError(t, f.Unload(ctx, dependentID))
	f.AssertCleanState()
}

func TestLoad_MapUsedByIsDerivedByManager(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	ownerID := owner[0].Record.ProgramID
	assert.Equal(t, []kernel.ProgramID{ownerID}, owner[0].Status.MapUsedBy)

	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, dependent, 1)
	dependentID := dependent[0].Record.ProgramID
	wantUsers := []kernel.ProgramID{ownerID, dependentID}
	assert.Equal(t, wantUsers, dependent[0].Status.MapUsedBy)

	gotOwner, err := f.Manager.Get(ctx, ownerID)
	require.NoError(t, err)
	assert.Equal(t, wantUsers, gotOwner.Status.MapUsedBy)

	listed, err := f.Manager.ListPrograms(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, wantUsers, listed[0].Status.MapUsedBy)
	assert.Equal(t, wantUsers, listed[1].Status.MapUsedBy)
}

func TestLoad_MapUsedByDerivationFailureRollsBackLoad(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	deriveErr := errors.New("injected map-set membership read failure")
	f := newTestFixtureWithOptionsAndStore(t, nil, nil, func(base platform.Store) platform.Store {
		return &failListInTxStore{Store: base, err: deriveErr}
	})
	objPath := f.BytecodeFile("object.o")
	f.Validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	_, err := f.Manager.Load(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "prog", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.ErrorIs(t, err, deriveErr)

	// The map_used_by derivation runs inside the load commit, so its
	// failure rolls the whole transaction back: no program row
	// survives, and the kernel/fs work is compensated.
	progs, listErr := f.Store.List(ctx)
	require.NoError(t, listErr)
	assert.Empty(t, progs, "load must roll back when the in-tx map_used_by derivation fails")
	assert.Equal(t, 0, f.Kernel.ProgramCount(), "kernel program must be cleaned up after rollback")
}

func TestLoad_MapSetGCDeletesSetOnlyAfterLastUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	var recorder *recordDeleteMapSetStore
	f := newTestFixtureWithOptionsAndStore(t, validator, nil, func(store platform.Store) platform.Store {
		recorder = &recordDeleteMapSetStore{Store: store}
		return recorder
	})
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	ownerID := owner[0].Record.ProgramID

	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	dependentID := dependent[0].Record.ProgramID

	require.NoError(t, f.Unload(ctx, ownerID))
	assert.Empty(t, recorder.deleted, "owner-first unload must not delete a map set with users")

	require.NoError(t, f.Unload(ctx, dependentID))
	assert.Equal(t, []kernel.ProgramID{ownerID}, recorder.deleted, "last user unload must delete the surviving map set")
}

func TestLoad_MapSetSurvivesDependentUnloadWhileCreatorLives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	ownerID := owner[0].Record.ProgramID

	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, dependent, 1)
	dependentID := dependent[0].Record.ProgramID

	require.NoError(t, f.Unload(ctx, dependentID))
	gotOwner, err := f.Store.Get(ctx, ownerID)
	require.NoError(t, err)
	assert.Nil(t, gotOwner.Handles.MapOwnerID)
	assert.Equal(t, owner[0].Record.Handles.MapsDir, gotOwner.Handles.MapsDir)

	require.NoError(t, f.Unload(ctx, ownerID))
	f.AssertCleanState()
}

func TestLoad_ReusedProgramIDCollidingWithSurvivingMapSetFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "reused", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	ownerID := owner[0].Record.ProgramID
	mapDir := owner[0].Record.Handles.MapsDir

	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, dependent, 1)
	dependentID := dependent[0].Record.ProgramID

	require.NoError(t, f.Unload(ctx, ownerID))
	_, err = f.Store.Get(ctx, ownerID)
	require.ErrorIs(t, err, platform.ErrRecordNotFound)

	// Force the next fake kernel allocation to reuse the old owner id.
	// fakeKernel increments before returning, so store ownerID-1.
	f.Kernel.nextID.Store(uint32(ownerID - 1))

	_, err = f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "reused", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.Error(t, err)
	assert.ErrorIs(t, err, platform.ErrMapSetIDReused, "reused-id collision must surface a diagnosable error, not a bare constraint violation")
	assert.Contains(t, err.Error(), "reused kernel program id collided with a surviving map set")

	_, err = f.Store.Get(ctx, ownerID)
	assert.ErrorIs(t, err, platform.ErrRecordNotFound, "reused program must not be persisted")
	gotDependent, err := f.Store.Get(ctx, dependentID)
	require.NoError(t, err)
	require.NotNil(t, gotDependent.Handles.MapOwnerID)
	assert.Equal(t, ownerID, *gotDependent.Handles.MapOwnerID)
	assert.Equal(t, mapDir, gotDependent.Handles.MapsDir)
	assert.Equal(t, 1, f.Kernel.ProgramCount(), "failed reused load must be rolled back")
}

func TestLoad_MapOwnerIDMustNameExistingMapSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "candidate", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	_, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "candidate",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: 429496729,
		}},
		manager.LoadOpts{})
	require.Error(t, err)
	assert.ErrorIs(t, err, platform.ErrMapOwnerNotFound)
	assert.Contains(t, err.Error(), "map_owner_id 429496729 does not exist")
	assert.Equal(t, 0, f.Kernel.ProgramCount(), "invalid owner must fail before kernel load")

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	ownerID := owner[0].Record.ProgramID

	dependent, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.NoError(t, err)
	dependentID := dependent[0].Record.ProgramID

	_, err = f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "candidate",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: dependentID,
		}},
		manager.LoadOpts{})
	require.Error(t, err)
	assert.ErrorIs(t, err, platform.ErrMapOwnerNotFound)
	assert.Contains(t, err.Error(), fmt.Sprintf("map_owner_id %d does not exist", dependentID))
	assert.Equal(t, 2, f.Kernel.ProgramCount(), "dependent-as-owner must fail before kernel load")
}

func TestLoad_RollbackExplicitMapOwnerDoesNotRemoveOwnerMapSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	saveErr := errors.New("simulated dependent save failure")
	validator := newFakeValidator()
	f := newTestFixtureWithOptionsAndStore(t, validator, nil, func(store platform.Store) platform.Store {
		return &failProgramSaveStore{Store: store, name: "dependent", err: saveErr}
	})
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "owner", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "dependent", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	owner, err := f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{Name: "owner", Type: bpfman.ProgramTypeXDP}},
		manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	ownerID := owner[0].Record.ProgramID
	ownerMapDir := owner[0].Record.Handles.MapsDir.String()

	f.Kernel.FailOnUnload(ownerMapDir, errors.New("owner map set must not be removed by dependent rollback"))

	_, err = f.LoadDirect(ctx,
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{{
			Name:       "dependent",
			Type:       bpfman.ProgramTypeXDP,
			MapOwnerID: ownerID,
		}},
		manager.LoadOpts{})
	require.ErrorIs(t, err, saveErr)

	_, err = f.Store.Get(ctx, ownerID)
	require.NoError(t, err, "owner record must survive dependent rollback")
	assert.Equal(t, 0, f.Kernel.UnloadFailureCount(ownerMapDir), "dependent rollback must not attempt to remove the owner's map set")
	assert.Equal(t, 1, f.Kernel.ProgramCount(), "only the owner should remain loaded")
}

// TestLoad_ExplicitPrograms_PreservesOrder asserts the contract that
// Manager.Load returns programs in the same order as the input
// ProgramSpec slice, independent of the order they appear in the
// object file. CLI and SDK consumers depend on this for stable JSON
// output and for variable assignment in shell scripts
// (`let progs = [bpfman program load ...]`).
func TestLoad_ExplicitPrograms_PreservesOrder(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_b", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_c", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_d", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	// Request a non-alphabetical, non-source-order subset so the
	// assertion catches both "validator order leaked" and
	// "result was sorted".
	requested := []manager.ProgramSpec{
		{Name: "prog_c", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_a", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_d", Type: bpfman.ProgramTypeXDP},
	}

	programs, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, requested, manager.LoadOpts{})

	require.NoError(t, err)
	require.Len(t, programs, len(requested))
	for i, want := range requested {
		assert.Equal(t, want.Name, programs[i].Record.Meta.Name, "programs[%d].Record.Meta.Name should match requested[%d].Name", i, i)
	}
}

// Loading a whole object means naming every program in it: the
// explicit specs carry the caller's declared types, so a tcx program
// stays tcx (a section-derived guess could not distinguish it from
// tc) and a fentry program carries its attach function.
func TestLoad_WholeObjectExplicit(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "xdp_prog", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "tcx_prog", SectionName: "classifier/tcx_prog", Type: bpfman.ProgramTypeTCX},
		{Name: "fentry_prog", SectionName: "fentry/vfs_read", Type: bpfman.ProgramTypeFentry, AttachFunc: "vfs_read"},
	})

	programs, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, []manager.ProgramSpec{
		{Name: "xdp_prog", Type: bpfman.ProgramTypeXDP},
		{Name: "tcx_prog", Type: bpfman.ProgramTypeTCX},
		{Name: "fentry_prog", Type: bpfman.ProgramTypeFentry, AttachFunc: "vfs_read"},
	}, manager.LoadOpts{})

	require.NoError(t, err)
	require.Len(t, programs, 3)
	assert.Equal(t, bpfman.ProgramTypeXDP, programs[0].Record.Load.ProgramType())
	assert.Equal(t, bpfman.ProgramTypeTCX, programs[1].Record.Load.ProgramType())
	assert.Equal(t, bpfman.ProgramTypeFentry, programs[2].Record.Load.ProgramType())
	assert.Equal(t, "vfs_read", programs[2].Record.Load.AttachFunc())
	assert.Equal(t, 3, f.Kernel.ProgramCount())
}

func TestLoad_ExplicitPrograms_Valid(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_b", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	// Request only prog_b
	programs, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, []manager.ProgramSpec{
		{Name: "prog_b", Type: bpfman.ProgramTypeXDP},
	}, manager.LoadOpts{})

	require.NoError(t, err)
	assert.Len(t, programs, 1)
	assert.Equal(t, "prog_b", programs[0].Record.Meta.Name)
	assert.Equal(t, 1, f.Kernel.ProgramCount())
}

func TestLoad_ExplicitPrograms_InvalidName(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	// Request non-existent program
	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, []manager.ProgramSpec{
		{Name: "nonexistent", Type: bpfman.ProgramTypeXDP},
	}, manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, 0, f.Kernel.ProgramCount())
}

func TestLoad_Rollback_SecondProgramFails(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_b", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	// Make second program fail to load
	f.Kernel.FailOnProgram("prog_b", fmt.Errorf("injected load failure"))

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prog_b")

	// Verify rollback: prog_a should be unloaded from both kernel and database
	f.AssertCleanState()

	// Verify the operation sequence
	ops := f.Kernel.Operations()
	// Should see: load prog_a (ok), load prog_b (error), unload prog_a (cleanup)
	assert.GreaterOrEqual(t, len(ops), 3)

	var loadA, loadB, unloadA bool
	for _, op := range ops {
		if op.Op == "load" && op.Name == "prog_a" && op.Err == nil {
			loadA = true
		}
		if op.Op == "load" && op.Name == "prog_b" && op.Err != nil {
			loadB = true
		}
		if op.Op == "unload" && op.Name == "prog_a" {
			unloadA = true
		}
	}
	assert.True(t, loadA, "expected prog_a to be loaded")
	assert.True(t, loadB, "expected prog_b load to fail")
	assert.True(t, unloadA, "expected prog_a to be unloaded during rollback")
}

func TestLoad_Rollback_ThirdProgramFails(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_b", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "prog_c", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	// Make third program fail to load
	f.Kernel.FailOnProgram("prog_c", fmt.Errorf("injected load failure"))

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prog_c")

	// Verify rollback: prog_a and prog_b should be unloaded from both kernel and database
	f.AssertCleanState()
}

func TestLoad_PullError(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	puller.SetPullError(fmt.Errorf("network error"))
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pull image")
	assert.Equal(t, 0, f.Kernel.ProgramCount())
}

func TestLoad_Rollback_FentryFexitSecondFails(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "trace_vfs_read", SectionName: "fentry/vfs_read", Type: bpfman.ProgramTypeFentry, AttachFunc: "vfs_read"},
		{Name: "trace_vfs_write", SectionName: "fexit/vfs_write", Type: bpfman.ProgramTypeFexit, AttachFunc: "vfs_write"},
	})

	// Make second program (fexit) fail to load
	f.Kernel.FailOnProgram("trace_vfs_write", fmt.Errorf("injected fexit load failure"))

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace_vfs_write")

	// Verify rollback: fentry program should be unloaded from both kernel and database
	f.AssertCleanState()

	// Verify the operation sequence
	ops := f.Kernel.Operations()
	var loadFentry, loadFexit, unloadFentry bool
	for _, op := range ops {
		if op.Op == "load" && op.Name == "trace_vfs_read" && op.Err == nil {
			loadFentry = true
		}
		if op.Op == "load" && op.Name == "trace_vfs_write" && op.Err != nil {
			loadFexit = true
		}
		if op.Op == "unload" && op.Name == "trace_vfs_read" {
			unloadFentry = true
		}
	}
	assert.True(t, loadFentry, "expected fentry program to be loaded")
	assert.True(t, loadFexit, "expected fexit program load to fail")
	assert.True(t, unloadFentry, "expected fentry program to be unloaded during rollback")
}

func TestLoad_Rollback_FentryFexitFirstFails(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "trace_vfs_read", SectionName: "fentry/vfs_read", Type: bpfman.ProgramTypeFentry, AttachFunc: "vfs_read"},
		{Name: "trace_vfs_write", SectionName: "fexit/vfs_write", Type: bpfman.ProgramTypeFexit, AttachFunc: "vfs_write"},
	})

	// Make first program (fentry) fail to load - no cleanup needed
	f.Kernel.FailOnProgram("trace_vfs_read", fmt.Errorf("injected fentry load failure"))

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace_vfs_read")

	// Verify no programs remain in kernel or database
	f.AssertCleanState()

	// Verify second program was never attempted
	ops := f.Kernel.Operations()
	var loadFentry, loadFexit bool
	for _, op := range ops {
		if op.Op == "load" && op.Name == "trace_vfs_read" {
			loadFentry = true
		}
		if op.Op == "load" && op.Name == "trace_vfs_write" {
			loadFexit = true
		}
	}
	assert.True(t, loadFentry, "expected fentry program load to be attempted")
	assert.False(t, loadFexit, "expected fexit program load to NOT be attempted after first failure")
}

func TestLoad_Rollback_MixedTypesThirdFails(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "my_xdp", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
		{Name: "trace_vfs_read", SectionName: "fentry/vfs_read", Type: bpfman.ProgramTypeFentry, AttachFunc: "vfs_read"},
		{Name: "trace_vfs_write", SectionName: "fexit/vfs_write", Type: bpfman.ProgramTypeFexit, AttachFunc: "vfs_write"},
	})

	// Make third program (fexit) fail to load
	f.Kernel.FailOnProgram("trace_vfs_write", fmt.Errorf("injected fexit load failure"))

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace_vfs_write")

	// Verify rollback: both xdp and fentry should be unloaded from both kernel and database
	f.AssertCleanState()

	// Verify the operation sequence
	ops := f.Kernel.Operations()
	var loadXDP, loadFentry, loadFexit, unloadXDP, unloadFentry bool
	for _, op := range ops {
		if op.Op == "load" && op.Name == "my_xdp" && op.Err == nil {
			loadXDP = true
		}
		if op.Op == "load" && op.Name == "trace_vfs_read" && op.Err == nil {
			loadFentry = true
		}
		if op.Op == "load" && op.Name == "trace_vfs_write" && op.Err != nil {
			loadFexit = true
		}
		if op.Op == "unload" && op.Name == "my_xdp" {
			unloadXDP = true
		}
		if op.Op == "unload" && op.Name == "trace_vfs_read" {
			unloadFentry = true
		}
	}
	assert.True(t, loadXDP, "expected xdp program to be loaded")
	assert.True(t, loadFentry, "expected fentry program to be loaded")
	assert.True(t, loadFexit, "expected fexit program load to fail")
	assert.True(t, unloadXDP, "expected xdp program to be unloaded during rollback")
	assert.True(t, unloadFentry, "expected fentry program to be unloaded during rollback")
}

func TestLoad_ValidationError(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)
	objPath := f.BytecodeFile("object.o")
	puller.SetObjectPath(objPath)
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "prog_a", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})
	validator.SetValidateError(fmt.Errorf("custom validation error"))

	// Request explicit programs to trigger validation
	_, err := f.LoadDirect(context.Background(), manager.LoadSource{
		Image: newTestImageRef(),
	}, []manager.ProgramSpec{
		{Name: "prog_a", Type: bpfman.ProgramTypeXDP},
	}, manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom validation error")
	assert.Equal(t, 0, f.Kernel.ProgramCount())
}

func TestLoad_FileSource(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "test_prog", SectionName: "xdp", Type: bpfman.ProgramTypeXDP},
	})

	programs, err := f.LoadDirect(context.Background(), manager.LoadSource{
		FilePath: objPath,
	}, validator.specsFor(objPath), manager.LoadOpts{})

	require.NoError(t, err)
	assert.Len(t, programs, 1)
	assert.Equal(t, "test_prog", programs[0].Record.Meta.Name)
	assert.Equal(t, 1, f.Kernel.ProgramCount())
}

func TestLoad_MetadataActualTypeResolvedInManager(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	f := newTestFixtureWithValidator(t, validator)
	objPath := f.BytecodeFile("object.o")
	validator.SetPrograms(objPath, []fakeProgramInfo{
		{Name: "retprobe", SectionName: "kprobe", Type: bpfman.ProgramTypeKprobe},
	})

	programs, err := f.LoadDirect(context.Background(),
		manager.LoadSource{FilePath: objPath},
		[]manager.ProgramSpec{
			{Name: "retprobe", Type: bpfman.ProgramTypeKprobe},
		},
		manager.LoadOpts{
			UserMetadata: map[string]string{
				"bpfman.io/actual-type:retprobe": "kretprobe",
			},
		})

	require.NoError(t, err)
	require.Len(t, programs, 1)
	assert.Equal(t, bpfman.ProgramTypeKretprobe, programs[0].Record.Load.ProgramType())
}

// A load with no program specs is rejected outright: the caller must
// name every TYPE:NAME to load. Auto-discovery guessed types from
// section names, which cannot distinguish tc from tcx (both live in
// classifier sections) and cannot carry a fentry/fexit attach
// function, so a whole-object load silently mislabelled programs.
// Explicit wins, matching the Rust CLI's required --programs.
func TestLoad_EmptyProgramsRejected(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	objPath := f.BytecodeFile("object.o")

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{FilePath: objPath}, nil, manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no programs specified")
	assert.Equal(t, 0, f.Kernel.ProgramCount())
}

// The rejection happens before the source resolves: an image load
// with no programs fails without pulling the image it could not use.
func TestLoad_EmptyProgramsRejectedBeforePull(t *testing.T) {
	t.Parallel()

	validator := newFakeValidator()
	puller := newFakeImagePuller()
	f := newTestFixtureWithOptions(t, validator, puller)

	_, err := f.LoadDirect(context.Background(), manager.LoadSource{Image: newTestImageRef()}, nil, manager.LoadOpts{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no programs specified")
	assert.Empty(t, puller.Pulls(), "an empty program list must be rejected before any image pull")
}
