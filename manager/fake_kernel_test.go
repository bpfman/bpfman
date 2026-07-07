// This file provides the manager test harness: a fake kernel adapter
// paired with the real SQLite store. The split is deliberate. The
// store is where cascades, snapshot replacement, and transaction
// semantics live, so faking it would only test our assumptions about
// SQL; the kernel side is faked so tests can force failures at exact
// operation boundaries (a snapshot persist failure, an occupied pin,
// a detach error, a missing namespace path) and assert kernel-side
// state directly (link counts, filter identity, dispatcher link
// targets) without scraping netlink or bpffs.
//
// Trust this suite accordingly. The fake verifies the kernel
// invariants it has been taught; it cannot discover invariants it
// does not model. A green run proves the manager's logic against the
// modelled behaviour, not that the model matches the kernel.
//
// Two rules keep that honest. Extend the fake only when a bug or a
// parity finding depends on a kernel invariant it lacks; grown
// speculatively it trends towards reimplementing the kernel. And
// when you teach it a new invariant, anchor the model to reality
// with an e2e script (see e2e/scripts) that proves the same
// invariant against the real kernel once; the fake-kernel test then
// guards the logic cheaply forever.
package manager_test

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

// fakeProgramInfo describes a program the fake validator knows about
// in a given object, standing in for what a real ELF scan would find.
type fakeProgramInfo struct {
	// Name is the program's function/symbol name.
	Name string
	// SectionName is the ELF section the program was defined in.
	SectionName string
	// Type is the attach-oriented program type.
	Type bpfman.ProgramType
	// AttachFunc is the fentry/fexit target function; empty otherwise.
	AttachFunc string
}

// fakeValidator implements platform.ProgramValidator for testing.
type fakeValidator struct {
	// Programs maps object path to the programs it contains
	programs map[string][]fakeProgramInfo
	// ValidateErr if set, ValidatePrograms returns this error
	validateErr error
}

func newFakeValidator() *fakeValidator {
	return &fakeValidator{
		programs: make(map[string][]fakeProgramInfo),
	}
}

// SetPrograms configures the programs to return for a given object path.
func (d *fakeValidator) SetPrograms(objectPath string, programs []fakeProgramInfo) {
	d.programs[objectPath] = programs
}

// AddPrograms appends programs to the list for the given object path.
func (d *fakeValidator) AddPrograms(objectPath string, programs ...fakeProgramInfo) {
	d.programs[objectPath] = append(d.programs[objectPath], programs...)
}

// specsFor returns explicit ProgramSpecs naming every program
// configured for objectPath, in configuration order. Tests that load
// a whole object use it to declare each program, mirroring the CLI's
// required --programs.
func (d *fakeValidator) specsFor(objectPath string) []manager.ProgramSpec {
	infos := d.programs[objectPath]
	specs := make([]manager.ProgramSpec, len(infos))
	for i, p := range infos {
		specs[i] = manager.ProgramSpec{Name: p.Name, Type: p.Type, AttachFunc: p.AttachFunc}
	}
	return specs
}

// SetValidateError configures ValidatePrograms to return the given error.
func (d *fakeValidator) SetValidateError(err error) {
	d.validateErr = err
}

func (d *fakeValidator) ValidatePrograms(objectPath string, programNames []string) error {
	if d.validateErr != nil {
		return d.validateErr
	}

	programs, ok := d.programs[objectPath]
	if !ok {
		return fmt.Errorf("object file not found: %s", objectPath)
	}
	// Build set of available program names
	available := make(map[string]bool)
	for _, p := range programs {
		available[p.Name] = true
	}
	// Check each requested program
	var missing []string
	for _, name := range programNames {
		if !available[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		availableList := make([]string, 0, len(available))
		for name := range available {
			availableList = append(availableList, name)
		}
		sort.Strings(availableList)
		return fmt.Errorf("program(s) not found: %v; available: %v", missing, availableList)
	}
	return nil
}

// Ensure fakeValidator implements the interface.
var _ platform.ProgramValidator = (*fakeValidator)(nil)

// fakeImagePuller implements platform.ImagePuller for testing.
type fakeImagePuller struct {
	objectPath string
	digest     string
	pullErr    error
	pulls      []platform.ImageRef
}

func newFakeImagePuller() *fakeImagePuller {
	return &fakeImagePuller{
		digest: "sha256:fake",
	}
}

func (p *fakeImagePuller) SetObjectPath(path string) {
	p.objectPath = path
}

func (p *fakeImagePuller) SetPullError(err error) {
	p.pullErr = err
}

func (p *fakeImagePuller) Pull(_ context.Context, ref platform.ImageRef) (platform.PulledImage, error) {
	p.pulls = append(p.pulls, ref)
	if p.pullErr != nil {
		return platform.PulledImage{}, p.pullErr
	}
	return platform.PulledImage{
		ObjectPath: p.objectPath,
		Digest:     p.digest,
	}, nil
}

func (p *fakeImagePuller) Pulls() []platform.ImageRef {
	return append([]platform.ImageRef(nil), p.pulls...)
}

// Ensure fakeImagePuller implements the interface.
var _ platform.ImagePuller = (*fakeImagePuller)(nil)

// kernelOp records an operation performed on the fake kernel.
type kernelOp struct {
	Op          string // "load", "unload", "attach", "detach", "attach-xdp-ext", "attach-tc-ext"
	Name        string // program or link name
	ID          uint32 // kernel ID assigned (untyped for recording purposes)
	Err         error  // error if operation failed
	ProgPinPath string // for XDP/TC extension attachments, the extension pin path used
}

// tcFilterKey identifies a TC filter by its location on an interface.
type tcFilterKey struct {
	netns    string
	ifindex  int
	parent   uint32
	priority uint16
}

type tcFilterDetach struct {
	tcFilterKey
	handle uint32
}

// clsactKey identifies the clsact qdisc slot on an interface.
type clsactKey struct {
	netns   string
	ifindex int
}

// fakeKernel implements platform.KernelOperations for testing.
// It simulates kernel BPF operations without actual syscalls.
type fakeKernel struct {
	nextID   atomic.Uint32
	programs map[kernel.ProgramID]fakeProgram
	links    map[kernel.LinkID]*bpfman.Link

	// TC filter handle tracking. A real attach point can briefly
	// contain both the old and new filter during a dispatcher swap,
	// so keep the handles separately; DetachTCFilter removes by exact
	// handle.
	tcFilters map[tcFilterKey][]uint32

	// clsact presence per (netns, ifindex): set when CreateTCFilter
	// installs the qdisc, cleared by RemoveTCClsactIfUnused once no
	// filters remain. Models bpfman owning the clsact lifecycle.
	clsacts map[clsactKey]bool

	// XDP dispatcher link tracking for UpdateXDPDispatcherLink. The
	// stable outer link survives dispatcher rebuilds and retargets a
	// different pinned dispatcher program on each revision.
	xdpDispatcherLinks map[string]string

	// Operation recording for verification
	ops              []kernelOp
	removePins       []string         // paths passed to RemovePin
	tcDetaches       []tcFilterDetach // TC filters detached
	uprobeAttachPids []int32          // pid filters received by uprobe attaches
	xdpConfigs       []dispatcher.XDPConfig
	tcConfigs        []dispatcher.TCConfig
	mu               sync.Mutex

	// Error injection - set these to control behaviour
	failOnProgram map[string]error // fail Load if program name matches
	failOnNthLoad int              // fail on Nth load (0 = never fail)
	loadCount     int              // track load count for failOnNthLoad

	// Attach error injection
	failOnAttach map[string]error // fail attach by type (e.g., "tracepoint", "kprobe")

	// Detach error injection
	failOnDetach map[kernel.LinkID]error // fail detach by link ID

	// Unload error injection: fail Unload when called with the
	// matching pin path. Used to simulate post-detach hygiene
	// failures such as a transient bpffs error on the program's
	// maps directory. failOnUnloadCalls counts how many times each
	// configured path was actually hit, so tests can assert the
	// injection fired rather than silently no-opping when the test's
	// computed path drifts from production's.
	failOnUnload      map[string]error
	failOnUnloadCalls map[string]int

	// Interface name -> ifindex resolution (what the manager now
	// queries via InterfaceByName). Seeded with the conventional test
	// interfaces; unknown names resolve to an error.
	ifaceIndex map[string]int

	// Interface error injection
	failOnIfname  map[string]error // fail attach if interface name matches
	failOnIfindex map[int]error    // fail attach if interface index matches

	// Tracepoint listing for pre-flight validation. When nil, ListTracepoints
	// returns nil (the "cannot validate" contract) and the manager's
	// pre-flight treats the attach as allowed. Tests that want to exercise
	// the validation path set this to a canned list.
	tracepoints []string
}

// fakeProgram stores program data for the fake kernel.
type fakeProgram struct {
	id          kernel.ProgramID
	name        string
	programType bpfman.ProgramType
	pinPath     string
	pinDir      string
}

// createPinFile creates a zero-byte file at path, simulating a
// kernel BPF object being pinned to bpffs. When the pin file is
// later removed (e.g. by os.RemoveAll on a revision directory),
// ProgramCount/LinkCount will detect the absence and garbage-collect
// the stale entry -- mirroring the real kernel's refcount semantics.
func createPinFile[P ~string](p P) {
	path := string(p)
	if path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, nil, 0644)
}

func createPinFileExclusive[P ~string](p P) error {
	path := string(p)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

func newFakeKernel() *fakeKernel {
	fk := &fakeKernel{
		programs:           make(map[kernel.ProgramID]fakeProgram),
		links:              make(map[kernel.LinkID]*bpfman.Link),
		tcFilters:          make(map[tcFilterKey][]uint32),
		clsacts:            make(map[clsactKey]bool),
		xdpDispatcherLinks: make(map[string]string),
		failOnProgram:      make(map[string]error),
		failOnAttach:       make(map[string]error),
		failOnDetach:       make(map[kernel.LinkID]error),
		failOnIfname:       make(map[string]error),
		failOnIfindex:      make(map[int]error),
		failOnUnload:       make(map[string]error),
		failOnUnloadCalls:  make(map[string]int),
		ifaceIndex:         map[string]int{"lo": 1, "eth0": 2},
	}
	fk.nextID.Store(100)
	return fk
}

// InjectInterface registers a name -> ifindex resolution for the fake
// kernel's InterfaceByName, mirroring an interface existing in the
// (possibly namespaced) target.
func (f *fakeKernel) InjectInterface(name string, ifindex int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ifaceIndex[name] = ifindex
}

// InterfaceByName resolves a name to its ifindex. It honours
// failOnIfname so existing lookup-failure injection still works, then
// the registry, then errors for an unknown interface -- matching the
// real adapter's "no such network interface".
func (f *fakeKernel) InterfaceByName(_ context.Context, name, _ string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.failOnIfname[name]; ok {
		return 0, err
	}
	if idx, ok := f.ifaceIndex[name]; ok {
		return idx, nil
	}
	// Mirror the real adapter: an unresolved interface wraps
	// platform.ErrInterfaceNotFound so callers can map it to an
	// invalid-argument status.
	return 0, fmt.Errorf("interface %q: %w", name, platform.ErrInterfaceNotFound)
}

// Operations returns a copy of recorded operations for verification.
func (f *fakeKernel) Operations() []kernelOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	ops := make([]kernelOp, len(f.ops))
	copy(ops, f.ops)
	return ops
}

// recordOp records an operation for later verification.
func (f *fakeKernel) recordOp(op, name string, id uint32, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, kernelOp{Op: op, Name: name, ID: id, Err: err})
}

// InjectKernelLink adds a link directly to the kernel state without going
// through the normal attach flow. This simulates a link that exists in the
// kernel but is not managed by bpfman.
func (f *fakeKernel) InjectKernelLink(id kernel.LinkID, kind bpfman.LinkKind) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[id] = &bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(id),
			Kind:         kind,
			KernelLinkID: &id,
		},
	}
}

// InjectKernelProgram adds a program directly to the kernel state without going
// through the normal load flow. This simulates a program that exists in the
// kernel but is not managed by bpfman.
func (f *fakeKernel) InjectKernelProgram(id kernel.ProgramID, name string, progType bpfman.ProgramType) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.programs[id] = fakeProgram{
		id:          id,
		name:        name,
		programType: progType,
	}
}

// RemoveKernelProgram simulates a program disappearing from the kernel
// (external unload, crash, or a daemon restart that lost the kernel
// objects) while its store record remains. Used to set up the
// dead-record reap path.
func (f *fakeKernel) RemoveKernelProgram(id kernel.ProgramID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.programs, id)
}

// recordExtensionAttach records an XDP/TC extension attachment with the progPinPath.
func (f *fakeKernel) recordExtensionAttach(op, programName string, id uint32, progPinPath string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, kernelOp{Op: op, Name: programName, ID: id, ProgPinPath: progPinPath})
}

// ExtensionAttachOps returns all XDP/TC extension attach operations.
func (f *fakeKernel) ExtensionAttachOps() []kernelOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ops []kernelOp
	for _, op := range f.ops {
		if op.Op == "attach-xdp-ext" || op.Op == "attach-tc-ext" {
			ops = append(ops, op)
		}
	}
	return ops
}

// recordTCXAttach records a TCX attachment with the programPinPath.
func (f *fakeKernel) recordTCXAttach(programPinPath string, id uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Reuse ProgPinPath field to store programPinPath for TCX
	f.ops = append(f.ops, kernelOp{Op: "attach-tcx", Name: programPinPath, ID: id})
}

// TCXAttachOps returns all TCX attach operations.
func (f *fakeKernel) TCXAttachOps() []kernelOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ops []kernelOp
	for _, op := range f.ops {
		if op.Op == "attach-tcx" {
			ops = append(ops, op)
		}
	}
	return ops
}

// FailOnProgram configures the kernel to fail when loading a specific program.
func (f *fakeKernel) FailOnProgram(name string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnProgram[name] = err
}

// FailOnNthLoad configures the kernel to fail on the Nth load attempt.
func (f *fakeKernel) FailOnNthLoad(n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnNthLoad = n
}

// FailOnAttach configures the kernel to fail when attaching a specific type.
// Valid types: "tracepoint", "kprobe", "uprobe", "fentry", "fexit", "xdp", "tc", "tcx"
func (f *fakeKernel) FailOnAttach(attachType string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnAttach[attachType] = err
}

// FailOnDetach configures the kernel to fail when detaching a specific link ID.
func (f *fakeKernel) FailOnDetach(linkID kernel.LinkID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnDetach[linkID] = err
}

// FailOnUnload configures Unload to return err when invoked with the
// given pin path. Used to simulate a post-detach hygiene failure.
func (f *fakeKernel) FailOnUnload(path string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnUnload[path] = err
}

// UnloadFailureCount returns how many times the configured Unload
// fault for path has actually fired. Tests use this to assert that
// the injection matched a real call, guarding against the case where
// the test's computed path no longer matches what production passes.
func (f *fakeKernel) UnloadFailureCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failOnUnloadCalls[path]
}

// FailOnIfname configures the kernel to fail when attaching to a specific interface.
func (f *fakeKernel) FailOnIfname(ifname string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnIfname[ifname] = err
}

// FailOnIfindex configures the kernel to fail when attaching to a specific interface index.
func (f *fakeKernel) FailOnIfindex(ifindex int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOnIfindex[ifindex] = err
}

// Reset clears all recorded operations and error injection settings.
func (f *fakeKernel) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = nil
	f.removePins = nil
	f.tcDetaches = nil
	f.tcFilters = make(map[tcFilterKey][]uint32)
	f.clsacts = make(map[clsactKey]bool)
	f.xdpDispatcherLinks = make(map[string]string)
	f.failOnProgram = make(map[string]error)
	f.failOnAttach = make(map[string]error)
	f.failOnDetach = make(map[kernel.LinkID]error)
	f.failOnIfname = make(map[string]error)
	f.failOnIfindex = make(map[int]error)
	f.failOnUnload = make(map[string]error)
	f.failOnUnloadCalls = make(map[string]int)
	f.failOnNthLoad = 0
	f.loadCount = 0
}

func (f *fakeKernel) HasPinByName(spec bpfman.LoadSpec) (bool, error) {
	return false, nil
}

func (f *fakeKernel) Load(_ context.Context, spec bpfman.LoadSpec, bpffs fs.BPFFS) (bpfman.LoadOutput, error) {
	// Validate program type - mirrors real kernel behaviour
	if !spec.ProgramType().Valid() {
		err := fmt.Errorf("invalid program type: %s", spec.ProgramType())
		f.recordOp("load", spec.ProgramName(), 0, err)
		return bpfman.LoadOutput{}, err
	}

	// Check error injection
	f.mu.Lock()
	f.loadCount++
	loadNum := f.loadCount
	failErr := f.failOnProgram[spec.ProgramName()]
	failOnNth := f.failOnNthLoad
	f.mu.Unlock()

	if failErr != nil {
		f.recordOp("load", spec.ProgramName(), 0, failErr)
		return bpfman.LoadOutput{}, failErr
	}

	if failOnNth > 0 && loadNum == failOnNth {
		err := fmt.Errorf("injected error on load %d", loadNum)
		f.recordOp("load", spec.ProgramName(), 0, err)
		return bpfman.LoadOutput{}, err
	}

	progID := kernel.ProgramID(f.nextID.Add(1))
	// Compute paths the same way the real kernel does - using bpffs methods
	progPinPath := bpffs.ProgPinPath(progID)

	// Map sharing: if MapOwnerID is set, use the owner's maps directory
	var mapsDir bpfman.MapDir
	if spec.MapOwnerID() != 0 {
		// Share maps with the owner program
		mapsDir = bpffs.MapPinDir(spec.MapOwnerID())
	} else {
		// Own maps - use our kernel ID
		mapsDir = bpffs.MapPinDir(progID)
	}

	// Create the pin file on disk so that GC's ownership check
	// (os.Stat on the pin path) recognises this as our program.
	if err := os.MkdirAll(bpffs.MountPoint(), 0755); err != nil {
		return bpfman.LoadOutput{}, fmt.Errorf("fake kernel: mkdir pin dir: %w", err)
	}

	if err := os.WriteFile(progPinPath.String(), nil, 0644); err != nil {
		return bpfman.LoadOutput{}, fmt.Errorf("fake kernel: create pin file: %w", err)
	}

	fp := fakeProgram{
		id:          progID,
		name:        spec.ProgramName(),
		programType: spec.ProgramType(),
		pinPath:     progPinPath.String(),
		pinDir:      mapsDir.String(),
	}
	f.programs[progID] = fp
	f.recordOp("load", spec.ProgramName(), uint32(progID), nil)
	return bpfman.LoadOutput{
		PinPath:      bpfman.ProgPinPath(fp.pinPath),
		MapsDir:      bpfman.MapDir(fp.pinDir),
		License:      "GPL",
		InferredType: fp.programType,
		Program: &kernel.Program{
			ID:          fp.id,
			Name:        fp.name,
			ProgramType: kernel.NewProgramType(fp.programType.String()),
		},
	}, nil
}

func (f *fakeKernel) Unload(_ context.Context, pinPath string) error {
	f.mu.Lock()
	if err, ok := f.failOnUnload[pinPath]; ok {
		f.failOnUnloadCalls[pinPath]++
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	for id, p := range f.programs {
		// Match by either program pin path or maps directory
		if p.pinPath == pinPath || p.pinDir == pinPath {
			delete(f.programs, id)
			f.recordOp("unload", p.name, uint32(id), nil)
			return nil
		}
	}
	return nil
}

func (f *fakeKernel) UnloadProgram(_ context.Context, progPinPath bpfman.ProgPinPath, mapsDir string) error {
	// Fake implementation - just removes any program whose pin path matches
	for id, p := range f.programs {
		if p.pinPath == progPinPath.String() || p.pinDir == mapsDir {
			delete(f.programs, id)
			f.recordOp("unload", p.name, uint32(id), nil)
			return nil
		}
	}
	return nil
}

// ProgramCount returns the number of programs currently loaded.
// Programs whose pin files have been removed from disk (e.g. by
// RemoveDispatcherRevDir) are garbage-collected, mirroring the
// kernel's behaviour of releasing objects when pins are removed.
func (f *fakeKernel) ProgramCount() int {
	for id, prog := range f.programs {
		if prog.pinPath != "" {
			if _, err := os.Stat(prog.pinPath); os.IsNotExist(err) {
				delete(f.programs, id)
			}
		}
	}
	return len(f.programs)
}

// LinkCount returns the number of links currently tracked. Links
// whose pin files have been removed are garbage-collected.
// UprobeAttachPids returns the pid filters received by uprobe
// attaches, in attach order. Tests use it to prove the pid crossed
// the kernel boundary rather than only decorating the details.
func (f *fakeKernel) UprobeAttachPids() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int32, len(f.uprobeAttachPids))
	copy(out, f.uprobeAttachPids)
	return out
}

func (f *fakeKernel) LinkCount() int {
	for id, link := range f.links {
		if link.Record.PinPath != nil {
			pinStr := link.Record.PinPath.String()
			if pinStr != "" {
				if _, err := os.Stat(pinStr); os.IsNotExist(err) {
					delete(f.links, id)
				}
			}
		}
	}
	return len(f.links)
}

func (f *fakeKernel) Programs(_ context.Context) iter.Seq2[kernel.Program, error] {
	return func(yield func(kernel.Program, error) bool) {
		for id, p := range f.programs {
			kp := kernel.Program{
				ID:          id,
				Name:        p.name,
				ProgramType: kernel.NewProgramType(p.programType.String()),
			}
			if !yield(kp, nil) {
				return
			}
		}
	}
}

func (f *fakeKernel) GetProgramByID(_ context.Context, id kernel.ProgramID) (kernel.Program, error) {
	p, ok := f.programs[id]
	if !ok {
		// Mirror the real adapter: cilium/ebpf reports a missing
		// program ID as os.ErrNotExist, which the manager relies on to
		// tell "gone" apart from "could not inspect".
		return kernel.Program{}, fmt.Errorf("program %d: %w", id, os.ErrNotExist)
	}
	return kernel.Program{
		ID:          id,
		Name:        p.name,
		ProgramType: kernel.NewProgramType(p.programType.String()),
	}, nil
}

func (f *fakeKernel) GetProgramStatsByID(_ context.Context, id kernel.ProgramID) (*kernel.ProgramStats, error) {
	// fakeKernel doesn't track stats, return nil (stats unavailable)
	return nil, nil
}

func (f *fakeKernel) GetLinkByID(_ context.Context, id kernel.LinkID) (kernel.Link, error) {
	link, ok := f.links[id]
	if !ok {
		return kernel.Link{}, fmt.Errorf("link %d not found", id)
	}
	return kernel.Link{
		ID:        id,
		LinkType:  link.Record.Kind.String(),
		ProgramID: 0, // fakeKernel doesn't track program association
	}, nil
}

func (f *fakeKernel) GetMapByID(_ context.Context, id kernel.MapID) (kernel.Map, error) {
	// fakeKernel doesn't track maps, return a minimal stub
	return kernel.Map{ID: id}, nil
}

func (f *fakeKernel) Maps(_ context.Context) iter.Seq2[kernel.Map, error] {
	return func(yield func(kernel.Map, error) bool) {}
}

func (f *fakeKernel) Links(_ context.Context) iter.Seq2[kernel.Link, error] {
	return func(yield func(kernel.Link, error) bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for id := range f.links {
			kl := kernel.Link{
				ID: id,
			}
			if !yield(kl, nil) {
				return
			}
		}
	}
}

func (f *fakeKernel) ListPinDir(_ context.Context, pinDir string, includeMaps bool) (*kernel.PinDirContents, error) {
	return &kernel.PinDirContents{}, nil
}

func (f *fakeKernel) GetPinned(_ context.Context, pinPath string) (*kernel.PinnedProgram, error) {
	return nil, nil
}

func (f *fakeKernel) AttachTracepoint(_ context.Context, progPinPath bpfman.ProgPinPath, group, name string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	// Check error injection
	f.mu.Lock()
	failErr := f.failOnAttach["tracepoint"]
	f.mu.Unlock()
	if failErr != nil {
		f.recordOp("attach", "tracepoint:"+group+"/"+name, 0, failErr)
		return bpfman.AttachOutput{}, failErr
	}

	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "tracepoint"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindTracepoint,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.TracepointDetails{Group: group, Name: name},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	f.recordOp("attach", "tracepoint:"+group+"/"+name, uint32(linkID), nil)
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) AttachXDP(_ context.Context, progPinPath bpfman.ProgPinPath, ifindex int, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "xdp"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindXDP,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.XDPDetails{Ifindex: uint32(ifindex)},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) AttachKprobe(_ context.Context, progPinPath bpfman.ProgPinPath, fnName string, offset uint64, retprobe bool, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	linkKind := bpfman.LinkKindKprobe
	kernelLinkType := "kprobe"
	if retprobe {
		linkKind = bpfman.LinkKindKretprobe
		kernelLinkType = "kretprobe"
	}
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: kernelLinkType}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         linkKind,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.KprobeDetails{FnName: fnName, Offset: offset, Retprobe: retprobe},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) AttachUprobeLocal(_ context.Context, progPinPath bpfman.ProgPinPath, target, fnName string, offset uint64, pid int32, retprobe bool, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	f.mu.Lock()
	f.uprobeAttachPids = append(f.uprobeAttachPids, pid)
	f.mu.Unlock()
	linkKind := bpfman.LinkKindUprobe
	kernelLinkType := "uprobe"
	if retprobe {
		linkKind = bpfman.LinkKindUretprobe
		kernelLinkType = "uretprobe"
	}
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: kernelLinkType}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         linkKind,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.UprobeDetails{Target: target, FnName: fnName, Offset: offset, PID: pid, Retprobe: retprobe, ContainerPid: 0},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) AttachUprobeContainer(_ context.Context, _ lock.WriterScope, progPinPath bpfman.ProgPinPath, target, fnName string, offset uint64, pid int32, retprobe bool, linkPinPath bpfman.LinkPath, containerPid int32) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	f.mu.Lock()
	f.uprobeAttachPids = append(f.uprobeAttachPids, pid)
	f.mu.Unlock()
	linkKind := bpfman.LinkKindUprobe
	kernelLinkType := "uprobe"
	if retprobe {
		linkKind = bpfman.LinkKindUretprobe
		kernelLinkType = "uretprobe"
	}
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: kernelLinkType}
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         linkKind,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.UprobeDetails{Target: target, FnName: fnName, Offset: offset, PID: pid, Retprobe: retprobe, ContainerPid: containerPid},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) AttachFentry(_ context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "fentry"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindFentry,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.FentryDetails{FnName: fnName},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) AttachFexit(_ context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "fexit"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindFexit,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.FexitDetails{FnName: fnName},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) DetachLink(_ context.Context, linkPinPath bpfman.LinkPath) error {
	path := linkPinPath.String()
	for id, link := range f.links {
		pinPath := ""
		if link.Record.PinPath != nil {
			pinPath = link.Record.PinPath.String()
		}
		if pinPath == linkPinPath.String() {
			// Check error injection
			f.mu.Lock()
			failErr := f.failOnDetach[id]
			f.mu.Unlock()
			if failErr != nil {
				f.recordOp("detach", path, uint32(id), failErr)
				return failErr
			}
			delete(f.links, id)
			delete(f.xdpDispatcherLinks, path)
			f.recordOp("detach", path, uint32(id), nil)
			return nil
		}
	}
	// Link not found - still record the detach attempt
	delete(f.xdpDispatcherLinks, path)
	f.recordOp("detach", path, 0, nil)
	return nil
}

func (f *fakeKernel) AttachXDPExtension(_ context.Context, spec dispatcher.XDPExtensionAttachSpec) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(spec.LinkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "xdp"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindXDP,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(spec.LinkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.XDPDetails{Position: int32(spec.Position)},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: spec.LinkPinPath != "",
		},
	}
	f.links[linkID] = &link
	// Record the operation with progPinPath for test verification
	f.recordExtensionAttach("attach-xdp-ext", spec.ProgramName, uint32(linkID), spec.ProgPinPath.String())
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      spec.LinkPinPath,
	}, nil
}

func (f *fakeKernel) DetachTCFilter(_ context.Context, ifindex int, ifname string, parent uint32, priority uint16, handle uint32, netnsPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tcFilterKey{netns: netnsPath, ifindex: ifindex, parent: parent, priority: priority}
	f.tcDetaches = append(f.tcDetaches, tcFilterDetach{tcFilterKey: key, handle: handle})
	handles := f.tcFilters[key]
	for i, h := range handles {
		if h == handle {
			handles = append(handles[:i], handles[i+1:]...)
			break
		}
	}
	if len(handles) == 0 {
		delete(f.tcFilters, key)
	} else {
		f.tcFilters[key] = handles
	}
	return nil
}

func (f *fakeKernel) ExtensionLinkInfo(_ context.Context, _ bpfman.LinkPath) (platform.ExtensionLinkInfo, error) {
	return platform.ExtensionLinkInfo{}, nil
}

func (f *fakeKernel) AttachTCExtension(_ context.Context, spec dispatcher.TCExtensionAttachSpec) (bpfman.AttachOutput, error) {
	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(spec.LinkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "tc"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindTC,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(spec.LinkPinPath),
			CreatedAt:    time.Now(),
			Details:      bpfman.TCDetails{Position: int32(spec.Position)},
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: spec.LinkPinPath != "",
		},
	}
	f.links[linkID] = &link
	// Record the operation with progPinPath for test verification
	f.recordExtensionAttach("attach-tc-ext", spec.ProgramName, uint32(linkID), spec.ProgPinPath.String())
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      spec.LinkPinPath,
	}, nil
}

func (f *fakeKernel) AttachTCX(_ context.Context, ifindex int, direction string, programPinPath bpfman.ProgPinPath, linkPinPath bpfman.LinkPath, netns string, order bpfman.TCXAttachOrder) (bpfman.AttachOutput, error) {
	// Check for interface-specific failure injection
	f.mu.Lock()
	if err, ok := f.failOnIfindex[ifindex]; ok {
		f.mu.Unlock()
		return bpfman.AttachOutput{}, err
	}
	f.mu.Unlock()

	linkID := kernel.LinkID(f.nextID.Add(1))
	createPinFile(linkPinPath)
	kl := kernel.Link{ID: linkID, ProgramID: 0, LinkType: "tcx"}
	// Store for DetachLink lookup and kernel iteration
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           bpfman.LinkID(linkID),
			Kind:         bpfman.LinkKindTCX,
			KernelLinkID: &linkID,
			PinPath:      bpfman.NewLinkPath(linkPinPath),
			CreatedAt:    time.Now(),
		},
		Status: bpfman.LinkStatus{
			Kernel:     &kl,
			KernelSeen: true,
			PinPresent: linkPinPath != "",
		},
	}
	f.links[linkID] = &link
	// Record the operation with programPinPath for test verification
	f.recordTCXAttach(programPinPath.String(), uint32(linkID))
	return bpfman.AttachOutput{
		KernelLinkID: &linkID,
		KernelLink:   &kl,
		PinPath:      linkPinPath,
	}, nil
}

func (f *fakeKernel) RemovePin(_ context.Context, p bpfman.ProgPinPath) error {
	path := p.String()
	f.mu.Lock()
	f.removePins = append(f.removePins, path)
	f.mu.Unlock()

	// Remove programs matching this pin path (for dispatcher cleanup).
	for id, prog := range f.programs {
		if prog.pinPath == path {
			delete(f.programs, id)
			break
		}
	}

	// Remove links whose pin paths are under this directory. This
	// simulates bpffs directory removal releasing pinned links.
	dirPrefix := path + "/"
	for id, link := range f.links {
		if link.Record.PinPath != nil {
			pinStr := link.Record.PinPath.String()
			if pinStr == path || strings.HasPrefix(pinStr, dirPrefix) {
				delete(f.links, id)
			}
		}
	}
	return nil
}

// RemovedPins returns a copy of all paths passed to RemovePin.
func (f *fakeKernel) RemovedPins() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.removePins))
	copy(result, f.removePins)
	return result
}

// TCDetaches returns a copy of all TC filter detach operations.
func (f *fakeKernel) TCDetaches() []tcFilterKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]tcFilterKey, len(f.tcDetaches))
	for i, d := range f.tcDetaches {
		result[i] = d.tcFilterKey
	}
	return result
}

func (f *fakeKernel) TCDetachEvents() []tcFilterDetach {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]tcFilterDetach, len(f.tcDetaches))
	copy(result, f.tcDetaches)
	return result
}

func (f *fakeKernel) TCFilterHandles() []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []uint32
	for _, handles := range f.tcFilters {
		result = append(result, handles...)
	}
	return result
}

// TCFilterCount returns the number of TC filters currently tracked.
func (f *fakeKernel) TCFilterCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, handles := range f.tcFilters {
		count += len(handles)
	}
	return count
}

func (f *fakeKernel) XDPDispatcherConfigs() []dispatcher.XDPConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dispatcher.XDPConfig(nil), f.xdpConfigs...)
}

func (f *fakeKernel) TCDispatcherConfigs() []dispatcher.TCConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dispatcher.TCConfig(nil), f.tcConfigs...)
}

func (f *fakeKernel) RepinMap(_ context.Context, srcPath, dstPath string) error {
	return nil // Fake implementation - no-op
}

func (f *fakeKernel) UpdateXDPDispatcherLink(_ context.Context, linkPinPath bpfman.LinkPath, newProgPinPath bpfman.ProgPinPath) error {
	f.mu.Lock()
	f.xdpDispatcherLinks[linkPinPath.String()] = newProgPinPath.String()
	f.mu.Unlock()
	f.recordOp("update-xdp-link", linkPinPath.String()+" -> "+newProgPinPath.String(), 0, nil)
	return nil
}

func (f *fakeKernel) XDPDispatcherTarget(linkPinPath bpfman.LinkPath) (bpfman.ProgPinPath, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	target, ok := f.xdpDispatcherLinks[linkPinPath.String()]
	return bpfman.ProgPinPath(target), ok
}

func (f *fakeKernel) LoadAndPinXDPDispatcher(_ context.Context, cfg dispatcher.XDPConfig, progPinPath bpfman.ProgPinPath) (kernel.ProgramID, error) {
	f.mu.Lock()
	f.xdpConfigs = append(f.xdpConfigs, cfg)
	f.mu.Unlock()

	dispatcherID := kernel.ProgramID(f.nextID.Add(1))
	if err := createPinFileExclusive(progPinPath); err != nil {
		f.recordOp("load-pin-xdp-dispatcher", progPinPath.String(), 0, err)
		return 0, err
	}
	f.programs[dispatcherID] = fakeProgram{
		id:          dispatcherID,
		name:        "xdp_dispatcher",
		programType: bpfman.ProgramTypeXDP,
		pinPath:     progPinPath.String(),
	}
	f.recordOp("load-pin-xdp-dispatcher", progPinPath.String(), uint32(dispatcherID), nil)
	return dispatcherID, nil
}

func (f *fakeKernel) LoadAndPinTCDispatcher(_ context.Context, cfg dispatcher.TCConfig, progPinPath bpfman.ProgPinPath) (kernel.ProgramID, error) {
	f.mu.Lock()
	f.tcConfigs = append(f.tcConfigs, cfg)
	f.mu.Unlock()

	dispatcherID := kernel.ProgramID(f.nextID.Add(1))
	if err := createPinFileExclusive(progPinPath); err != nil {
		f.recordOp("load-pin-tc-dispatcher", progPinPath.String(), 0, err)
		return 0, err
	}
	f.programs[dispatcherID] = fakeProgram{
		id:          dispatcherID,
		name:        "tc_dispatcher",
		programType: bpfman.ProgramTypeTC,
		pinPath:     progPinPath.String(),
	}
	f.recordOp("load-pin-tc-dispatcher", progPinPath.String(), uint32(dispatcherID), nil)
	return dispatcherID, nil
}

func (f *fakeKernel) CreateXDPLink(_ context.Context, progPinPath bpfman.ProgPinPath, ifindex int, linkPinPath bpfman.LinkPath, netnsPath string) (*platform.XDPDispatcherResult, error) {
	// Check for interface-specific failure injection
	f.mu.Lock()
	if err, ok := f.failOnIfindex[ifindex]; ok {
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Unlock()

	createPinFile(linkPinPath)
	dispatcherID := kernel.ProgramID(0) // Not easily available from pin
	linkID := kernel.LinkID(f.nextID.Add(1))
	f.mu.Lock()
	f.xdpDispatcherLinks[linkPinPath.String()] = progPinPath.String()
	f.mu.Unlock()
	f.recordOp("create-xdp-link", linkPinPath.String(), uint32(linkID), nil)
	return &platform.XDPDispatcherResult{
		DispatcherID:  dispatcherID,
		KernelLinkID:  linkID,
		DispatcherPin: progPinPath,
		LinkPin:       linkPinPath,
	}, nil
}

func (f *fakeKernel) CreateTCFilter(_ context.Context, progPinPath bpfman.ProgPinPath, ifindex int, ifname string, direction bpfman.TCDirection, netnsPath string, desiredHandle uint32) (*platform.TCDispatcherResult, error) {
	// Check for interface-specific failure injection
	f.mu.Lock()
	if err, ok := f.failOnIfname[ifname]; ok {
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Unlock()

	dispatcherID := kernel.ProgramID(0)
	// A non-zero desiredHandle requests that exact handle (the rollback
	// path restoring a filter under its recorded handle); otherwise the
	// kernel would assign one, modelled here by a fresh id.
	handle := desiredHandle
	if handle == 0 {
		handle = f.nextID.Add(1)
	}

	var parent uint32
	switch direction {
	case bpfman.TCDirectionIngress:
		parent = 0xFFFFFFF2
	case bpfman.TCDirectionEgress:
		parent = 0xFFFFFFF3
	}

	f.mu.Lock()
	key := tcFilterKey{netns: netnsPath, ifindex: ifindex, parent: parent, priority: 50}
	f.tcFilters[key] = append(f.tcFilters[key], handle)
	// Installing a filter implies the clsact qdisc is present (bpfman
	// creates it on first attach).
	f.clsacts[clsactKey{netns: netnsPath, ifindex: ifindex}] = true
	f.mu.Unlock()

	f.recordOp("create-tc-filter", progPinPath.String(), handle, nil)
	return &platform.TCDispatcherResult{
		DispatcherID:  dispatcherID,
		DispatcherPin: progPinPath,
		Handle:        handle,
		Priority:      50,
	}, nil
}

// RemoveTCClsactIfUnused models reclaiming the clsact qdisc: it clears
// the modelled clsact for (netns, ifindex) only when no filters remain
// on that interface, mirroring the real "both blocks empty" gate.
func (f *fakeKernel) RemoveTCClsactIfUnused(_ context.Context, ifindex int, ifname string, netnsPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.tcFilters {
		if k.netns == netnsPath && k.ifindex == ifindex {
			return nil // filters remain; leave the clsact
		}
	}
	delete(f.clsacts, clsactKey{netns: netnsPath, ifindex: ifindex})
	return nil
}

// ClsactPresent reports whether the modelled clsact qdisc exists on the
// given attach point.
func (f *fakeKernel) ClsactPresent(netnsPath string, ifindex int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clsacts[clsactKey{netns: netnsPath, ifindex: ifindex}]
}

func (f *fakeKernel) ListTracepoints(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tracepoints == nil {
		return nil, nil
	}
	out := make([]string, len(f.tracepoints))
	copy(out, f.tracepoints)
	return out, nil
}
