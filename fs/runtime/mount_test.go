package runtime_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/bpfman/bpfman/fs/runtime"
)

func TestIsBpffsMounted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mountinfo  string
		mountPoint string
		want       bool
	}{
		{
			name: "util-linux format without propagation - no bpf",
			mountinfo: `15 20 0:3 / /proc rw,relatime - proc /proc rw
16 20 0:15 / /sys rw,relatime - sysfs /sys rw
17 20 0:5 / /dev rw,relatime - devtmpfs udev rw,size=1983516k,nr_inodes=495879,mode=755
20 1 8:4 / / rw,noatime - ext3 /dev/sda4 rw,errors=continue,user_xattr,acl,barrier=0,data=ordered
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "util-linux format without propagation - with bpf",
			mountinfo: `15 20 0:3 / /proc rw,relatime - proc /proc rw
16 20 0:15 / /sys rw,relatime - sysfs /sys rw
17 20 0:5 / /dev rw,relatime - devtmpfs udev rw,size=1983516k,nr_inodes=495879,mode=755
20 1 8:4 / / rw,noatime - ext3 /dev/sda4 rw,errors=continue,user_xattr,acl,barrier=0,data=ordered
48 16 0:39 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "NixOS format with propagation - no bpf",
			mountinfo: `22 31 0:6 / /dev rw,nosuid shared:12 - devtmpfs devtmpfs rw,size=6532720k,nr_inodes=16327128,mode=755
25 31 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:5 - proc proc rw
28 31 0:26 / /sys rw,nosuid,nodev,noexec,relatime shared:6 - sysfs sysfs rw
36 28 0:35 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:8 - cgroup2 cgroup2 rw,nsdelegate,memory_recursiveprot
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "NixOS format with propagation - with bpf",
			mountinfo: `22 31 0:6 / /dev rw,nosuid shared:12 - devtmpfs devtmpfs rw,size=6532720k,nr_inodes=16327128,mode=755
25 31 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:5 - proc proc rw
28 31 0:26 / /sys rw,nosuid,nodev,noexec,relatime shared:6 - sysfs sysfs rw
36 28 0:35 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:8 - cgroup2 cgroup2 rw,nsdelegate,memory_recursiveprot
39 28 0:38 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:11 - bpf bpf rw,gid=983,mode=770
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "CoreOS format with propagation - no bpf",
			mountinfo: `21 72 0:20 / /proc rw,nosuid,nodev,noexec,relatime shared:15 - proc proc rw
22 72 0:21 / /sys rw,nosuid,nodev,noexec,relatime shared:5 - sysfs sysfs rw,seclabel
23 72 0:5 / /dev rw,nosuid shared:11 - devtmpfs devtmpfs rw,seclabel,size=4096k,nr_inodes=4094014,mode=755,inode64
28 22 0:25 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:7 - cgroup2 cgroup2 rw,seclabel
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "CoreOS format with propagation - with bpf",
			mountinfo: `21 72 0:20 / /proc rw,nosuid,nodev,noexec,relatime shared:15 - proc proc rw
22 72 0:21 / /sys rw,nosuid,nodev,noexec,relatime shared:5 - sysfs sysfs rw,seclabel
23 72 0:5 / /dev rw,nosuid shared:11 - devtmpfs devtmpfs rw,seclabel,size=4096k,nr_inodes=4094014,mode=755,inode64
28 22 0:25 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:7 - cgroup2 cgroup2 rw,seclabel
30 22 0:27 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "different mount point",
			mountinfo: `30 22 0:27 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/some/other/path",
			want:       false,
		},
		{
			name: "multiple optional fields",
			mountinfo: `30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 master:1 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name:       "empty file",
			mountinfo:  "",
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "malformed line without separator",
			mountinfo: `this line has no separator
30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "stacked mounts - non-bpf on top of bpf",
			mountinfo: `30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700
31 22 0:28 / /sys/fs/bpf rw,nosuid shared:9 - tmpfs tmpfs rw
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "stacked mounts - bpf on top of non-bpf",
			mountinfo: `30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - tmpfs tmpfs rw
31 22 0:28 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a temporary file with the test mountinfo content
			tmpDir := t.TempDir()
			mountInfoPath := filepath.Join(tmpDir, "mountinfo")
			if err := os.WriteFile(mountInfoPath, []byte(tt.mountinfo), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			got, err := runtime.IsBpffsMounted(mountInfoPath, tt.mountPoint)
			if err != nil {
				t.Fatalf("IsBpffsMounted() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("IsBpffsMounted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBpffsMounted_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := runtime.IsBpffsMounted("/nonexistent/path/mountinfo", "/sys/fs/bpf")
	if err == nil {
		t.Error("IsBpffsMounted() expected error for nonexistent file, got nil")
	}
}

func TestIsBpffsMounted_LongLine(t *testing.T) {
	t.Parallel()

	// Generate a mountinfo line > 64 KiB (default scanner limit).
	// This tests the scanner buffer increase (prevents ErrTooLong).
	// Target ~70 KiB to ensure it fails without the buffer bump.
	var b strings.Builder
	b.WriteString("30 22 0:27 / /sys/fs/bpf rw")
	for b.Len() < 70*1024 {
		b.WriteString(",option")
		b.WriteByte(byte('a' + (b.Len() % 26)))
	}
	b.WriteString(" shared:9 - bpf bpf rw,mode=700\n")
	mountinfo := b.String()

	tmpDir := t.TempDir()
	mountInfoPath := filepath.Join(tmpDir, "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte(mountinfo), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, err := runtime.IsBpffsMounted(mountInfoPath, "/sys/fs/bpf")
	if err != nil {
		t.Fatalf("IsBpffsMounted() error = %v (scanner buffer may be too small)", err)
	}

	if !got {
		t.Error("IsBpffsMounted() = false, want true")
	}
}

func TestIsBpffsMounted_EscapedMountPoint(t *testing.T) {
	t.Parallel()

	mountinfo := "30 22 0:27 / /sys/fs/bpf\\040extra rw,nosuid shared:9 - bpf bpf rw,mode=700\n"

	tmpDir := t.TempDir()
	mountInfoPath := filepath.Join(tmpDir, "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte(mountinfo), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, err := runtime.IsBpffsMounted(mountInfoPath, "/sys/fs/bpf extra")
	if err != nil {
		t.Fatalf("IsBpffsMounted() error = %v", err)
	}

	if !got {
		t.Error("IsBpffsMounted() = false, want true")
	}
}

func TestEnsureMounted_EbusyRecheck(t *testing.T) {
	t.Parallel()

	mountPoint := "/sys/fs/bpf"
	mountinfo := "15 20 0:3 / /proc rw,relatime - proc /proc rw\n"

	tmpDir := t.TempDir()
	mountInfoPath := filepath.Join(tmpDir, "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte(mountinfo), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mountFn := func(mp string) error {
		updated := mountinfo + "30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700\n"
		if err := os.WriteFile(mountInfoPath, []byte(updated), 0644); err != nil {
			t.Fatalf("failed to update test file: %v", err)
		}
		return fmt.Errorf("mount syscall: %w", syscall.EBUSY)
	}

	if err := runtime.EnsureMountedWith(mountInfoPath, mountPoint, mountFn); err != nil {
		t.Fatalf("EnsureMounted() error = %v", err)
	}
}
