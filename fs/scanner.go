package fs

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/kernel"
)

// Scanner provides read-only access to bpfman's bpffs layout.
// It encapsulates path conventions and provides streaming iterators
// for filesystem facts.
type Scanner struct {
	b           BPFFS
	onMalformed func(path string, err error)
}

// NewScanner creates a Scanner for the given BPFFS domain.
func NewScanner(b BPFFS) *Scanner {
	b.mustValid()
	return &Scanner{b: b}
}

// WithOnMalformed sets a callback for unparseable filesystem entries.
// The callback receives the path and the parse error. Returns the
// Scanner for chaining.
func (s *Scanner) WithOnMalformed(f func(path string, err error)) *Scanner {
	s.onMalformed = f
	return s
}

// reportMalformed calls the OnMalformed callback if set.
func (s *Scanner) reportMalformed(path string, err error) {
	if s.onMalformed != nil {
		s.onMalformed(path, err)
	}
}

// ProgPin represents a program pin: {fs}/prog_{program_id}
type ProgPin struct {
	// Path is the full path to the pin file.
	Path string `json:"path"`

	// ProgramID is the kernel program ID parsed from the pin name.
	ProgramID kernel.ProgramID `json:"program_id"`
}

// MapDir represents a map directory: {fs}/maps/{program_id}
type MapDir struct {
	// Path is the full path to the map directory.
	Path string `json:"path"`

	// ProgramID is the kernel program ID parsed from the directory name.
	ProgramID kernel.ProgramID `json:"program_id"`
}

// DispatcherDir represents a dispatcher revision directory.
// Path: {fs}/{type}/dispatcher_{nsid}_{ifindex}_{revision}
// LinkCount is derived by counting link_* files in the directory.
type DispatcherDir struct {
	// Path is the full path to the dispatcher revision directory.
	Path string `json:"path"`

	// DispType is the dispatcher type: "xdp", "tc-ingress", or
	// "tc-egress".
	DispType string `json:"disp_type"`

	// Nsid is the network namespace ID the dispatcher belongs to.
	Nsid uint64 `json:"nsid"`

	// Ifindex is the index of the interface the dispatcher is attached
	// to.
	Ifindex uint32 `json:"ifindex"`

	// Revision is the dispatcher revision number parsed from the
	// directory name.
	Revision uint32 `json:"revision"`

	// LinkCount is the number of link_* files in the directory.
	LinkCount int `json:"link_count"`
}

// DispatcherLinkPin represents a dispatcher link pin.
// Path: {fs}/{type}/dispatcher_{nsid}_{ifindex}_link
type DispatcherLinkPin struct {
	// Path is the full path to the dispatcher link pin file.
	Path string `json:"path"`

	// DispType is the dispatcher type: "xdp", "tc-ingress", or
	// "tc-egress".
	DispType string `json:"disp_type"`

	// Nsid is the network namespace ID the dispatcher belongs to.
	Nsid uint64 `json:"nsid"`

	// Ifindex is the index of the interface the dispatcher is attached
	// to.
	Ifindex uint32 `json:"ifindex"`
}

// SharedMapPin represents a shared PinByName map pin: {fs}/shared/{map_name}
type SharedMapPin struct {
	// Path is the full path to the shared map pin file.
	Path string `json:"path"`

	// MapName is the map name, taken from the pin file's base name.
	MapName string `json:"map_name"`
}

// FSState is a materialised snapshot of the filesystem.
// Use Scanner.Scan() to create, or construct directly in tests.
type FSState struct {
	// ProgPins are the program pins under {fs}/prog_*.
	ProgPins []ProgPin `json:"prog_pins"`

	// MapDirs are the per-program map directories under {fs}/maps/.
	MapDirs []MapDir `json:"map_dirs"`

	// DispatcherDirs are the dispatcher revision directories across the
	// XDP and TC trees.
	DispatcherDirs []DispatcherDir `json:"dispatcher_dirs"`

	// DispatcherLinkPins are the dispatcher link pins across the XDP and
	// TC trees.
	DispatcherLinkPins []DispatcherLinkPin `json:"dispatcher_link_pins"`

	// SharedMapPins are the shared PinByName map pins under {fs}/shared/.
	SharedMapPins []SharedMapPin `json:"shared_map_pins"`
}

// fs returns the bpffs mount point path.
func (s *Scanner) fs() string {
	return s.b.mountPoint()
}

// MountPoint returns the bpffs mount point this scanner is
// rooted at. Exposed so callers that need to walk the whole
// tree (e.g. inspect's link-pin indexer) can do so without
// re-encoding the layout's path conventions.
func (s *Scanner) MountPoint() string {
	return s.b.MountPoint()
}

// xdpDir returns the XDP dispatcher directory.
func (s *Scanner) xdpDir() string {
	return s.b.xdpDir()
}

// tcIngressDir returns the TC ingress dispatcher directory.
func (s *Scanner) tcIngressDir() string {
	return s.b.tcIngressDir()
}

// tcEgressDir returns the TC egress dispatcher directory.
func (s *Scanner) tcEgressDir() string {
	return s.b.tcEgressDir()
}

// mapsDir returns the maps directory.
func (s *Scanner) mapsDir() string {
	return s.b.mapsDir()
}

// ProgPins returns an iterator over program pins in {fs}/prog_*.
// Errors are yielded only for failures that prevent enumeration.
// Malformed entries are skipped and reported via OnMalformed.
func (s *Scanner) ProgPins(ctx context.Context) iter.Seq2[ProgPin, error] {
	return func(yield func(ProgPin, error) bool) {
		fs := s.fs()
		entries, err := os.ReadDir(fs)
		if err != nil {
			if os.IsNotExist(err) {
				return // directory doesn't exist: no pins
			}
			yield(ProgPin{}, fmt.Errorf("read dir %s: %w", fs, err))
			return
		}

		for _, entry := range entries {
			if ctx.Err() != nil {
				yield(ProgPin{}, ctx.Err())
				return
			}

			name := entry.Name()
			if !strings.HasPrefix(name, "prog_") {
				continue
			}

			suffix := strings.TrimPrefix(name, "prog_")
			id, err := strconv.ParseUint(suffix, 10, 32)
			if err != nil {
				s.reportMalformed(filepath.Join(fs, name), fmt.Errorf("parse program ID: %w", err))
				continue
			}

			pin := ProgPin{
				Path:      filepath.Join(fs, name),
				ProgramID: kernel.ProgramID(id),
			}
			if !yield(pin, nil) {
				return
			}
		}
	}
}

// MapDirs returns an iterator over map directories in {fs}/maps/{program_id}.
// Errors are yielded only for failures that prevent enumeration.
// Malformed entries are skipped and reported via OnMalformed.
func (s *Scanner) MapDirs(ctx context.Context) iter.Seq2[MapDir, error] {
	return func(yield func(MapDir, error) bool) {
		mapsDir := s.mapsDir()
		entries, err := os.ReadDir(mapsDir)
		if err != nil {
			if os.IsNotExist(err) {
				return // directory doesn't exist: no map dirs
			}
			yield(MapDir{}, fmt.Errorf("read dir %s: %w", mapsDir, err))
			return
		}

		for _, entry := range entries {
			if ctx.Err() != nil {
				yield(MapDir{}, ctx.Err())
				return
			}

			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			id, err := strconv.ParseUint(name, 10, 32)
			if err != nil {
				s.reportMalformed(filepath.Join(mapsDir, name), fmt.Errorf("parse program ID: %w", err))
				continue
			}

			dir := MapDir{
				Path:      filepath.Join(mapsDir, name),
				ProgramID: kernel.ProgramID(id),
			}
			if !yield(dir, nil) {
				return
			}
		}
	}
}

// DispatcherDirs returns an iterator over dispatcher revision directories.
// Path pattern: {fs}/{type}/dispatcher_{nsid}_{ifindex}_{revision}
// LinkCount is the number of link_* files in each directory.
// Errors are yielded only for failures that prevent enumeration.
// Malformed entries are skipped and reported via OnMalformed.
func (s *Scanner) DispatcherDirs(ctx context.Context) iter.Seq2[DispatcherDir, error] {
	return func(yield func(DispatcherDir, error) bool) {
		dispTypes := []struct {
			name string
			dir  string
		}{
			{"xdp", s.xdpDir()},
			{"tc-ingress", s.tcIngressDir()},
			{"tc-egress", s.tcEgressDir()},
		}

		for _, t := range dispTypes {
			if ctx.Err() != nil {
				yield(DispatcherDir{}, ctx.Err())
				return
			}

			entries, err := os.ReadDir(t.dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue // directory doesn't exist
				}
				if !yield(DispatcherDir{}, fmt.Errorf("read dir %s: %w", t.dir, err)) {
					return
				}
				continue
			}

			for _, entry := range entries {
				if ctx.Err() != nil {
					yield(DispatcherDir{}, ctx.Err())
					return
				}

				if !entry.IsDir() {
					continue
				}

				name := entry.Name()
				if !strings.HasPrefix(name, "dispatcher_") {
					continue
				}

				// Parse dispatcher_{nsid}_{ifindex}_{revision}
				nsid, ifindex, revision, ok := parseDispatcherDirName(name)
				if !ok {
					s.reportMalformed(filepath.Join(t.dir, name), fmt.Errorf("parse dispatcher dir: expected dispatcher_NSID_IFINDEX_REV"))
					continue
				}

				dirPath := filepath.Join(t.dir, name)
				linkCount := s.countLinkFiles(dirPath)

				dir := DispatcherDir{
					Path:      dirPath,
					DispType:  t.name,
					Nsid:      nsid,
					Ifindex:   ifindex,
					Revision:  revision,
					LinkCount: linkCount,
				}
				if !yield(dir, nil) {
					return
				}
			}
		}
	}
}

// countLinkFiles counts link_* files in a directory.
func (s *Scanner) countLinkFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "link_") {
			count++
		}
	}
	return count
}

// DispatcherLinkPins returns an iterator over dispatcher link pins.
// Path pattern: {fs}/{type}/dispatcher_{nsid}_{ifindex}_link
// Errors are yielded only for failures that prevent enumeration.
// Malformed entries are skipped and reported via OnMalformed.
func (s *Scanner) DispatcherLinkPins(ctx context.Context) iter.Seq2[DispatcherLinkPin, error] {
	return func(yield func(DispatcherLinkPin, error) bool) {
		dispTypes := []struct {
			name string
			dir  string
		}{
			{"xdp", s.xdpDir()},
			{"tc-ingress", s.tcIngressDir()},
			{"tc-egress", s.tcEgressDir()},
		}

		for _, t := range dispTypes {
			if ctx.Err() != nil {
				yield(DispatcherLinkPin{}, ctx.Err())
				return
			}

			entries, err := os.ReadDir(t.dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				if !yield(DispatcherLinkPin{}, fmt.Errorf("read dir %s: %w", t.dir, err)) {
					return
				}
				continue
			}

			for _, entry := range entries {
				if ctx.Err() != nil {
					yield(DispatcherLinkPin{}, ctx.Err())
					return
				}

				if entry.IsDir() {
					continue
				}

				name := entry.Name()
				if !strings.HasPrefix(name, "dispatcher_") || !strings.HasSuffix(name, "_link") {
					continue
				}

				// Parse dispatcher_{nsid}_{ifindex}_link
				nsid, ifindex, ok := parseDispatcherLinkName(name)
				if !ok {
					s.reportMalformed(filepath.Join(t.dir, name), fmt.Errorf("parse dispatcher link pin: expected dispatcher_NSID_IFINDEX_link"))
					continue
				}

				pin := DispatcherLinkPin{
					Path:     filepath.Join(t.dir, name),
					DispType: t.name,
					Nsid:     nsid,
					Ifindex:  ifindex,
				}
				if !yield(pin, nil) {
					return
				}
			}
		}
	}
}

// SharedMapPins returns an iterator over shared map pins in {fs}/shared/.
// Errors are yielded only for failures that prevent enumeration.
func (s *Scanner) SharedMapPins(ctx context.Context) iter.Seq2[SharedMapPin, error] {
	return func(yield func(SharedMapPin, error) bool) {
		sharedDir := s.b.SharedMapPinDir()
		entries, err := os.ReadDir(sharedDir)
		if err != nil {
			if os.IsNotExist(err) {
				return // directory doesn't exist: no shared pins
			}
			yield(SharedMapPin{}, fmt.Errorf("read dir %s: %w", sharedDir, err))
			return
		}

		for _, entry := range entries {
			if ctx.Err() != nil {
				yield(SharedMapPin{}, ctx.Err())
				return
			}

			if entry.IsDir() {
				continue
			}

			pin := SharedMapPin{
				Path:    filepath.Join(sharedDir, entry.Name()),
				MapName: entry.Name(),
			}
			if !yield(pin, nil) {
				return
			}
		}
	}
}

// PathExists checks if a path exists on the filesystem.
// Used to verify store-recorded pin paths actually exist.
func (s *Scanner) PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func parseDispatcherDirName(name string) (nsid uint64, ifindex, revision uint32, ok bool) {
	if !strings.HasPrefix(name, "dispatcher_") {
		return 0, 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(name, "dispatcher_"), "_")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	parsedNsid, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}

	parsedIfindex, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, 0, false
	}

	parsedRevision, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return parsedNsid, uint32(parsedIfindex), uint32(parsedRevision), true
}

func parseDispatcherLinkName(name string) (nsid uint64, ifindex uint32, ok bool) {
	if !strings.HasPrefix(name, "dispatcher_") || !strings.HasSuffix(name, "_link") {
		return 0, 0, false
	}
	trim := strings.TrimSuffix(strings.TrimPrefix(name, "dispatcher_"), "_link")
	parts := strings.Split(trim, "_")
	if len(parts) != 2 {
		return 0, 0, false
	}
	parsedNsid, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	parsedIfindex, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return parsedNsid, uint32(parsedIfindex), true
}

// Scan materialises everything into an FSState.
// Returns an error if any iterator encounters a fatal error.
func (s *Scanner) Scan(ctx context.Context) (*FSState, error) {
	state := &FSState{}

	for pin, err := range s.ProgPins(ctx) {
		if err != nil {
			return nil, fmt.Errorf("scan prog pins: %w", err)
		}
		state.ProgPins = append(state.ProgPins, pin)
	}

	for dir, err := range s.MapDirs(ctx) {
		if err != nil {
			return nil, fmt.Errorf("scan map dirs: %w", err)
		}
		state.MapDirs = append(state.MapDirs, dir)
	}

	for dir, err := range s.DispatcherDirs(ctx) {
		if err != nil {
			return nil, fmt.Errorf("scan dispatcher dirs: %w", err)
		}
		state.DispatcherDirs = append(state.DispatcherDirs, dir)
	}

	for pin, err := range s.DispatcherLinkPins(ctx) {
		if err != nil {
			return nil, fmt.Errorf("scan dispatcher link pins: %w", err)
		}
		state.DispatcherLinkPins = append(state.DispatcherLinkPins, pin)
	}

	for pin, err := range s.SharedMapPins(ctx) {
		if err != nil {
			return nil, fmt.Errorf("scan shared map pins: %w", err)
		}
		state.SharedMapPins = append(state.SharedMapPins, pin)
	}

	return state, nil
}
