package manager_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/fs/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager"
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

// testFixture provides access to all components for verification.
//
// Keep the store real. Manager tests deliberately use the SQLite
// implementation, even when the kernel is fake, because the schema's
// foreign keys, ON DELETE CASCADE, and ON DELETE RESTRICT behaviour are
// part of the manager contract. A mock store would miss exactly the
// dependency and cascading-delete paths these tests need to exercise.
type testFixture struct {
	Manager       *manager.Manager
	Kernel        *fakeKernel
	Validator     *fakeValidator
	Store         platform.Store
	Layout        fs.Layout
	t             *testing.T
	bytecodeDir   string            // temp dir for dummy bytecode files
	bytecodeFiles map[string]string // name -> path cache
}

// newTestFixture creates a complete test fixture with accessible components.
func newTestFixture(t *testing.T) *testFixture {
	return newTestFixtureWithOptions(t, nil, nil)
}

// newTestFixtureWithValidator creates a test fixture with a custom validator.
func newTestFixtureWithValidator(t *testing.T, validator *fakeValidator) *testFixture {
	return newTestFixtureWithOptions(t, validator, nil)
}

// newTestFixtureWithOptions creates a test fixture with optional overrides.
// The store is intentionally not overridable; tests should exercise the
// real SQLite schema so cascade and restriction behaviour stays covered.
func newTestFixtureWithOptions(t *testing.T, validator *fakeValidator, puller platform.ImagePuller) *testFixture {
	return newTestFixtureWithOptionsAndStore(t, validator, puller, nil)
}

func newTestFixtureWithStore(t *testing.T, wrap func(platform.Store) platform.Store) *testFixture {
	return newTestFixtureWithOptionsAndStore(t, nil, nil, wrap)
}

func newTestFixtureWithOptionsAndStore(
	t *testing.T,
	validator *fakeValidator,
	puller platform.ImagePuller,
	wrap func(platform.Store) platform.Store,
) *testFixture {
	t.Helper()
	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	t.Cleanup(func() { store.Close() })
	managerStore := store
	if wrap != nil {
		managerStore = wrap(store)
	}
	layout, err := fs.New(filepath.Join(t.TempDir(), "bpfman"))
	require.NoError(t, err, "failed to create fs layout")
	kernel := newFakeKernel()
	if validator == nil {
		validator = newFakeValidator()
	}

	// Centralised ensure call in fixture
	ensuredRuntime, err := runtime.New(layout, runtime.NoOpMounter{}, testLogger())
	require.NoError(t, err, "failed to ensure runtime")

	mgr, err := manager.New(ensuredRuntime, puller, managerStore, kernel, validator, testLogger())
	require.NoError(t, err, "failed to create manager")
	bcDir := t.TempDir()
	return &testFixture{
		Manager:       mgr,
		Kernel:        kernel,
		Validator:     validator,
		Store:         managerStore,
		Layout:        layout,
		t:             t,
		bytecodeDir:   bcDir,
		bytecodeFiles: make(map[string]string),
	}
}

// BytecodeFile returns the path to a dummy bytecode file with the
// given name. The file is created on first request and reused for
// subsequent calls with the same name. Tests should use this instead
// of hard-coded paths like "/path/to/prog.o".
func (f *testFixture) BytecodeFile(name string) string {
	f.t.Helper()
	if p, ok := f.bytecodeFiles[name]; ok {
		return p
	}
	p := filepath.Join(f.bytecodeDir, name)
	dir := filepath.Dir(p)
	require.NoError(f.t, os.MkdirAll(dir, 0755))
	require.NoError(f.t, os.WriteFile(p, []byte("ELF dummy bytecode"), 0644))
	f.bytecodeFiles[name] = p
	return p
}

func (f *testFixture) TempNetnsPath() string {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), "netns")
	require.NoError(f.t, os.WriteFile(path, nil, 0644))
	return path
}

// AssertKernelEmpty verifies no programs remain in the kernel.
func (f *testFixture) AssertKernelEmpty() {
	f.t.Helper()
	assert.Equal(f.t, 0, f.Kernel.ProgramCount(), "expected no programs in kernel")
}

// AssertDatabaseEmpty verifies no programs remain in the database.
func (f *testFixture) AssertDatabaseEmpty() {
	f.t.Helper()
	programs, err := f.Store.List(context.Background())
	require.NoError(f.t, err, "failed to list programs from store")
	assert.Empty(f.t, programs, "expected no programs in database")
	mapSets, err := f.Store.CountMapSets(context.Background())
	require.NoError(f.t, err, "failed to count map sets from store")
	assert.Zero(f.t, mapSets, "expected no map sets in database")
}

// AssertCleanState verifies both kernel and database are empty.
func (f *testFixture) AssertCleanState() {
	f.t.Helper()
	f.AssertKernelEmpty()
	f.AssertDatabaseEmpty()
}

// AssertKernelOps verifies the sequence of kernel operations.
func (f *testFixture) AssertKernelOps(expected []string) {
	f.t.Helper()
	ops := f.Kernel.Operations()
	actual := make([]string, len(ops))
	for i, op := range ops {
		if op.Err != nil {
			actual[i] = fmt.Sprintf("%s:%s:error", op.Op, op.Name)
		} else {
			actual[i] = fmt.Sprintf("%s:%s:ok", op.Op, op.Name)
		}
	}
	assert.Equal(f.t, expected, actual, "kernel operations mismatch")
}

// Load is a convenience wrapper that loads a single program from a LoadSpec.
// It translates the LoadSpec into the LoadSource/ProgramSpec form expected
// by Manager.Load, ensures the fake validator knows about the program,
// acquires a real lock for compile-time safety, and returns the single
// loaded program.
func (f *testFixture) Load(ctx context.Context, spec bpfman.LoadSpec, opts manager.LoadOpts) (bpfman.Program, error) {
	f.t.Helper()
	source := manager.LoadSource{FilePath: spec.ObjectPath()}
	programs := []manager.ProgramSpec{{
		Name:       spec.ProgramName(),
		Type:       spec.ProgramType(),
		AttachFunc: spec.AttachFunc(),
	}}
	if gd := spec.GlobalData(); gd != nil {
		programs[0].GlobalData = gd
	}
	if id := spec.MapOwnerID(); id != 0 {
		programs[0].MapOwnerID = id
	}
	// Ensure the validator knows about the program so validation passes.
	f.Validator.AddPrograms(spec.ObjectPath(), fakeProgramInfo{
		Name:       spec.ProgramName(),
		Type:       spec.ProgramType(),
		AttachFunc: spec.AttachFunc(),
	})
	// Manager.Load decides internally whether this load needs the
	// writer lock. Explicit map-owner joins and PinByName loads take it.
	result, err := f.Manager.Load(ctx, source, programs, opts)
	if err != nil {
		return bpfman.Program{}, err
	}
	return result[0], nil
}

// Unload is a convenience wrapper that acquires the lock and calls
// Manager.Unload.
func (f *testFixture) Unload(ctx context.Context, programID kernel.ProgramID) error {
	f.t.Helper()
	return lock.Run(ctx, f.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
		return f.Manager.Unload(ctx, writeLock, programID)
	})
}

// Attach is a convenience wrapper that acquires the lock and calls
// Manager.Attach.
func (f *testFixture) Attach(ctx context.Context, spec bpfman.AttachSpec) (bpfman.Link, error) {
	f.t.Helper()
	var link bpfman.Link
	err := lock.Run(ctx, f.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
		var attachErr error
		link, attachErr = f.Manager.Attach(ctx, writeLock, spec)
		return attachErr
	})
	return link, err
}

// Detach is a convenience wrapper that acquires the lock and calls
// Manager.Detach.
func (f *testFixture) Detach(ctx context.Context, linkID bpfman.LinkID) error {
	f.t.Helper()
	return lock.Run(ctx, f.Layout.LockPath(), func(ctx context.Context, writeLock lock.WriterScope) error {
		return f.Manager.Detach(ctx, writeLock, linkID)
	})
}

// LoadDirect is a convenience wrapper that calls Manager.Load with
// raw LoadSource and ProgramSpec arguments. Use this for tests that
// bypass LoadSpec (e.g., auto-discovery tests).
func (f *testFixture) LoadDirect(ctx context.Context, source manager.LoadSource, programs []manager.ProgramSpec, opts manager.LoadOpts) ([]bpfman.Program, error) {
	f.t.Helper()
	return f.Manager.Load(ctx, source, programs, opts)
}
