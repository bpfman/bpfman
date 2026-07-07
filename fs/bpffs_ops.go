package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	bpfman "github.com/bpfman/bpfman"
)

// removePinFile removes a file under the bpffs mount.
//
// This is intentionally unexported. External callers must use typed
// deletion methods (RemoveProgPin, RemoveDispatcherLinkPin, etc.) to
// avoid "delete arbitrary file" foot-guns.
func (b BPFFS) removePinFile(path string) error {
	path, err := b.cleanUnderMount(path)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pin %s: %w", path, err)
	}
	return nil
}

// removeDir removes a directory tree under the bpffs mount.
//
// This is intentionally unexported. External callers must use typed
// deletion methods to ensure we only remove directories we own and
// recognise.
func (b BPFFS) removeDir(path string) error {
	path, err := b.cleanUnderMount(path)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove dir %s: %w", path, err)
	}
	return nil
}

// RemoveProgPin removes a bpfman program pin of the form:
//
//	{bpffs}/prog_{program_id}
//
// The suffix must be a valid numeric program ID.
func (b BPFFS) RemoveProgPin(p bpfman.ProgPinPath) error {
	path, err := b.cleanUnderMount(p.String())
	if err != nil {
		return err
	}

	if filepath.Dir(path) != b.MountPoint() {
		return fmt.Errorf("prog pin not under mount root: %s", path)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "prog_") {
		return fmt.Errorf("prog pin has unexpected name: %s", path)
	}
	// Validate suffix is numeric.
	suffix := strings.TrimPrefix(base, "prog_")
	if _, err := strconv.ParseUint(suffix, 10, 32); err != nil {
		return fmt.Errorf("prog pin has unexpected name: %s", path)
	}
	return b.removePinFile(path)
}

// RemoveMapDir removes a map directory of the form:
//
//	{bpffs}/maps/{program_id}
func (b BPFFS) RemoveMapDir(p bpfman.MapDir) error {
	return b.removeNumericChildDir(b.Maps(), p.String(), "map dir")
}

// RemoveDispatcherProgPin removes a dispatcher program pin.
//
// The dispatcher program pin is located at:
//
//	{bpffs}/{type}/dispatcher_{nsid}_{ifindex}_{revision}/dispatcher
//
// We validate both the revision directory name pattern and the pin
// filename (must be "dispatcher").
func (b BPFFS) RemoveDispatcherProgPin(p bpfman.ProgPinPath) error {
	path, err := b.cleanUnderMount(p.String())
	if err != nil {
		return err
	}

	if filepath.Base(path) != "dispatcher" {
		return fmt.Errorf("dispatcher prog pin has unexpected name: %s", path)
	}
	revDir := filepath.Dir(path)
	if err := b.validateDispatcherRevDirPath(revDir); err != nil {
		return err
	}
	return b.removePinFile(path)
}

// RemoveDispatcherRevDir removes a dispatcher revision directory of
// the form:
//
//	{bpffs}/{type}/dispatcher_{nsid}_{ifindex}_{revision}
//
// The directory is owned by bpfman and safe to delete when deemed
// stale or orphaned.
func (b BPFFS) RemoveDispatcherRevDir(p bpfman.DispatcherRevDir) error {
	path, err := b.cleanUnderMount(p.String())
	if err != nil {
		return err
	}

	if err := b.validateDispatcherRevDirPath(path); err != nil {
		return err
	}
	return b.removeDir(path)
}

// RemoveDispatcherLinkPin removes a dispatcher link pin of the form:
//
//	{bpffs}/{type}/dispatcher_{nsid}_{ifindex}_link
func (b BPFFS) RemoveDispatcherLinkPin(p bpfman.LinkPath) error {
	path, err := b.cleanUnderMount(p.String())
	if err != nil {
		return err
	}

	parent := filepath.Dir(path)
	if !b.isDispatcherTypeDir(parent) {
		return fmt.Errorf("dispatcher link not under type dir: %s", path)
	}

	base := filepath.Base(path)
	if !strings.HasPrefix(base, "dispatcher_") || !strings.HasSuffix(base, "_link") {
		return fmt.Errorf("dispatcher link has unexpected name: %s", path)
	}

	trim := strings.TrimSuffix(strings.TrimPrefix(base, "dispatcher_"), "_link")
	parts := strings.Split(trim, "_")
	if len(parts) != 2 {
		return fmt.Errorf("dispatcher link has unexpected name: %s", path)
	}
	for _, p := range parts {
		if _, err := strconv.ParseUint(p, 10, 64); err != nil {
			return fmt.Errorf("dispatcher link has unexpected name: %s", path)
		}
	}

	return b.removePinFile(path)
}

// RemoveSharedMapPin removes a shared map pin file under the
// {bpffs}/shared/ directory.
func (b BPFFS) RemoveSharedMapPin(p bpfman.MapPinPath) error {
	path, err := b.cleanUnderMount(string(p))
	if err != nil {
		return err
	}

	if filepath.Dir(path) != b.SharedMapPinDir() {
		return fmt.Errorf("shared map pin not inside shared directory: %s", path)
	}
	return b.removePinFile(path)
}

// removeNumericChildDir removes a directory that is a direct child of
// parent and has a numeric name.
func (b BPFFS) removeNumericChildDir(parent, path, what string) error {
	path, err := b.cleanUnderMount(path)
	if err != nil {
		return err
	}

	if filepath.Dir(path) != parent {
		return fmt.Errorf("%s not directly under %s: %s", what, parent, path)
	}
	if _, err := strconv.ParseUint(filepath.Base(path), 10, 32); err != nil {
		return fmt.Errorf("%s has non-numeric name: %s", what, path)
	}
	return b.removeDir(path)
}

// isDispatcherTypeDir returns true if path is one of the dispatcher
// type directories (xdp, tc-ingress, tc-egress).
func (b BPFFS) isDispatcherTypeDir(path string) bool {
	switch path {
	case b.XDP(), b.TCIngress(), b.TCEgress():
		return true
	default:
		return false
	}
}

// validateDispatcherRevDirPath validates a dispatcher revision
// directory path has the expected structure:
//
//	{bpffs}/{type}/dispatcher_{nsid}_{ifindex}_{revision}
func (b BPFFS) validateDispatcherRevDirPath(path string) error {
	parent := filepath.Dir(path)
	if !b.isDispatcherTypeDir(parent) {
		return fmt.Errorf("dispatcher dir not under type dir: %s", path)
	}

	base := filepath.Base(path)
	if !strings.HasPrefix(base, "dispatcher_") {
		return fmt.Errorf("dispatcher dir has unexpected name: %s", path)
	}

	// Validate suffixes are numeric to avoid rm -rf surprises.
	// Expected: dispatcher_NSID_IFINDEX_REV.
	parts := strings.Split(strings.TrimPrefix(base, "dispatcher_"), "_")
	if len(parts) != 3 {
		return fmt.Errorf("dispatcher dir has unexpected name: %s", path)
	}
	for _, p := range parts {
		if _, err := strconv.ParseUint(p, 10, 64); err != nil {
			return fmt.Errorf("dispatcher dir has unexpected name: %s", path)
		}
	}

	return nil
}

// cleanUnderMount validates and cleans a path, ensuring it is under
// the bpffs mount point.
func (b BPFFS) cleanUnderMount(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	mount := filepath.Clean(b.MountPoint())
	clean := filepath.Clean(path)

	// Refuse to delete the mount root itself.
	if clean == mount {
		return "", fmt.Errorf("refusing to remove bpffs mount root: %s", clean)
	}

	// Ensure the path is within the mount.
	prefix := mount + string(os.PathSeparator)
	if !strings.HasPrefix(clean, prefix) {
		return "", fmt.Errorf("path escapes bpffs mount: %s", clean)
	}

	return clean, nil
}
