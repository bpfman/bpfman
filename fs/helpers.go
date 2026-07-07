package fs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// copyFile copies a regular file from src to dst.
func copyFile(src, dst string, perm os.FileMode) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer df.Close()

	if _, err := io.Copy(df, sf); err != nil {
		return err
	}
	return df.Close()
}

// writeJSON writes v as indented JSON to path.
func writeJSON(path string, perm os.FileMode, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	data = append(data, '\n')
	return os.WriteFile(path, data, perm)
}

// safeRemoveAll removes target only if it is under parent. This
// prevents accidental deletion of paths outside the expected tree.
//
// Both paths are cleaned before comparison to normalise "..", ".", and
// redundant separators. Uses filepath.Rel to avoid prefix false positives
// (e.g., /run/bpfman/programsX matching /run/bpfman/programs).
func safeRemoveAll(parent, target string) error {
	cleanParent := filepath.Clean(parent)
	cleanTarget := filepath.Clean(target)

	rel, err := filepath.Rel(cleanParent, cleanTarget)
	if err != nil {
		return ErrOutsideLayout{Parent: cleanParent, Target: cleanTarget}
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ErrOutsideLayout{Parent: cleanParent, Target: cleanTarget}
	}

	if err := os.RemoveAll(cleanTarget); err != nil {
		return &PathError{Op: "remove_all", Path: cleanTarget, Err: err}
	}
	return nil
}

// validateRegularFile checks that path exists and is a regular file.
func validateRegularFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return &PathError{Op: "validate", Path: path, Err: err}
	}
	if !fi.Mode().IsRegular() {
		return &PathError{Op: "validate", Path: path, Err: fmt.Errorf("not a regular file")}
	}
	return nil
}
