package ebpf

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/kernel"
)

// applyGlobalData sets the user-supplied global variables on the
// collection spec before load. A key that names no variable in the
// object fails, matching Rust bpfman, which calls aya's set_global
// with must_exist=true so an absent symbol raises
// ParseError::SymbolNotFound rather than loading silently with the
// compile-time default -- a typo'd --global-data name is an error,
// not a no-op. The size check (a value whose length differs from
// the variable's) is enforced by VariableSpec.Set, mirroring aya's
// ParseError::InvalidGlobalData.
func applyGlobalData(collSpec *ebpf.CollectionSpec, globalData map[string][]byte) error {
	for name, data := range globalData {
		v, ok := collSpec.Variables[name]
		if !ok {
			return fmt.Errorf("global variable %q not found in program; available: %v", name, slices.Sorted(maps.Keys(collSpec.Variables)))
		}
		if err := v.Set(data); err != nil {
			return fmt.Errorf("set variable %q: %w", name, err)
		}
	}
	return nil
}

// HasPinByName reports whether the bytecode at spec.ObjectPath()
// declares any LIBBPF_PIN_BY_NAME maps. The manager uses this to
// decide whether the load needs the cross-process writer lock.
func (k *kernelAdapter) HasPinByName(spec bpfman.LoadSpec) (bool, error) {
	collSpec, err := ebpf.LoadCollectionSpec(spec.ObjectPath())
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", spec.ObjectPath(), err)
	}
	for name, mapSpec := range collSpec.Maps {
		if mapSpec.Pinning == ebpf.PinByName && !kernel.IsInternalMapName(name) {
			return true, nil
		}
	}
	return false, nil
}

// Load loads a BPF program into the kernel and pins it using program
// ID-based paths.
//
// Pin paths follow the upstream bpfman convention, computed via bpffs methods:
//   - Program: bpffs.ProgPinPath(program_id)
//   - Maps: bpffs.MapPinDir(program_id)/<map_name>
//
// On failure, all successfully pinned objects are cleaned up.
//
// Map sharing: If spec.MapOwnerID() is non-zero, this program will share maps
// with the owner program instead of creating its own. The owner's maps directory
// must exist and contain the required pinned maps. This is used when loading
// multiple programs from the same image (e.g., via the bpfman-operator) where
// all programs should share the same map instances.
func (k *kernelAdapter) Load(ctx context.Context, spec bpfman.LoadSpec, bpffs fs.BPFFS) (bpfman.LoadOutput, error) {
	// Load the collection from the object file
	collSpec, err := ebpf.LoadCollectionSpec(spec.ObjectPath())
	if err != nil {
		return bpfman.LoadOutput{}, fmt.Errorf("failed to load collection spec: %w", err)
	}

	// Record which maps declare PinByName before clearing the flag.
	// These maps will be shared across programs via a shared pin
	// directory, matching aya's LIBBPF_PIN_BY_NAME behaviour.
	pinByNameMaps := make(map[string]bool)
	for name, mapSpec := range collSpec.Maps {
		if mapSpec.Pinning == ebpf.PinByName && !kernel.IsInternalMapName(name) {
			pinByNameMaps[name] = true
		}
	}

	// Set global data if provided.
	if err := applyGlobalData(collSpec, spec.GlobalData()); err != nil {
		return bpfman.LoadOutput{}, err
	}

	// Clear map pinning flags - we'll pin manually after getting the kernel ID.
	// Some BPF programs have maps annotated with PIN_BY_NAME which requires
	// a pin path at load time, but we need the kernel ID first.
	for _, mapSpec := range collSpec.Maps {
		mapSpec.Pinning = ebpf.PinNone
	}

	// Find the requested program and get its license (needed before loading)
	progSpec, ok := collSpec.Programs[spec.ProgramName()]
	if !ok {
		available := slices.Sorted(maps.Keys(collSpec.Programs))
		return bpfman.LoadOutput{}, fmt.Errorf("program %q not found in collection spec; available programs: %v", spec.ProgramName(), available)
	}
	license := progSpec.License

	// Determine program type: prefer user-specified type, fall back to ELF inference.
	// The user's CLI specification (e.g., --programs kretprobe:func) takes precedence
	// because a kprobe program CAN be attached as either entry or return probe.
	programType := spec.ProgramType()
	secInferredType := inferProgramType(progSpec.SectionName)
	if !programType.Valid() {
		// Fall back to inferring from ELF section name
		programType = secInferredType
	}

	// The ELF section encodes the program's kind. Honour an explicit
	// declared type only when it agrees with the section, up to the
	// equivalence classes where one kernel program type backs several
	// bpfman types and the finer distinction is settled after load
	// (see sectionFamily). A genuine mismatch -- xdp from a kprobe SEC,
	// fentry from a fexit SEC, kprobe from an xdp SEC -- is rejected
	// here, before the kernel load. Otherwise a trivial program may
	// pass the wrong verifier and be silently mislabelled (any metadata
	// bpfman records would then be a lie, and the attach layer trusts
	// that recorded type), or a real type clash surfaces later as a
	// cryptic verifier error instead of this clean one.
	if secInferredType.Valid() && !declaredTypeMatchesSection(programType, secInferredType) {
		return bpfman.LoadOutput{}, fmt.Errorf("program type mismatch: caller specified %s but ELF section %q implies %s; "+"recompile the .bpf.o with the matching SEC or pass the matching ProgramType", programType, progSpec.SectionName, secInferredType)
	}

	// For fentry/fexit/lsm and other tracing programs that
	// require an attach function name, propagate the spec's
	// AttachFunc into ProgramSpec.AttachTo so the load-time
	// attach target overrides whatever the ELF SEC name said.
	// Without this, a program compiled with
	// SEC("fexit/some_placeholder") loads bound to that
	// placeholder regardless of the AttachFunc the caller passes
	// at attach time -- the kernel ties tracing programs to their
	// target at LOAD, not at link-create. cilium/ebpf resolves
	// AttachTo through vmlinux + loaded module BTF, so this also
	// makes fentry/fexit work against kernel-module functions
	// (e.g. the bpfman_e2e_targets slot pool used by the
	// hermetic e2e tests).
	if programType.RequiresAttachFunc() && spec.AttachFunc() != "" {
		progSpec.AttachTo = spec.AttachFunc()
	}

	// For XDP/TC programs: load as BPF_PROG_TYPE_EXT targeting a test
	// dispatcher. This matches Rust bpfman's approach where extension
	// programs are loaded once and reused from their pin on every
	// dispatcher rebuild, rather than re-reading the ELF file.
	if programType == bpfman.ProgramTypeXDP || programType == bpfman.ProgramTypeTC {
		var testProg *ebpf.Program
		if programType == bpfman.ProgramTypeXDP {
			testProg, err = k.testDisp.getXDP()
		} else {
			testProg, err = k.testDisp.getTC()
		}
		if err != nil {
			return bpfman.LoadOutput{}, fmt.Errorf("get test dispatcher for %s: %w", programType, err)
		}

		progSpec.Type = ebpf.Extension
		progSpec.AttachTarget = testProg
		progSpec.AttachTo = "prog0"
	}

	// Check if we should share maps with another program (map_owner_id).
	// When set, we load the owner's pinned maps and pass them as replacements
	// so this program uses the same map instances.
	var mapReplacements map[string]*ebpf.Map
	var ownerMapsDir bpfman.MapDir
	mapOwnerID := spec.MapOwnerID()

	if mapOwnerID != 0 {
		ownerMapsDir = bpffs.MapPinDir(mapOwnerID)
		mapReplacements = make(map[string]*ebpf.Map)

		k.logger.Debug("loading shared maps from owner program", "map_owner_id", mapOwnerID, "owner_maps_dir", ownerMapsDir)

		// Load pinned maps from owner's directory.
		// We iterate over collSpec.Maps to get the exact ELF map names.
		for name := range collSpec.Maps {
			// Skip internal maps (same filtering as pinning below)
			if kernel.IsInternalMapName(name) {
				continue
			}
			mapPath := bpffs.MapPinPath(mapOwnerID, name)
			m, err := ebpf.LoadPinnedMap(mapPath.String(), nil)
			if err != nil {
				// Clean up any maps we've already loaded
				for _, loaded := range mapReplacements {
					loaded.Close()
				}
				return bpfman.LoadOutput{}, fmt.Errorf("load shared map %q from owner %d: %w", name, mapOwnerID, err)
			}

			mapReplacements[name] = m
			k.logger.Debug("loaded shared map from owner", "name", name, "path", mapPath)
		}
	}

	// For PinByName maps without an explicit owner, ensure the
	// shared pin exists and load it into mapReplacements before
	// NewCollection. Two loaders racing here would both miss the
	// pre-check, both NewMap, and one fail m.Pin with EEXIST. To
	// avoid that, take the cross-process writer flock for just
	// this share-or-create step (two syscalls per map; no
	// BPF_PROG_LOAD inside) and run NewCollection lockless below
	// with the shared map already pinned and available as a
	// replacement.
	//
	// Crash recovery is handled here too: an earlier process that
	// pinned successfully then died leaves a pin pointing at a
	// live (orphaned) kernel map; we find it in the LoadPinnedMap
	// branch and share. The EEXIST fallback is defensive against
	// an external pinner (raw libbpf, bpftool) racing us inside
	// the flock; it should not fire from another bpfman.
	if len(pinByNameMaps) > 0 && mapOwnerID == 0 {
		if mapReplacements == nil {
			mapReplacements = make(map[string]*ebpf.Map)
		}
		if err := bpffs.EnsureSharedMapPinDir(); err != nil {
			return bpfman.LoadOutput{}, fmt.Errorf("failed to create shared map pin directory: %w", err)
		}
		shareErr := func() error {
			for name := range pinByNameMaps {
				sharedPath := bpffs.SharedMapPin(name)
				if m, lerr := ebpf.LoadPinnedMap(sharedPath.String(), nil); lerr == nil {
					mapReplacements[name] = m
					k.logger.Debug("shared PinByName map: using existing pin", "name", name, "path", sharedPath)
					continue
				}
				mapSpec, ok := collSpec.Maps[name]
				if !ok {
					return fmt.Errorf("pinByName map %q missing from collection spec", name)
				}
				specCopy := mapSpec.Copy()
				specCopy.Pinning = ebpf.PinNone
				m, nerr := ebpf.NewMap(specCopy)
				if nerr != nil {
					return fmt.Errorf("create shared map %q: %w", name, nerr)
				}
				if perr := m.Pin(sharedPath.String()); perr != nil {
					m.Close()
					if !errors.Is(perr, unix.EEXIST) {
						return fmt.Errorf("pin shared map %q: %w", name, perr)
					}
					// EEXIST: another writer pinned the same
					// name between our LoadPinnedMap pre-check
					// and this Pin. Load and share.
					fallback, lerr := ebpf.LoadPinnedMap(sharedPath.String(), nil)
					if lerr != nil {
						return fmt.Errorf("pin shared map %q: EEXIST but cannot load existing pin: %w", name, lerr)
					}
					mapReplacements[name] = fallback
					k.logger.Debug("shared PinByName map: external pinner won; using existing", "name", name, "path", sharedPath)
					continue
				}
				mapReplacements[name] = m
				k.logger.Debug("shared PinByName map: created and pinned", "name", name, "path", sharedPath)
			}
			return nil
		}()
		if shareErr != nil {
			for _, m := range mapReplacements {
				m.Close()
			}
			return bpfman.LoadOutput{}, shareErr
		}
	}

	// Load only the requested program. cilium/ebpf's NewCollection
	// loads and verifies every program in the spec, so an unrelated
	// broken program elsewhere in the object (or an fentry SEC naming a
	// function this kernel lacks) would fail this load even though the
	// caller never asked for it. aya/Rust verify only the requested
	// program; match that by dropping the others first. Maps are left
	// intact -- the requested program may share them, and Rust creates
	// the object's maps regardless of which programs load.
	wanted := spec.ProgramName()
	maps.DeleteFunc(collSpec.Programs, func(n string, _ *ebpf.ProgramSpec) bool {
		return n != wanted
	})

	// Load collection - use map replacements if sharing with owner
	var coll *ebpf.Collection
	if len(mapReplacements) > 0 {
		coll, err = ebpf.NewCollectionWithOptions(collSpec, ebpf.CollectionOptions{
			MapReplacements: mapReplacements,
		})
	} else {
		coll, err = ebpf.NewCollection(collSpec)
	}
	if err != nil {
		// Clean up map replacements on error
		for _, m := range mapReplacements {
			m.Close()
		}
		return bpfman.LoadOutput{}, fmt.Errorf("failed to load collection: %w", err)
	}
	defer coll.Close()

	prog, ok := coll.Programs[spec.ProgramName()]
	if !ok {
		return bpfman.LoadOutput{}, fmt.Errorf("program %q not found in collection", spec.ProgramName())
	}

	// Get program info to obtain kernel ID
	info, err := prog.Info()
	if err != nil {
		return bpfman.LoadOutput{}, fmt.Errorf("failed to get program info: %w", err)
	}

	progID, ok := info.ID()
	if !ok {
		return bpfman.LoadOutput{}, fmt.Errorf("failed to get program ID from kernel")
	}

	programID := kernel.ProgramID(progID)

	// Track pinned paths for rollback on failure.
	// Use BPFFS safe removal to ensure we only remove paths under the bpffs mount.
	var pinnedPaths []string
	cleanup := func() {
		for i := len(pinnedPaths) - 1; i >= 0; i-- {
			if err := bpffs.SafeRemove(pinnedPaths[i]); err != nil {
				k.logger.Warn("failed to remove pin during cleanup", "path", pinnedPaths[i], "error", err)
			}
		}
	}

	// Pin program using bpffs convention
	progPinPath := bpffs.ProgPinPath(programID)
	if err := prog.Pin(progPinPath.String()); err != nil {
		return bpfman.LoadOutput{}, fmt.Errorf("failed to pin program: %w", err)
	}
	pinnedPaths = append(pinnedPaths, progPinPath.String())

	// Determine the maps directory to use:
	// - If sharing maps (map_owner_id set): use owner's mapsDir, don't create/pin maps
	// - Otherwise: create our own mapsDir and pin maps
	var mapsDir bpfman.MapDir
	if mapOwnerID != 0 {
		// Use owner's maps directory - maps are already pinned there
		mapsDir = ownerMapsDir
		k.logger.Debug("using shared maps from owner", "program_id", programID, "map_owner_id", mapOwnerID, "maps_dir", mapsDir)
	} else {
		// Create our own maps directory using bpffs convention
		mapsDir = bpffs.MapPinDir(programID)
		if err := bpffs.EnsureMapsDir(programID); err != nil {
			cleanup()
			return bpfman.LoadOutput{}, fmt.Errorf("failed to create maps directory: %w", err)
		}

		// Pin all maps to the per-program directory. Maps that
		// are already pinned (at the shared location) need to be
		// cloned first, since Clone() produces an unpinned
		// duplicate that can be pinned to a second path.
		for name, m := range coll.Maps {
			if kernel.IsInternalMapName(name) {
				continue
			}
			mapPinPath := bpffs.MapPinPath(programID, name)
			if m.IsPinned() {
				clone, cloneErr := m.Clone()
				if cloneErr != nil {
					cleanup()
					if rmErr := bpffs.SafeRemoveAll(mapsDir.String()); rmErr != nil {
						k.logger.Warn("failed to remove maps directory during cleanup", "path", mapsDir, "error", rmErr)
					}
					return bpfman.LoadOutput{}, fmt.Errorf("failed to clone map %q for per-program pin: %w", name, cloneErr)
				}
				if err := clone.Pin(mapPinPath.String()); err != nil {
					clone.Close()
					cleanup()
					if rmErr := bpffs.SafeRemoveAll(mapsDir.String()); rmErr != nil {
						k.logger.Warn("failed to remove maps directory during cleanup", "path", mapsDir, "error", rmErr)
					}
					return bpfman.LoadOutput{}, fmt.Errorf("failed to pin map %q: %w", name, err)
				}
				clone.Close()
			} else {
				if err := m.Pin(mapPinPath.String()); err != nil {
					cleanup()
					if rmErr := bpffs.SafeRemoveAll(mapsDir.String()); rmErr != nil {
						k.logger.Warn("failed to remove maps directory during cleanup", "path", mapsDir, "error", rmErr)
					}
					return bpfman.LoadOutput{}, fmt.Errorf("failed to pin map %q: %w", name, err)
				}
			}
			pinnedPaths = append(pinnedPaths, mapPinPath.String())
		}
	}

	ebpfMapIDs, ok := info.MapIDs()
	if !ok {
		cleanup()
		if mapOwnerID == 0 {
			if rmErr := bpffs.SafeRemoveAll(mapsDir.String()); rmErr != nil {
				k.logger.Warn("failed to remove maps directory during cleanup", "path", mapsDir, "error", rmErr)
			}
		}
		return bpfman.LoadOutput{}, fmt.Errorf("failed to get map IDs from kernel")
	}

	_ = ebpfMapIDs // MapIDs now accessed via kernel.Program

	// Collect PinByName map names for reference counting.
	sharedMapNames := slices.Sorted(maps.Keys(pinByNameMaps))

	return bpfman.LoadOutput{
		PinPath:        progPinPath,
		MapsDir:        mapsDir,
		Program:        ToKernelProgram(info),
		License:        license,
		InferredType:   programType,
		SharedMapNames: sharedMapNames,
	}, nil
}

// Unload removes a BPF program from the kernel by unpinning.
// Handles both a single-directory layout (everything in one
// directory) and a split layout (separate program pin and maps
// directory).
func (k *kernelAdapter) Unload(ctx context.Context, pinPath string) error {
	info, err := os.Stat(pinPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat pin path: %w", err)
	}

	// If it's a file (program pin), just remove it
	if !info.IsDir() {
		if err := os.Remove(pinPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to unpin %s: %w", pinPath, err)
		}
		return nil
	}

	// It's a directory - remove contents then directory
	entries, err := os.ReadDir(pinPath)
	if err != nil {
		return fmt.Errorf("failed to read pin directory: %w", err)
	}

	for _, e := range entries {
		path := filepath.Join(pinPath, e.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to unpin %s: %w", path, err)
		}
	}

	if err := os.Remove(pinPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pin directory: %w", err)
	}

	return nil
}

// UnloadProgram removes a program and its maps using the upstream pin layout.
// progPinPath is the program pin (e.g., /run/bpfman/fs/prog_123)
// mapsDir is the maps directory (e.g., /run/bpfman/fs/maps/123)
func (k *kernelAdapter) UnloadProgram(ctx context.Context, progPinPath bpfman.ProgPinPath, mapsDir string) error {
	// Remove program pin
	if err := os.Remove(progPinPath.String()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to unpin program %s: %w", progPinPath, err)
	}

	// Remove maps directory and contents
	if mapsDir != "" {
		if err := k.Unload(ctx, mapsDir); err != nil {
			return fmt.Errorf("failed to unload maps: %w", err)
		}
	}

	return nil
}
