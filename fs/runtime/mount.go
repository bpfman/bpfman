package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	// DefaultMountInfoPath is the path to the mountinfo file.
	DefaultMountInfoPath = "/proc/self/mountinfo"

	// defaultScanMaxLineLen is the maximum line length for
	// scanning mountinfo. Some nodes/runtimes can produce long
	// lines; this prevents ErrTooLong.
	defaultScanMaxLineLen = 1024 * 1024
)

// MountpointFsType returns the filesystem type of the mount at
// mountPoint according to mountInfoPath (e.g. /proc/self/mountinfo),
// and whether any mount was found there. When mounts are stacked on
// the same mountpoint the topmost (last-listed) entry wins, matching
// what a process at that path actually sees.
//
// The mountinfo format is documented in proc(5). Each line contains:
//
//	mount_id parent_id major:minor root mount_point options [optional_fields...] - fstype source super_options
//
// Example bpffs entry:
//
//	30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700
//	              ^                               ^
//	              mount_point (fields[4])         fstype (after " - ")
//
// The key insight from libmount (util-linux) is that the separator "
// - " must be found using string search, not by assuming a fixed
// field position. This is because optional fields (like "shared:N"
// for mount propagation) may be present between the mount options and
// the separator.
func MountpointFsType(mountInfoPath, mountPoint string) (string, bool, error) {
	file, err := os.Open(mountInfoPath)
	if err != nil {
		return "", false, fmt.Errorf("opening mountinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultScanMaxLineLen)

	fsType := ""
	found := false
	for scanner.Scan() {
		line := scanner.Text()

		// Find the separator " - " which precedes "fstype
		// source super_options". This is how libmount parses
		// mountinfo (see mnt_parse_mountinfo_line).
		before, after, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}

		// Parse the prefix: mount_id parent_id major:minor
		// root mount_point ...
		fields := strings.Fields(before)
		if len(fields) < 5 {
			continue
		}
		if unescapeMountInfo(fields[4]) != mountPoint {
			continue
		}

		// Parse the suffix after " - ": fstype source
		// super_options.
		suffixFields := strings.Fields(after)
		if len(suffixFields) < 1 {
			continue
		}

		// Topmost mount wins: keep scanning and take the last
		// matching entry.
		fsType = suffixFields[0]
		found = true
	}

	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("reading mountinfo: %w", err)
	}

	return fsType, found, nil
}

// IsBpffsMounted reports whether the filesystem mounted at mountPoint
// is a bpffs, according to mountInfoPath (e.g. /proc/self/mountinfo).
// A bind mount of a bpffs also reports the "bpf" filesystem type, so
// this is true for both a direct bpffs mount and a bind mount of one.
func IsBpffsMounted(mountInfoPath, mountPoint string) (bool, error) {
	fsType, mounted, err := MountpointFsType(mountInfoPath, mountPoint)
	if err != nil || !mounted {
		return false, err
	}
	return fsType == "bpf", nil
}

// Mount mounts a bpffs at mountPoint, creating the directory if needed.
func Mount(mountPoint string) error {
	fi, err := os.Stat(mountPoint)
	switch {
	case err == nil:
		if !fi.IsDir() {
			return fmt.Errorf("mount point exists but is not a directory")
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return fmt.Errorf("creating mount point directory: %w", err)
		}
	default:
		return fmt.Errorf("stat mount point: %w", err)
	}

	if err := syscall.Mount("bpffs", mountPoint, "bpf", 0, ""); err != nil {
		return fmt.Errorf("mount syscall: %w", err)
	}

	return nil
}

// EnsureMounted ensures a bpffs is mounted at mountPoint. It checks
// mountInfoPath (e.g. /proc/self/mountinfo) for an existing bpf mount
// at mountPoint; if none is found, it mounts one.
//
// Equivalent to:
//
//	if ! findmnt --noheadings --types bpf <mountPoint>; then
//	  mount bpffs <mountPoint> -t bpf
//	fi
func EnsureMounted(mountInfoPath, mountPoint string) error {
	return EnsureMountedWith(mountInfoPath, mountPoint, Mount)
}

// EnsureMountedWith is like EnsureMounted but accepts a mount function.
// This is primarily for tests that need to simulate mount errors.
func EnsureMountedWith(mountInfoPath, mountPoint string, mountFn func(string) error) error {
	mounted, err := IsBpffsMounted(mountInfoPath, mountPoint)
	if err != nil {
		return err
	}

	if mounted {
		return nil
	}
	if err := mountFn(mountPoint); err != nil {
		if errors.Is(err, syscall.EBUSY) {
			mounted, recheckErr := IsBpffsMounted(mountInfoPath, mountPoint)
			if recheckErr == nil && mounted {
				return nil
			}
		}
		return err
	}
	return nil
}

// unescapeMountInfo converts an escaped mountinfo field into its literal form.
// The kernel escapes space, tab, newline, and backslash in mount point fields
// using 3-digit octal sequences (e.g., "\040" for space). See mangle_path() in
// fs/seq_file.c and its usage in fs/proc_namespace.c.
// This mirrors util-linux/libmount's handling of mountinfo escaping.
//
// We unescape because comparisons against mountPoint should use the literal
// path as provided by callers, not the escaped representation from /proc.
//
// Examples:
//   - "/sys/fs/bpf\\040extra" -> "/sys/fs/bpf extra"
//   - "tab\\011sep" -> "tab\tsep"
//   - "newline\\012here" -> "newline\nhere"
//   - "backslash\\134path" -> "backslash\\path"
//
// The logic scans for backslash-escaped octal triplets and replaces them with
// the corresponding byte. Non-escape sequences are left as-is. This matches
// util-linux's unmangle_to_buffer() in lib/mangle.c, which only decodes
// backslash plus three octal digits and leaves other sequences untouched.
func unescapeMountInfo(s string) string {
	if strings.IndexByte(s, '\\') == -1 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+3 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		if s[i+1] < '0' || s[i+1] > '7' || s[i+2] < '0' || s[i+2] > '7' || s[i+3] < '0' || s[i+3] > '7' {
			b.WriteByte(s[i])
			continue
		}
		v, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String()
}
