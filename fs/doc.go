// Package fs models bpfman's runtime filesystem hierarchy and
// provides safe path construction for both bpffs and regular
// filesystem operations.
//
// # Overview
//
// bpfman maintains state across two filesystem hierarchies:
//
//   - A regular filesystem tree (typically under /run/bpfman) for
//     runtime directories, the SQLite database, Unix sockets, and
//     persisted bytecode files
//   - A bpffs mount (typically /run/bpfman/fs) for BPF program pins,
//     map pins, link pins, and dispatcher directories
//
// The package has two layers. Path computation ([Layout], [BPFFS]
// path methods, [ImageCache] key derivation) is pure and I/O-free.
// Operations that touch the filesystem -- [BPFFS] removal methods,
// [Bytecode] publish/remove, and [Scanner] directory reads -- perform
// real I/O but enforce path-safety invariants before doing so.
// Directory creation and bpffs mounting are handled by the
// fs/runtime subpackage.
//
// # Capability Tokens
//
// The package uses capability tokens to enforce construction-time
// validation:
//
//   - [Layout]: validated filesystem layout, constructed via [New].
//     All path methods panic on zero-value receivers to catch
//     programmer errors.
//   - [Runtime]: proves that directories exist and bpffs is
//     mounted. Obtained from fs/runtime.New() and required by
//     manager.New().
//   - [EnsuredImageCache]: proves that the OCI image cache directory
//     exists. Obtained from [EnsureCache].
//
// # Filesystem Domains
//
// [Layout] provides access to two domain types:
//
//   - [BPFFS]: bpffs pin path conventions for programs, maps, links,
//     and dispatchers. Includes path-validated removal methods that
//     refuse to delete files outside the bpffs mount point. Also
//     provides the [Scanner] for reading filesystem state.
//   - [Bytecode]: regular-filesystem operations for bytecode
//     persistence. Uses atomic staging (write to .staging/, rename to
//     programs/{id}/) with provenance metadata.
//
// # Image Cache
//
// [ImageCache] manages the OCI image cache (typically
// /var/cache/bpfman), which is separate from [Layout] because they
// have different lifecycles: Layout points at tmpfs cleared on
// reboot, while the image cache is persistent. The cache uses
// SHA256-based keys and supports atomic pull-to-cache operations.
//
// # Scanner
//
// [Scanner] provides read-only streaming iterators over the bpffs
// layout, yielding typed records for program pins, link directories,
// map directories, and dispatcher directories. It is used by the
// inspect package to build correlated state views.
//
// # Path Safety
//
// All removal methods validate that the target path is under the
// expected parent directory using filepath.Rel, preventing accidental
// deletion of unrelated files. Type-specific removal methods
// ([BPFFS.RemoveProgPin], [BPFFS.RemoveDispatcherRevDir], etc.)
// additionally validate naming conventions (numeric suffixes,
// dispatcher_ prefixes) before proceeding.
package fs
