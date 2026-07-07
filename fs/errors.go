package fs

import (
	"errors"
	"fmt"
)

// ErrInvalidLayout names the invalid zero-value condition of a
// Layout, Runtime, or BPFFS. Operations on a zero-value receiver
// panic via the internal validity check rather than returning an
// error, so no path currently returns this sentinel.
var ErrInvalidLayout = errors.New("fs: invalid layout (zero value)")

// ErrFinalExists is returned by PublishBytecode when the final
// directory already exists. This is an invariant violation: GC
// should have removed orphan directories before the load path
// executes.
var ErrFinalExists = errors.New("fs: final directory already exists")

// ErrOutsideLayout is returned when a path safety check fails.
type ErrOutsideLayout struct {
	// Parent is the directory the target was required to stay within.
	Parent string

	// Target is the path that escaped Parent.
	Target string
}

// Error reports the target path and the parent directory it escaped.
func (e ErrOutsideLayout) Error() string {
	return fmt.Sprintf("fs: target %q is outside parent %q", e.Target, e.Parent)
}

// PathError wraps a filesystem error with the operation and path.
type PathError struct {
	// Op is the short name of the operation that failed (e.g.
	// "scan_programs", "clean_staging").
	Op string

	// Path is the filesystem path the operation was acting on.
	Path string

	// Err is the underlying filesystem error, returned by Unwrap.
	Err error
}

// Error reports the operation, path, and underlying error.
func (e *PathError) Error() string {
	return fmt.Sprintf("fs: %s %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap returns the wrapped filesystem error.
func (e *PathError) Unwrap() error {
	return e.Err
}
