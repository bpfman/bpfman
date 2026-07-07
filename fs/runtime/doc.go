// Package runtime provides I/O operations for bpfman's runtime environment.
//
// This package handles the one-time setup required before constructing a
// manager: creating runtime directories and mounting bpffs. It is separate
// from the fs package to maintain the distinction between pure path
// computation (fs) and actual I/O operations (fs/runtime).
//
// The New function returns a Runtime capability token that proves
// the filesystem is ready. This token is required by manager.New(), enforcing
// that setup is complete before any operations.
//
// Usage:
//
//	layout, err := fs.New("/run/bpfman")
//	if err != nil {
//	    return err
//	}
//	rt, err := runtime.New(layout, runtime.RealMounter{}, logger)
//	if err != nil {
//	    return err
//	}
//	mgr, err := manager.New(rt, store, kernel, validator, logger)
//
// For tests, use NoOpMounter to skip actual bpffs mounting:
//
//	rt, _ := runtime.New(layout, runtime.NoOpMounter{}, logger)
package runtime
