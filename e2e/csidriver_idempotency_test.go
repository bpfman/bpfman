//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestCSIDriver_NodePublishVolume_Idempotent verifies that NodePublishVolume is
// idempotent: a second call with identical arguments (same volume, same
// target) has the same effect as the first, per the CSI spec
// (spec.md:2459-2461, second-call table at spec.md:2476).
//
// Idempotency here is observable as mount stability: a duplicate publish
// must not stack an extra bpffs mount on the per-pod directory or an
// extra bind mount on the target. The test publishes the same volume
// twice and asserts each path still carries exactly one mount.
func TestCSIDriver_NodePublishVolume_Idempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "csi-idempotency-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	req := publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, "")

	// Tear down mounts even if an assertion fails mid-test. Unmount each
	// path repeatedly so any stacked mounts a failed run leaves behind
	// are fully drained.
	t.Cleanup(func() {
		cleanupCSI(d, volumeID, targetPath, podBpffs)
	})

	// First publish establishes the mount and re-pins the map.
	_, err := d.NodePublishVolume(ctx, req)
	require.NoError(t, err, "first NodePublishVolume should succeed")
	require.FileExists(t, filepath.Join(targetPath, "kprobe_stats_map"), "map should be visible at the target path after the first publish")
	require.Equal(t, 1, countMountpoints(t, podBpffs), "one bpffs mount after first publish")
	require.Equal(t, 1, countMountpoints(t, targetPath), "one bind mount after first publish")

	// Second publish with identical arguments must add no mounts.
	_, err = d.NodePublishVolume(ctx, req)
	require.NoError(t, err, "second NodePublishVolume with identical args should return OK")
	require.Equal(t, 1, countMountpoints(t, podBpffs), "second NodePublishVolume must not stack another bpffs mount (CSI idempotency)")
	require.Equal(t, 1, countMountpoints(t, targetPath), "second NodePublishVolume must not stack another bind mount (CSI idempotency)")
}

// countMountpoints returns how many entries in the caller's
// mountinfo have target as their mount point. Field index 4 is the
// mount point and is positionally stable regardless of the optional
// fields that precede the "-" separator.
func countMountpoints(t *testing.T, target string) int {
	t.Helper()
	data, err := os.ReadFile("/proc/self/mountinfo")
	require.NoError(t, err)

	n := 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && fields[4] == target {
			n++
		}
	}
	return n
}

// forceUnmountAll lazily unmounts path until it is no longer a
// mountpoint, ignoring errors. It drains any stacked mounts so a failed
// test leaves no residue behind.
func forceUnmountAll(path string) {
	for range 8 {
		if err := unix.Unmount(path, unix.MNT_DETACH); err != nil {
			return
		}
	}
}
