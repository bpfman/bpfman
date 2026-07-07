package residue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteNetns_RemovesStaleMarkerFile pins the EINVAL
// fallback in DeleteNetns.Apply: when /run/netns/X exists as a
// plain file (no longer a bind-mount), the unmount step in the
// vendored vishvananda/netns helper returns EINVAL and bails
// before reaching the unlink, so the marker file lingers
// forever. Production triggers include reboot, OOM kill, manual
// `umount /run/netns/X` that leaves the marker, or any other
// case where the netns went away but iproute2's accounting file
// did not. Apply must tolerate EINVAL on the unmount and still
// remove the file.
func TestDeleteNetns_RemovesStaleMarkerFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := "B24817bdd1c8aN"
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, nil, 0o000))

	action := DeleteNetns{Name: name, Dir: dir}
	require.NoError(t, action.Apply())

	_, err := os.Stat(path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist),
		"expected the stale marker to be removed, got %v", err)
}

// TestDeleteNetns_ReportsMissingFile distinguishes the
// stale-marker fallback from "the marker was never there".
// A delete against a name with no marker should not silently
// succeed (that would mask scan bugs); the surfaced error
// gives the operator a clear signal that the plan was built
// against a different snapshot.
func TestDeleteNetns_ReportsMissingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	action := DeleteNetns{Name: "absent", Dir: dir}
	err := action.Apply()
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist),
		"expected ErrNotExist for an absent marker, got %v", err)
}
