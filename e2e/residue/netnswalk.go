package residue

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	bpfnetns "github.com/bpfman/bpfman/ns/netns"
)

// namedNetnsEntry is one named netns under netnsDir that needs
// visiting. Path is /run/netns/<name>; Name is the basename.
// Entries whose inode matches the current process's netns are
// excluded -- the e2e suite bind-mounts the root netns at
// /run/netns/root, and visiting it under both names would
// double-count every attachment.
type namedNetnsEntry struct {
	Path string
	Name string
}

// listDistinctNamedNetns enumerates netnsDir and returns one
// entry per named netns whose inode differs from the current
// process's netns. Returns an empty slice (no error) when the
// directory does not exist.
func listDistinctNamedNetns(netnsDir string) ([]namedNetnsEntry, error) {
	entries, err := os.ReadDir(netnsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	rootNSID, err := bpfnetns.CurrentNSID()
	if err != nil {
		// If we cannot identify the current netns we cannot
		// deduplicate, so visit everything; downstream
		// dedup-by-content (e.g. the seen[] map in the pin
		// scanner) still catches the duplicates.
		rootNSID = 0
	}

	var out []namedNetnsEntry
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(netnsDir, name)
		if rootNSID != 0 {
			id, err := bpfnetns.NSID(path)
			if err == nil && id == rootNSID {
				continue
			}
		}
		out = append(out, namedNetnsEntry{Path: path, Name: name})
	}
	return out, nil
}
