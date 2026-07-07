package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	fsruntime "github.com/bpfman/bpfman/fs/runtime"
	"github.com/bpfman/bpfman/platform"
)

// mapsMode is the permission mode for CSI-exposed maps (owner+group read/write).
const mapsMode = 0o0660

// CSI volume attribute keys matching upstream Rust bpfman.
const (
	// VolumeAttrProgram specifies the program name to look up.
	// This is matched against the bpfman.io/ProgramName metadata.
	VolumeAttrProgram = "csi.bpfman.io/program"

	// VolumeAttrMaps specifies a comma-separated list of map names to expose.
	VolumeAttrMaps = "csi.bpfman.io/maps"

	// MetadataKeyProgramName is the metadata key used to identify programs.
	MetadataKeyProgramName = "bpfman.io/ProgramName"
)

// NodeGetInfo returns information about this node.
func (d *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	d.logger.Debug("NodeGetInfo", "method", "Node.NodeGetInfo")

	resp := &csi.NodeGetInfoResponse{
		NodeId: d.nodeID,
	}

	d.logger.Info("NodeGetInfo response", "method", "Node.NodeGetInfo", "nodeID", resp.NodeId)

	return resp, nil
}

// NodeGetCapabilities returns the capabilities of this node plugin.
func (d *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	d.logger.Debug("NodeGetCapabilities", "method", "Node.NodeGetCapabilities")

	resp := &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_VOLUME_MOUNT_GROUP,
					},
				},
			},
		},
	}

	d.logger.Info("NodeGetCapabilities response", "method", "Node.NodeGetCapabilities", "capabilities", len(resp.Capabilities))

	return resp, nil
}

// NodePublishVolume mounts BPF maps to the target path.
//
// The driver looks up programs by csi.bpfman.io/program metadata,
// re-pins requested maps to a per-pod bpffs, and bind-mounts
// that bpffs to the container.
func (d *Driver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	volumeContext := req.GetVolumeContext()
	readonly := req.GetReadonly()

	// Extract fsGroup from volume capability if present.
	// This allows unprivileged containers to access the maps.
	var fsGroup int = -1
	if volCap := req.GetVolumeCapability(); volCap != nil {
		if mount := volCap.GetMount(); mount != nil {
			if groupStr := mount.GetVolumeMountGroup(); groupStr != "" {
				if gid, err := strconv.Atoi(groupStr); err == nil {
					fsGroup = gid
				}
			}
		}
	}

	d.logger.Info("NodePublishVolume request", "method", "Node.NodePublishVolume", "volumeID", volumeID, "targetPath", targetPath, "volumeContext", volumeContext, "readonly", readonly, "fsGroup", fsGroup, "accessMode", req.GetVolumeCapability().GetAccessMode().GetMode().String())

	if err := checkVolumeID(volumeID); err != nil {
		return nil, err
	}
	if err := checkTargetPath(targetPath); err != nil {
		return nil, err
	}
	if err := validateVolumeCapability(req.GetVolumeCapability()); err != nil {
		return nil, err
	}

	programName := volumeContext[VolumeAttrProgram]
	requestedMaps := parseMapNames(volumeContext[VolumeAttrMaps])

	if programName == "" || len(requestedMaps) == 0 {
		return nil, status.Error(codes.InvalidArgument, "csi.bpfman.io/program and at least one csi.bpfman.io/maps are required")
	}
	if err := validateMapNames(requestedMaps); err != nil {
		return nil, err
	}

	// enforceReadOnly gates a hardening check that rejects any
	// publish whose volume is not read-only so a workload pod
	// cannot tamper with the resource it is handed. It is off
	// because bpfman's example pods make the *container* mount
	// read-only (volumeMounts[].readOnly), not the CSI source
	// (volumes[].csi.readOnly), so req.Readonly is false and
	// enforcing it would reject every current example.
	//
	// To enable: flip this to true and set readOnly: true on the
	// csi volume source in the examples (and the e2e
	// publishRequest); see
	// https://github.com/bpfman/bpfman/issues/1670. A read-only
	// mount restricts only the map pins, not the map data, so
	// userspace can still read and write the maps it is handed.
	const enforceReadOnly = false
	if enforceReadOnly && !req.GetReadonly() {
		return nil, status.Error(codes.InvalidArgument, "volume must be published read-only (set csi.readOnly: true)")
	}

	// Serialise per-volume work: the CO may lose state and issue concurrent
	// calls for the same volume, and the idempotency check below plus the
	// mount and re-pin work are not safe to run twice at once.
	if !d.locks.TryAcquire(volumeID) {
		return nil, status.Errorf(codes.Aborted, "an operation is already in progress for volume %q", volumeID)
	}
	defer d.locks.Release(volumeID)

	// NodePublishVolume MUST be idempotent: if the volume is already
	// published at targetPath, reply OK without repeating the mount and
	// re-pin work. Repeating it stacks a second bpffs mount on the per-pod
	// directory and a second bind mount on the target, which leak because
	// NodeUnpublishVolume removes only one mount per path. kubelet retries
	// (and CO state loss) make duplicate calls routine.
	//
	// The spec also requires ALREADY_EXISTS when the same target is
	// re-published with different arguments. We compare observable state --
	// the maps at the target and the mount's read-only flag -- so the check
	// needs no remembered request and survives a restart. It cannot see the
	// program name or capability (those are not recorded at the target), but
	// the CO allocates a unique target path per (pod, volume), so a different
	// volume or program does not collide at one target; the reference drivers
	// (spiffe-csi, secrets-store) live with the same limitation.
	fsType, mounted, err := fsruntime.MountpointFsType(fsruntime.DefaultMountInfoPath, targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check target path mount state: %v", err)
	}
	if mounted && fsType != "bpf" {
		// Something other than our bpffs owns the target. Do not mount over
		// it; that is not safe and is not our volume to reclaim.
		return nil, status.Errorf(codes.FailedPrecondition, "target path %q is already mounted with filesystem %q", targetPath, fsType)
	}
	if mounted {
		same, err := publishMatches(targetPath, requestedMaps, readonly)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "compare published volume at %q: %v", targetPath, err)
		}
		if !same {
			return nil, status.Errorf(codes.AlreadyExists, "volume %q already published at %q with different arguments", volumeID, targetPath)
		}
		d.logger.Info("NodePublishVolume already published; returning OK", "method", "Node.NodePublishVolume", "volumeID", volumeID, "targetPath", targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if d.programFinder == nil || d.kernel == nil {
		return nil, status.Error(codes.FailedPrecondition, "bpfman integration not configured; programFinder and kernel required")
	}

	// 1. Find program by metadata (reconciled with kernel state)
	metadata, _, err := d.programFinder.FindLoadedProgramByMetadata(ctx, MetadataKeyProgramName, programName)
	if err != nil {
		// Return appropriate gRPC code based on error type.
		// NotFound is expected during reconciliation -- the CSI
		// driver may ask before the operator has loaded the program.
		switch {
		case errors.Is(err, platform.ErrRecordNotFound):
			d.logger.Warn("program not yet loaded", "programName", programName, "error", err)
			return nil, status.Errorf(codes.NotFound, "program %q not found", programName)
		default:
			d.logger.Error("failed to find program", "programName", programName, "error", err)
			return nil, status.Errorf(codes.Internal, "failed to find program %q: %v", programName, err)
		}
	}

	// 2. Get the maps directory from the program (may differ from PinPath if sharing maps)
	mapPinPath := metadata.Handles.MapsDir.String()
	if mapPinPath == "" {
		return nil, status.Errorf(codes.Internal, "program %q has no map pin path", programName)
	}

	d.logger.Info("found program", "programName", programName, "mapPinPath", mapPinPath)

	podBpffs := filepath.Join(d.csiFsRoot, volumeID)

	// From here we change host state; on any failure, unwind it. The target
	// directory is not removed -- the CO owns it -- so only the per-pod
	// bpffs and the bind mount are ours to undo.
	committed := false
	defer func() {
		if committed {
			return
		}
		d.logger.Warn("NodePublishVolume failed; rolling back partial mount", "volumeID", volumeID, "targetPath", targetPath, "podBpffs", podBpffs)
		if err := unmountIfMounted(targetPath); err != nil {
			d.logger.Warn("rollback: unmount target", "path", targetPath, "error", err)
		}
		if err := cleanupMount(podBpffs); err != nil {
			d.logger.Warn("rollback: clean per-pod bpffs", "path", podBpffs, "error", err)
		}
	}()

	// 3. Create and mount the per-pod bpffs. A previous attempt may have
	// crashed after mounting it but before binding the target, in which case
	// the target check above missed it; clean any stale mount so we rebuild
	// rather than stacking a second bpffs on top.
	if _, stale, err := fsruntime.MountpointFsType(fsruntime.DefaultMountInfoPath, podBpffs); err != nil {
		return nil, status.Errorf(codes.Internal, "check per-pod mount state: %v", err)
	} else if stale {
		if err := cleanupMount(podBpffs); err != nil {
			return nil, status.Errorf(codes.Internal, "clean stale per-pod bpffs %q: %v", podBpffs, err)
		}
	}
	if err := os.MkdirAll(podBpffs, 0o750); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create bpffs dir %q: %v", podBpffs, err)
	}
	if err := mountBpffs(podBpffs); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to mount bpffs at %q: %v", podBpffs, err)
	}

	// 4. Set group ownership on the bpffs directory if fsGroup is specified.
	// We advertise VOLUME_MOUNT_GROUP, so kubelet delegates fsGroup to us and
	// does not chown the volume itself. If we cannot apply it the pod would
	// silently be unable to reach its maps, so fail rather than half-succeed.
	if fsGroup >= 0 {
		if err := unix.Chown(podBpffs, -1, fsGroup); err != nil {
			return nil, status.Errorf(codes.Internal, "set group ownership on %q: %v", podBpffs, err)
		}
	}

	// 5. Re-pin each requested map into the per-pod bpffs.
	for _, mapName := range requestedMaps {
		srcPath := filepath.Join(mapPinPath, mapName)
		dstPath := filepath.Join(podBpffs, mapName)

		// A map the caller named but the program does not pin is a caller
		// error, not an internal fault: report NotFound so the pod event
		// names the bad map rather than implying a driver bug. A found
		// program has its maps pinned, so a missing source is terminal, not
		// a transient the caller should expect to clear.
		if _, err := os.Stat(srcPath); err != nil {
			if os.IsNotExist(err) {
				return nil, status.Errorf(codes.NotFound, "map %q is not published by program %q", mapName, programName)
			}
			return nil, status.Errorf(codes.Internal, "stat map pin %q: %v", srcPath, err)
		}

		d.logger.Debug("re-pinning map", "map", mapName, "src", srcPath, "dst", dstPath)

		if err := d.kernel.RepinMap(ctx, srcPath, dstPath); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to re-pin map %q: %v", mapName, err)
		}

		// Set group ownership and mode so an unprivileged container in the
		// fsGroup can access the map. As with the directory above, a failure
		// here leaves the pod unable to use the map it asked for, so it is
		// fatal rather than a warning.
		if fsGroup >= 0 {
			if err := unix.Chown(dstPath, -1, fsGroup); err != nil {
				return nil, status.Errorf(codes.Internal, "set group ownership on map %q: %v", dstPath, err)
			}
			if err := os.Chmod(dstPath, mapsMode); err != nil {
				return nil, status.Errorf(codes.Internal, "set mode on map %q: %v", dstPath, err)
			}
		}
	}

	// 6. Create the target directory and bind-mount the per-pod bpffs onto it.
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create target path: %v", err)
	}
	if err := unix.Mount(podBpffs, targetPath, "", unix.MS_BIND, ""); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to bind-mount %q to %q: %v", podBpffs, targetPath, err)
	}

	// MS_RDONLY is ignored on the initial bind; a read-only bind mount needs
	// a second remount pass to take effect.
	if readonly {
		if err := unix.Mount("", targetPath, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to remount %q read-only: %v", targetPath, err)
		}
	}

	committed = true

	d.logger.Info("NodePublishVolume succeeded", "method", "Node.NodePublishVolume", "volumeID", volumeID, "programName", programName, "maps", requestedMaps, "podBpffs", podBpffs, "targetPath", targetPath, "readonly", readonly, "fsGroup", fsGroup)

	return &csi.NodePublishVolumeResponse{}, nil
}

// publishMatches reports whether the volume already published at
// targetPath matches the requested maps and read-only flag. It compares
// observable state -- the map pins at the target and the mount's
// read-only flag -- so it does not depend on remembering the original
// request and survives a driver restart.
func publishMatches(targetPath string, requestedMaps []string, readonly bool) (bool, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(targetPath, &st); err != nil {
		return false, err
	}
	if (st.Flags&unix.ST_RDONLY != 0) != readonly {
		return false, nil
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return false, err
	}
	published := make([]string, 0, len(entries))
	for _, e := range entries {
		// bpffs auto-creates root-level introspection files (maps.debug,
		// progs.debug) on newer kernels. Those dentries are reserved by the
		// filesystem -- no map pin can take their names -- so exclude them
		// from the published set; otherwise an idempotent re-publish reads as
		// a different map set and is wrongly rejected with ALREADY_EXISTS.
		switch e.Name() {
		case "maps.debug", "progs.debug":
			continue
		}
		published = append(published, e.Name())
	}
	return equalStringSets(published, requestedMaps), nil
}

// parseMapNames splits a comma-separated map list, trimming whitespace,
// dropping empty entries, and de-duplicating while preserving first-seen
// order. Duplicates would otherwise re-pin the same map twice (the second
// pin failing) and skew the idempotency comparison against the target.
func parseMapNames(s string) []string {
	var names []string
	seen := make(map[string]struct{})
	for name := range strings.SplitSeq(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// equalStringSets reports whether a and b contain the same elements,
// ignoring order. It does not mutate its inputs.
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func checkPathElement(kind, value string) error {
	if strings.ContainsRune(value, '/') || value == "." || value == ".." {
		return status.Errorf(codes.InvalidArgument, "invalid %s %q: must be a single path element", kind, value)
	}
	return nil
}

// checkVolumeID validates the volume id. Besides being required, it becomes
// a directory name under csiFsRoot, so it must be a single clean path
// element -- never a separator or traversal that could escape the root and
// have cleanupMount unmount or remove something unrelated.
func checkVolumeID(volumeID string) error {
	if volumeID == "" {
		return status.Error(codes.InvalidArgument, "volume ID is required")
	}
	return checkPathElement("volume ID", volumeID)
}

// checkTargetPath validates CSI's target_path contract before the driver
// creates, mounts, unmounts, or removes it.
func checkTargetPath(targetPath string) error {
	if targetPath == "" {
		return status.Error(codes.InvalidArgument, "target path is required")
	}
	if !filepath.IsAbs(targetPath) {
		return status.Errorf(codes.InvalidArgument, "target path %q must be absolute", targetPath)
	}
	if filepath.Clean(targetPath) == string(filepath.Separator) {
		return status.Error(codes.InvalidArgument, "target path must not be the filesystem root")
	}
	return nil
}

func validateMapNames(names []string) error {
	for _, name := range names {
		if err := checkPathElement("map name", name); err != nil {
			return err
		}
	}
	return nil
}

// validateVolumeCapability rejects capabilities the driver cannot honour.
// The driver supports only a plain bind mount of a per-pod bpffs, so it
// rejects block volumes, a foreign fsType, and mount flags it would
// otherwise silently ignore, and it requires an access mode. This mirrors
// what spiffe-csi and the host-path reference driver enforce.
func validateVolumeCapability(vc *csi.VolumeCapability) error {
	if vc == nil {
		return status.Error(codes.InvalidArgument, "volume capability is required")
	}
	if vc.GetBlock() != nil {
		return status.Error(codes.InvalidArgument, "block volumes are not supported")
	}
	mount := vc.GetMount()
	if mount == nil {
		return status.Error(codes.InvalidArgument, "volume capability must be a mount")
	}
	if mount.GetFsType() != "" {
		return status.Error(codes.InvalidArgument, "fsType is not supported")
	}
	if len(mount.GetMountFlags()) != 0 {
		return status.Error(codes.InvalidArgument, "mount flags are not supported")
	}
	// The driver exposes maps as a node-local per-pod bind mount keyed by
	// volume id, so it serves one target per volume: read-only is governed by
	// the readonly flag, and it implements neither multi-node nor
	// multi-writer (same volume, multiple targets) semantics. Accept only the
	// single-writer single-node modes -- which is what kubelet sets for an
	// ephemeral inline volume -- and reject the rest, including UNKNOWN,
	// reader-only, and SINGLE_NODE_MULTI_WRITER.
	switch mode := vc.GetAccessMode().GetMode(); mode {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER:
		return nil
	default:
		return status.Errorf(codes.InvalidArgument, "unsupported access mode %s", mode)
	}
}

// bpffsMagic is the magic number for bpffs (from statfs).
const bpffsMagic = 0xcafe4a11

// mountBpffs mounts a bpffs filesystem at the given path.
func mountBpffs(path string) error {
	// A bpffs only ever holds pinned BPF objects, so these flags are largely
	// belt-and-braces, but they match the reference driver and keep a mount
	// exposed into a container from carrying suid, device, or exec semantics.
	flags := uintptr(unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC | unix.MS_RELATIME)
	if err := unix.Mount("bpf", path, "bpf", flags, ""); err != nil {
		return err
	}

	// Verify the mount is actually bpffs - catches misconfiguration early
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		unix.Unmount(path, 0)
		return err
	}
	if stat.Type != bpffsMagic {
		unix.Unmount(path, 0)
		return unix.EINVAL
	}

	return nil
}

// unmountIfMounted unmounts path, tolerating a path that is not a
// mountpoint or does not exist.
func unmountIfMounted(path string) error {
	if err := unix.Unmount(path, 0); err != nil {
		if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	return nil
}

// cleanupMount unmounts path and removes its directory, tolerating a path
// that is not mounted or does not exist. It mirrors the behaviour of
// k8s.io/mount-utils CleanupMountPoint without taking the dependency.
func cleanupMount(path string) error {
	if err := unmountIfMounted(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// NodeUnpublishVolume unmounts the volume from the target path.
// It also cleans up the per-pod bpffs.
func (d *Driver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()

	d.logger.Info("NodeUnpublishVolume request", "method", "Node.NodeUnpublishVolume", "volumeID", volumeID, "targetPath", targetPath)

	if err := checkVolumeID(volumeID); err != nil {
		return nil, err
	}
	if err := checkTargetPath(targetPath); err != nil {
		return nil, err
	}

	// Serialise against a concurrent publish/unpublish for the same volume.
	if !d.locks.TryAcquire(volumeID) {
		return nil, status.Errorf(codes.Aborted, "an operation is already in progress for volume %q", volumeID)
	}
	defer d.locks.Release(volumeID)

	// Unmount the bind-mount and remove the target directory. Both are
	// idempotent, so a repeated unpublish still returns OK.
	if err := cleanupMount(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to clean target path %q: %v", targetPath, err)
	}

	// Tear down the per-pod bpffs (best-effort).
	podBpffs := filepath.Join(d.csiFsRoot, volumeID)
	if err := cleanupMount(podBpffs); err != nil {
		d.logger.Warn("failed to clean per-pod bpffs", "path", podBpffs, "error", err)
	}

	d.logger.Info("NodeUnpublishVolume succeeded", "method", "Node.NodeUnpublishVolume", "volumeID", volumeID, "targetPath", targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeStageVolume is called before NodePublishVolume if staging is advertised.
func (d *Driver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	d.logger.Warn("NodeStageVolume called but not implemented", "method", "Node.NodeStageVolume", "volumeID", req.GetVolumeId())
	return nil, status.Error(codes.Unimplemented, "NodeStageVolume not supported")
}

// NodeUnstageVolume is the counterpart to NodeStageVolume.
func (d *Driver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	d.logger.Warn("NodeUnstageVolume called but not implemented", "method", "Node.NodeUnstageVolume", "volumeID", req.GetVolumeId())
	return nil, status.Error(codes.Unimplemented, "NodeUnstageVolume not supported")
}

// NodeGetVolumeStats returns statistics about a volume.
func (d *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	d.logger.Warn("NodeGetVolumeStats called but not implemented", "method", "Node.NodeGetVolumeStats", "volumeID", req.GetVolumeId())
	return nil, status.Error(codes.Unimplemented, "NodeGetVolumeStats not supported")
}

// NodeExpandVolume expands a volume on the node.
func (d *Driver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	d.logger.Warn("NodeExpandVolume called but not implemented", "method", "Node.NodeExpandVolume", "volumeID", req.GetVolumeId())
	return nil, status.Error(codes.Unimplemented, "NodeExpandVolume not supported")
}
