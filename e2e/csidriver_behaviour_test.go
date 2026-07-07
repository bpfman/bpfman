//go:build e2e

package e2e

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bpfman/bpfman"
	csidriver "github.com/bpfman/bpfman/csi"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform/ebpf"
)

// newCSIDriver loads a probe program carrying the metadata the driver
// looks up and returns a driver wired to the suite's manager and a fresh
// per-test CSI filesystem root. The map "kprobe_stats_map" is available
// under the program's MapsDir for publishing.
func newCSIDriver(t *testing.T) (d *csidriver.Driver, programName, csiRoot string) {
	t.Helper()

	ctx := context.Background()
	env := NewTestEnv(t)
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	programName = t.Name()
	programs, err := env.LoadFile(ctx, "testdata/bpf/kprobe_counter.bpf.o",
		[]manager.ProgramSpec{{
			Type: bpfman.ProgramTypeKprobe,
			Name: "kprobe_counter",
		}},
		manager.LoadOpts{
			UserMetadata: map[string]string{
				csidriver.MetadataKeyProgramName: programName,
			},
		})
	require.NoError(t, err)
	require.Len(t, programs, 1)
	t.Cleanup(func() { env.Unload(context.Background(), programs[0].Status.Kernel.ID) })

	csiRoot = filepath.Join(t.TempDir(), "csi-fs")
	d = csidriver.New(
		"csi.bpfman.io", "test", "node-test", "unix:///unused.sock",
		discard,
		csidriver.WithProgramFinder(env.Manager),
		csidriver.WithKernel(ebpf.New(ebpf.WithLogger(discard))),
		csidriver.WithCSIFsRoot(csiRoot),
	)
	return d, programName, csiRoot
}

func publishRequest(volumeID, targetPath, programName, mapList string, readonly bool, mountGroup string) *csi.NodePublishVolumeRequest {
	return &csi.NodePublishVolumeRequest{
		VolumeId:   volumeID,
		TargetPath: targetPath,
		Readonly:   readonly,
		VolumeContext: map[string]string{
			csidriver.VolumeAttrProgram: programName,
			csidriver.VolumeAttrMaps:    mapList,
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{VolumeMountGroup: mountGroup},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
	}
}

// cleanupCSI drains a published volume even if the test failed partway.
func cleanupCSI(d *csidriver.Driver, volumeID, targetPath, podBpffs string) {
	_, _ = d.NodeUnpublishVolume(context.Background(), &csi.NodeUnpublishVolumeRequest{
		VolumeId:   volumeID,
		TargetPath: targetPath,
	})
	forceUnmountAll(targetPath)
	forceUnmountAll(podBpffs)
}

// mountHasOption reports whether the topmost mount at target carries the
// given mount option (field index 5 of mountinfo, the comma-separated
// per-mount options).
func mountHasOption(t *testing.T, target, opt string) bool {
	t.Helper()
	data, err := os.ReadFile("/proc/self/mountinfo")
	require.NoError(t, err)

	found := false
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 5 && fields[4] == target {
			for o := range strings.SplitSeq(fields[5], ",") {
				if o == opt {
					found = true
				}
			}
		}
	}
	return found
}

// A publish, then unpublish, returns the host to a clean state: both the
// target and the per-pod bpffs are unmounted and their directories gone.
func TestCSIDriver_PublishUnpublishRoundTrip(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "roundtrip-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, ""))
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(targetPath, "kprobe_stats_map"))
	require.Equal(t, 1, countMountpoints(t, targetPath))

	// The per-pod bpffs is mounted with the hardening flags.
	for _, opt := range []string{"nosuid", "nodev", "noexec"} {
		require.True(t, mountHasOption(t, podBpffs, opt), "per-pod bpffs should be mounted %s", opt)
	}

	_, err = d.NodeUnpublishVolume(context.Background(), &csi.NodeUnpublishVolumeRequest{
		VolumeId:   volumeID,
		TargetPath: targetPath,
	})
	require.NoError(t, err)

	require.Equal(t, 0, countMountpoints(t, targetPath), "target unmounted")
	require.Equal(t, 0, countMountpoints(t, podBpffs), "per-pod bpffs unmounted")
	require.NoDirExists(t, targetPath, "target directory removed")
	require.NoDirExists(t, podBpffs, "per-pod directory removed")
}

// A publish that fails partway (requesting a map the program does not
// have) rolls back to a clean state: nothing left mounted, no directory
// left behind.
func TestCSIDriver_RollbackLeavesCleanState(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "rollback-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "no_such_map", false, ""))
	require.Equal(t, codes.NotFound, status.Code(err), "a map the program does not pin is a caller error, not Internal")

	require.Equal(t, 0, countMountpoints(t, podBpffs), "per-pod bpffs unmounted after rollback")
	require.NoDirExists(t, podBpffs, "per-pod directory removed after rollback")
	require.Equal(t, 0, countMountpoints(t, targetPath), "target not mounted")
}

// Publishing with an fsGroup sets group ownership and 0660 mode on the
// exposed map so an unprivileged container in that group can use it.
func TestCSIDriver_PublishFsGroup(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "fsgroup-vol"
	const gid = 12345
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, strconv.Itoa(gid)))
	require.NoError(t, err)

	var st unix.Stat_t
	require.NoError(t, unix.Stat(filepath.Join(targetPath, "kprobe_stats_map"), &st))
	require.Equal(t, uint32(gid), st.Gid, "map group ownership set to fsGroup")
	require.Equal(t, uint32(0o660), st.Mode&0o777, "map mode set to 0660")
}

// Publishing read-only makes the target bind-mount read-only.
func TestCSIDriver_PublishReadOnly(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "readonly-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", true, ""))
	require.NoError(t, err)

	require.True(t, mountHasOption(t, targetPath, "ro"), "target bind-mount should be read-only")
}

// Re-publishing an already read-only volume with identical arguments is
// idempotent too. This exercises publishMatches' Statfs read-only check.
func TestCSIDriver_RepublishReadOnlyWithSameArgs(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "republish-readonly-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	req := publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", true, "")

	_, err := d.NodePublishVolume(context.Background(), req)
	require.NoError(t, err, "first read-only publish should succeed")
	require.True(t, mountHasOption(t, targetPath, "ro"), "target bind-mount should be read-only")

	_, err = d.NodePublishVolume(context.Background(), req)
	require.NoError(t, err, "republish with identical read-only args should return OK")
	require.True(t, mountHasOption(t, targetPath, "ro"), "target bind-mount should stay read-only")
	require.Equal(t, 1, countMountpoints(t, targetPath), "read-only republish must not stack the target mount")
	require.Equal(t, 1, countMountpoints(t, podBpffs), "read-only republish must not stack the per-pod mount")
}

// Re-publishing at the same target returns OK when the arguments match
// and ALREADY_EXISTS when they differ (the CSI second-call contract).
func TestCSIDriver_RepublishWithDifferentArgs(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "republish-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	// Initial publish: one map, read-write.
	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, ""))
	require.NoError(t, err)

	// Identical arguments -> OK (idempotent).
	_, err = d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, ""))
	require.NoError(t, err, "republish with identical args should return OK")

	// Different map set -> ALREADY_EXISTS.
	_, err = d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map,other_map", false, ""))
	require.Equal(t, codes.AlreadyExists, status.Code(err), "republish with a different map set")

	// Different read-only flag -> ALREADY_EXISTS.
	_, err = d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", true, ""))
	require.Equal(t, codes.AlreadyExists, status.Code(err), "republish with a different read-only flag")
}

// Concurrent duplicate publishes for one volume must not stack mounts: the
// per-volume lock serialises them, so exactly one establishes the mount and
// the rest return OK (idempotent) or ABORTED -- never an error -- and the
// target ends up with a single mount.
func TestCSIDriver_ConcurrentPublishDoesNotStack(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "concurrent-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	req := publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, "")

	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = d.NodePublishVolume(context.Background(), req)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			require.Equal(t, codes.Aborted, status.Code(err), "publish %d returned an unexpected error", i)
		}
	}
	require.Equal(t, 1, countMountpoints(t, targetPath), "one bind mount after concurrent publishes")
	require.Equal(t, 1, countMountpoints(t, podBpffs), "one bpffs mount after concurrent publishes")
}

// Publishing onto a target that already carries a non-bpf mount is
// refused, rather than the driver stacking its bpffs on top of something
// it does not own.
func TestCSIDriver_RejectsForeignTargetMount(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "foreign-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	require.NoError(t, os.MkdirAll(targetPath, 0o755))
	require.NoError(t, unix.Mount("tmpfs", targetPath, "tmpfs", 0, ""), "set up a foreign mount")
	t.Cleanup(func() {
		forceUnmountAll(targetPath)
		cleanupCSI(d, volumeID, targetPath, podBpffs)
	})

	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, ""))
	require.Equal(t, codes.FailedPrecondition, status.Code(err), "should refuse to mount over a foreign mount")
}

// Repeated publish/unpublish cycles leave no residue, a failed publish
// rolls back so a retry succeeds, and a repeated unpublish stays OK.
func TestCSIDriver_RepeatedCycles(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "cycles-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	t.Cleanup(func() { cleanupCSI(d, volumeID, targetPath, podBpffs) })

	good := publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, "")
	unpub := &csi.NodeUnpublishVolumeRequest{VolumeId: volumeID, TargetPath: targetPath}

	for i := range 3 {
		_, err := d.NodePublishVolume(context.Background(), good)
		require.NoError(t, err, "publish cycle %d", i)
		require.Equal(t, 1, countMountpoints(t, targetPath), "one mount in cycle %d", i)

		_, err = d.NodeUnpublishVolume(context.Background(), unpub)
		require.NoError(t, err, "unpublish cycle %d", i)
		require.Equal(t, 0, countMountpoints(t, targetPath), "target unmounted in cycle %d", i)
		require.Equal(t, 0, countMountpoints(t, podBpffs), "per-pod unmounted in cycle %d", i)
	}

	// A failed publish (missing map) must roll back, and a good retry must
	// still succeed.
	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "no_such_map", false, ""))
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, 0, countMountpoints(t, podBpffs), "clean after a failed publish")

	_, err = d.NodePublishVolume(context.Background(), good)
	require.NoError(t, err, "retry after a failed publish should succeed")
	require.Equal(t, 1, countMountpoints(t, targetPath))

	// Unpublish is idempotent.
	_, err = d.NodeUnpublishVolume(context.Background(), unpub)
	require.NoError(t, err)
	_, err = d.NodeUnpublishVolume(context.Background(), unpub)
	require.NoError(t, err, "second unpublish should also return OK")
}

// A retry after a crash that left the per-pod bpffs mounted but the target
// unbound must replace the stale mount, not stack a second bpffs over it.
func TestCSIDriver_RecoversStalePerPodMount(t *testing.T) {
	t.Parallel()

	d, programName, csiRoot := newCSIDriver(t)
	const volumeID = "stale-vol"
	targetPath := filepath.Join(t.TempDir(), "target")
	podBpffs := filepath.Join(csiRoot, volumeID)
	// Simulate a crashed prior attempt: per-pod bpffs mounted, target unbound.
	require.NoError(t, os.MkdirAll(podBpffs, 0o750))
	require.NoError(t, unix.Mount("bpf", podBpffs, "bpf", 0, ""), "set up a stale per-pod mount")
	t.Cleanup(func() {
		forceUnmountAll(podBpffs)
		cleanupCSI(d, volumeID, targetPath, podBpffs)
	})

	_, err := d.NodePublishVolume(context.Background(),
		publishRequest(volumeID, targetPath, programName, "kprobe_stats_map", false, ""))
	require.NoError(t, err)
	require.Equal(t, 1, countMountpoints(t, podBpffs), "stale per-pod bpffs replaced, not stacked")
	require.Equal(t, 1, countMountpoints(t, targetPath))
}
