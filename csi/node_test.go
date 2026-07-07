package driver

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The request-validation guards run before any I/O, so they are exercised
// with a bare Driver (just a logger) and no mount/program/kernel
// dependencies. These are pure checks: there is no kernel behaviour for a
// fake to misrepresent.
func TestNodePublishVolume_RejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	validCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
	}
	validCtx := map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: "map_a"}

	mountCap := func(m *csi.VolumeCapability_MountVolume) *csi.VolumeCapability {
		return &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{Mount: m},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		}
	}

	tests := []struct {
		name string
		req  *csi.NodePublishVolumeRequest
	}{
		{
			name: "missing volume id",
			req:  &csi.NodePublishVolumeRequest{TargetPath: "/target", VolumeCapability: validCap, VolumeContext: validCtx},
		},
		{
			name: "missing target path",
			req:  &csi.NodePublishVolumeRequest{VolumeId: "vol1", VolumeCapability: validCap, VolumeContext: validCtx},
		},
		{
			name: "relative target path",
			req:  &csi.NodePublishVolumeRequest{VolumeId: "vol1", TargetPath: "target", VolumeCapability: validCap, VolumeContext: validCtx},
		},
		{
			name: "root target path",
			req:  &csi.NodePublishVolumeRequest{VolumeId: "vol1", TargetPath: "/", VolumeCapability: validCap, VolumeContext: validCtx},
		},
		{
			name: "missing volume capability",
			req:  &csi.NodePublishVolumeRequest{VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx},
		},
		{
			name: "missing program",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrMaps: "map_a"},
			},
		},
		{
			name: "missing maps",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrProgram: "prog"},
			},
		},
		{
			name: "maps present but empty",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: " , ,"},
			},
		},
		{
			name: "map name with path traversal",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: "../escape"},
			},
		},
		{
			name: "map name with subdirectory",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: "sub/map"},
			},
		},
		{
			name: "map name is current directory",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: "."},
			},
		},
		{
			name: "map name is absolute path",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeCapability: validCap,
				VolumeContext: map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: "/escape"},
			},
		},
		{
			name: "block volume",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
				},
			},
		},
		{
			name: "foreign fsType",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: mountCap(&csi.VolumeCapability_MountVolume{FsType: "ext4"}),
			},
		},
		{
			name: "mount flags",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: mountCap(&csi.VolumeCapability_MountVolume{MountFlags: []string{"noexec"}}),
			},
		},
		{
			name: "missing access mode",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
				},
			},
		},
		{
			name: "reader-only access mode",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY},
				},
			},
		},
		{
			name: "multi-node access mode",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
				},
			},
		},
		{
			name: "single-node multi-writer access mode",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "vol1", TargetPath: "/target", VolumeContext: validCtx,
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER},
				},
			},
		},
		{
			name: "volume id with path traversal",
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "../escape", TargetPath: "/target", VolumeCapability: validCap, VolumeContext: validCtx,
			},
		},
	}

	d := &Driver{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), locks: newVolumeLocks()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := d.NodePublishVolume(context.Background(), tt.req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestNodeUnpublishVolume_RejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *csi.NodeUnpublishVolumeRequest
	}{
		{
			name: "missing volume id",
			req:  &csi.NodeUnpublishVolumeRequest{TargetPath: "/target"},
		},
		{
			name: "volume id with path traversal",
			req:  &csi.NodeUnpublishVolumeRequest{VolumeId: "../escape", TargetPath: "/target"},
		},
		{
			name: "missing target path",
			req:  &csi.NodeUnpublishVolumeRequest{VolumeId: "vol1"},
		},
		{
			name: "relative target path",
			req:  &csi.NodeUnpublishVolumeRequest{VolumeId: "vol1", TargetPath: "target"},
		},
		{
			name: "root target path",
			req:  &csi.NodeUnpublishVolumeRequest{VolumeId: "vol1", TargetPath: "/"},
		},
	}

	d := &Driver{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), locks: newVolumeLocks()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := d.NodeUnpublishVolume(context.Background(), tt.req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestVolumeLocks(t *testing.T) {
	t.Parallel()

	l := newVolumeLocks()
	require.True(t, l.TryAcquire("vol1"))
	require.False(t, l.TryAcquire("vol1"), "a held volume cannot be acquired again")
	require.True(t, l.TryAcquire("vol2"), "a different volume is independent")
	l.Release("vol1")
	require.True(t, l.TryAcquire("vol1"), "acquire succeeds after release")
}

// A publish for a volume with an operation already in flight returns
// ABORTED, after passing validation but before any I/O.
func TestNodePublishVolume_AbortsWhenVolumeLocked(t *testing.T) {
	t.Parallel()

	d := &Driver{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), locks: newVolumeLocks()}
	d.locks.TryAcquire("vol1") // simulate an in-flight operation for vol1

	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "vol1",
		TargetPath: "/target",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
		VolumeContext: map[string]string{VolumeAttrProgram: "prog", VolumeAttrMaps: "map_a"},
	}
	_, err := d.NodePublishVolume(context.Background(), req)
	require.Equal(t, codes.Aborted, status.Code(err))
}

func TestParseMapNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "a", []string{"a"}},
		{"trim and split", " a , b ,c", []string{"a", "b", "c"}},
		{"drop empties", "a,,b, ,c", []string{"a", "b", "c"}},
		{"deduplicate, keep first-seen order", "a,b,a,c,b", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, parseMapNames(tt.in))
		})
	}
}
