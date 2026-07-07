package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bpfman/bpfman/kernel"
)

const (
	bytecodeName = "bytecode.o"
	provName     = "provenance.json"
	dirMode      = os.FileMode(0755)
	fileMode     = os.FileMode(0644)

	programsDir = "programs"
	stagingDir  = ".staging"
)

// Provenance records how a bytecode file was obtained. Written
// alongside bytecode.o as a diagnostic trace; never read on
// operational code paths.
type Provenance struct {
	// Version is the schema version of this provenance record.
	Version int `json:"version"`

	// ProgramID is the kernel program ID the bytecode was loaded as.
	ProgramID kernel.ProgramID `json:"program_id"`

	// ProgramName is the BPF program name within the bytecode.
	ProgramName string `json:"program_name"`

	// Source is the origin descriptor: a file path or an image
	// reference, depending on SourceKind.
	Source string `json:"source"`

	// SourceKind classifies Source as "file", "image", or "unknown".
	SourceKind string `json:"source_kind"`

	// LoadedAt is when the bytecode was obtained, in RFC 3339 UTC.
	LoadedAt time.Time `json:"loaded_at"`
}

// Bytecode provides regular-filesystem operations for bytecode
// persistence. Fields are unexported; obtain via Layout.Bytecode().
type Bytecode struct {
	layout Layout
}

// Valid reports whether the Bytecode was obtained from a valid Layout.
func (rt Bytecode) Valid() bool {
	return rt.layout.Valid()
}

// mustValid panics if rt was not obtained from Layout.Bytecode().
func (rt Bytecode) mustValid() {
	if !rt.Valid() {
		panic("fs: zero Bytecode used; obtain via Layout.Bytecode()")
	}
}

// Runtime is a capability token proving that the filesystem
// directories exist and bpffs is mounted. Obtain via runtime.New().
//
// Holding a Runtime guarantees:
//   - Base directory and subdirectories exist
//   - bpffs is mounted at the expected mount point
//
// This enables upfront validation before operations begin.
type Runtime struct {
	layout Layout
}

// NewRuntime creates a Runtime from a validated Layout.
// This is called by runtime.New() after directories and bpffs are ready.
// Direct callers must ensure the filesystem is properly initialised.
func NewRuntime(layout Layout) Runtime {
	return Runtime{layout: layout}
}

// Layout returns the underlying Layout.
func (r Runtime) Layout() Layout {
	return r.layout
}

// BPFFS returns the bpffs accessor for pin path conventions.
func (r Runtime) BPFFS() BPFFS {
	return r.layout.BPFFS()
}

// Bytecode returns the bytecode filesystem accessor for program persistence.
func (r Runtime) Bytecode() Bytecode {
	return r.layout.Bytecode()
}

// Valid reports whether the Runtime was properly constructed.
func (r Runtime) Valid() bool {
	return r.layout.Valid()
}

// programsPath returns <base>/programs.
func (rt Bytecode) programsPath() string {
	return filepath.Join(rt.layout.base, programsDir)
}

// stagingPath returns <base>/.staging.
func (rt Bytecode) stagingPath() string {
	return filepath.Join(rt.layout.base, stagingDir)
}

// ProgramDir returns <base>/programs/{id}.
func (rt Bytecode) ProgramDir(id kernel.ProgramID) string {
	return filepath.Join(rt.layout.base, programsDir, strconv.FormatUint(uint64(id), 10))
}

// PublishBytecode publishes srcPath to:
//
//	<base>/programs/{id}/bytecode.o
//
// via staging under <base>/.staging/.
//
// srcPath must refer to a readable regular file containing the ELF
// object. If it does not exist or is not readable, a PathError is
// returned.
//
// If <base>/programs/{id} already exists, PublishBytecode returns
// ErrFinalExists. The caller is expected to have run GC before
// loading, which removes orphan directories. An existing final
// directory after GC indicates an invariant violation.
//
// A provenance.json is written alongside the bytecode. Publish is
// atomic (rename on the same filesystem).
func (rt Bytecode) PublishBytecode(id kernel.ProgramID, srcPath string, prov Provenance) error {
	rt.mustValid()

	// Validate source file.
	if err := validateRegularFile(srcPath); err != nil {
		return err
	}

	finalDir := rt.ProgramDir(id)
	programs := rt.programsPath()
	staging := rt.stagingPath()

	// Check final directory does not exist.
	_, err := os.Stat(finalDir)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrFinalExists, finalDir)
	}
	if !os.IsNotExist(err) {
		return &PathError{Op: "publish", Path: finalDir, Err: err}
	}

	// Ensure parent directories exist.
	if err := os.MkdirAll(programs, dirMode); err != nil {
		return &PathError{Op: "publish", Path: programs, Err: err}
	}

	if err := os.MkdirAll(staging, dirMode); err != nil {
		return &PathError{Op: "publish", Path: staging, Err: err}
	}

	// Create temp dir under staging for atomic publish.
	tmpDir, err := os.MkdirTemp(staging, "pub-*")
	if err != nil {
		return &PathError{Op: "publish", Path: staging, Err: err}
	}

	// Clean up temp dir on any error after this point.
	cleanup := true
	defer func() {
		if cleanup {
			_ = safeRemoveAll(staging, tmpDir)
		}
	}()

	// Copy bytecode and write provenance into temp dir.
	bytecodeDst := filepath.Join(tmpDir, bytecodeName)
	provDst := filepath.Join(tmpDir, provName)

	if err := copyFile(srcPath, bytecodeDst, fileMode); err != nil {
		return &PathError{Op: "publish", Path: tmpDir, Err: err}
	}

	if err := writeJSON(provDst, fileMode, prov); err != nil {
		return &PathError{Op: "publish", Path: tmpDir, Err: err}
	}

	// Atomic rename from staging to final location.
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return &PathError{Op: "publish", Path: finalDir, Err: err}
	}

	cleanup = false
	return nil
}

// RemoveProgram removes <base>/programs/{id}/ and its contents.
// Returns nil if the directory does not exist. Uses safeRemoveAll to
// verify the target is under the programs directory.
func (rt Bytecode) RemoveProgram(id kernel.ProgramID) error {
	rt.mustValid()
	return safeRemoveAll(rt.programsPath(), rt.ProgramDir(id))
}

// RemoveProgramDir removes a program directory by path. The path must
// be a direct child of <base>/programs/. Returns nil if the directory
// does not exist. This handles both numeric and non-numeric directory
// names (e.g., orphaned directories with unexpected names).
func (rt Bytecode) RemoveProgramDir(path string) error {
	rt.mustValid()
	return safeRemoveAll(rt.programsPath(), path)
}

// ProgramExists reports whether <base>/programs/{id}/ exists.
func (rt Bytecode) ProgramExists(id kernel.ProgramID) bool {
	rt.mustValid()
	_, err := os.Stat(rt.ProgramDir(id))
	return err == nil
}

// ProgramBytecodePath returns the published bytecode path for DB
// ObjectPath storage.
func (rt Bytecode) ProgramBytecodePath(id kernel.ProgramID) string {
	rt.mustValid()
	return filepath.Join(rt.ProgramDir(id), bytecodeName)
}

// ProgramProvenancePath returns the published provenance path for a
// program.
func (rt Bytecode) ProgramProvenancePath(id kernel.ProgramID) string {
	rt.mustValid()
	return filepath.Join(rt.ProgramDir(id), provName)
}

// CleanStaging removes all entries under <base>/.staging/. Staging is
// a writer-only concern and is never visible to readers.
func (rt Bytecode) CleanStaging() error {
	rt.mustValid()
	staging := rt.stagingPath()

	entries, err := os.ReadDir(staging)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return &PathError{Op: "clean_staging", Path: staging, Err: err}
	}

	for _, entry := range entries {
		target := filepath.Join(staging, entry.Name())
		if err := safeRemoveAll(staging, target); err != nil {
			return err
		}
	}
	return nil
}

// RemoveStagingDir removes a staging directory by path. The path must
// be a direct child of <base>/.staging/. Returns nil if the directory
// does not exist.
func (rt Bytecode) RemoveStagingDir(path string) error {
	rt.mustValid()
	return safeRemoveAll(rt.stagingPath(), path)
}

// ProgramDirEntry represents a directory under <base>/programs/.
type ProgramDirEntry struct {
	// Path is the full path to the directory.
	Path string

	// ProgramID is the program ID parsed from the directory name; 0
	// when the name is not numeric (Numeric is false).
	ProgramID kernel.ProgramID

	// Numeric reports whether the directory name parsed as a valid
	// numeric program ID.
	Numeric bool
}

// ScanProgramDirs returns all directories under <base>/programs/.
// Returns nil (not error) if the programs directory does not exist.
func (rt Bytecode) ScanProgramDirs() ([]ProgramDirEntry, error) {
	rt.mustValid()
	programsPath := rt.programsPath()

	entries, err := os.ReadDir(programsPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, &PathError{Op: "scan_programs", Path: programsPath, Err: err}
	}

	var result []ProgramDirEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		pde := ProgramDirEntry{
			Path: filepath.Join(programsPath, name),
		}
		if id, err := strconv.ParseUint(name, 10, 32); err == nil {
			pde.ProgramID = kernel.ProgramID(id)
			pde.Numeric = true
		}

		result = append(result, pde)
	}
	return result, nil
}

// ScanStagingDirs returns all entry paths under <base>/.staging/.
// Returns nil (not error) if the staging directory does not exist.
func (rt Bytecode) ScanStagingDirs() ([]string, error) {
	rt.mustValid()
	stagingPath := rt.stagingPath()

	entries, err := os.ReadDir(stagingPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, &PathError{Op: "scan_staging", Path: stagingPath, Err: err}
	}

	var result []string
	for _, entry := range entries {
		result = append(result, filepath.Join(stagingPath, entry.Name()))
	}
	return result, nil
}
