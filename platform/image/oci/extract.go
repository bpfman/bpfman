package oci

import (
	"archive/tar"
	"compress/gzip"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ELF machine type for eBPF
	elfMachineBPF = 247

	// Maximum size of a BPF object file (16 MB should be plenty)
	maxBytecodeSize = 16 * 1024 * 1024
)

// extractBytecode finds and extracts the BPF bytecode from downloaded OCI layers.
// It searches for .o files in:
// 1. Direct files in the directory (may be ELF or gzipped ELF)
// 2. Gzipped tar archives (OCI layer blobs)
// 3. Plain ELF files without .o extension
func extractBytecode(dir string, logger *slog.Logger) (string, error) {
	logger.Debug("extracting bytecode from download directory", "dir", dir)

	// First, look for direct .o files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	logger.Debug("scanning directory for bytecode", "dir", dir, "file_count", len(entries))
	for _, entry := range entries {
		info, _ := entry.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		logger.Debug("found file", "name", entry.Name(), "is_dir", entry.IsDir(), "size", size)
	}

	var lastExtractErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		// Check for direct ELF file (with or without .o extension)
		if isELFFile(path) {
			logger.Debug("found ELF bytecode file", "name", entry.Name())
			return path, nil
		}

		// Check for gzipped content (could be gzipped ELF or gzipped tar)
		if isGzipFile(path) {
			logger.Debug("found gzipped content", "name", entry.Name())

			// Try to extract as gzipped tar first
			extracted, err := extractFromTarGz(path, dir, logger)
			if err == nil && extracted != "" {
				return extracted, nil
			}

			// If that failed, try as gzipped ELF
			extracted, err = extractGzippedELF(path, dir, logger)
			if err == nil && extracted != "" {
				return extracted, nil
			}

			// Both attempts failed. Keep the reason so a corrupt,
			// oversized, or malformed layer is reported as such at the
			// fall-through rather than as a missing-bytecode image.
			if err != nil {
				lastExtractErr = fmt.Errorf("extract %s: %w", entry.Name(), err)
			}
			logger.Debug("gzip extraction failed", "name", entry.Name(), "error", err)
		}

		// Check for plain tar
		if isTarFile(path) {
			logger.Debug("found tar archive, extracting", "name", entry.Name())
			extracted, err := extractFromTar(path, dir, logger)
			if err != nil {
				return "", fmt.Errorf("failed to extract archive %s: %w", entry.Name(), err)
			}
			if extracted != "" {
				return extracted, nil
			}
		}
	}

	if lastExtractErr != nil {
		return "", fmt.Errorf("no BPF bytecode (.o file) found in image: %w", lastExtractErr)
	}
	return "", fmt.Errorf("no BPF bytecode (.o file) found in image")
}

// extractGzippedELF extracts a gzip-compressed ELF file.
func extractGzippedELF(gzPath, destDir string, logger *slog.Logger) (string, error) {
	f, err := os.Open(gzPath)
	if err != nil {
		return "", err
	}

	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader: %w", err)
	}

	defer gzr.Close()

	// Read decompressed content
	destPath := filepath.Join(destDir, "extracted_bytecode.o")
	outFile, err := os.Create(destPath)
	if err != nil {
		return "", err
	}

	limited := io.LimitReader(gzr, maxBytecodeSize)
	_, copyErr := io.Copy(outFile, limited)
	closeErr := outFile.Close()

	if copyErr != nil {
		if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logger.Warn("failed to remove file during cleanup", "path", destPath, "error", rmErr)
		}
		return "", copyErr
	}

	if closeErr != nil {
		if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logger.Warn("failed to remove file during cleanup", "path", destPath, "error", rmErr)
		}
		return "", fmt.Errorf("failed to close output file: %w", closeErr)
	}

	// Verify it's an ELF file
	if !isELFFile(destPath) {
		if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logger.Warn("failed to remove invalid file during cleanup", "path", destPath, "error", rmErr)
		}
		return "", fmt.Errorf("decompressed content is not an ELF file")
	}

	logger.Debug("extracted gzipped ELF", "path", destPath)
	return destPath, nil
}

// extractFromTarGz extracts .o files from a gzipped tar archive.
func extractFromTarGz(archivePath, destDir string, logger *slog.Logger) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}

	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader: %w", err)
	}

	defer gzr.Close()

	return extractFromTarReader(tar.NewReader(gzr), destDir, logger)
}

// extractFromTar extracts .o files from a tar archive.
func extractFromTar(archivePath, destDir string, logger *slog.Logger) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}

	defer f.Close()

	return extractFromTarReader(tar.NewReader(f), destDir, logger)
}

// extractFromTarReader extracts .o files from a tar reader.
func extractFromTarReader(tr *tar.Reader, destDir string, logger *slog.Logger) (string, error) {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Only extract .o files
		if !strings.HasSuffix(hdr.Name, ".o") {
			continue
		}

		// Security: prevent path traversal
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") {
			return "", fmt.Errorf("archive entry has invalid path: %s", hdr.Name)
		}

		// Check size limit
		if hdr.Size > maxBytecodeSize {
			return "", fmt.Errorf("archive entry %s is too large: %d bytes", hdr.Name, hdr.Size)
		}

		destPath := filepath.Join(destDir, "extracted_"+filepath.Base(cleanName))
		logger.Debug("extracting file from archive", "name", hdr.Name, "dest", destPath)

		outFile, err := os.Create(destPath)
		if err != nil {
			return "", fmt.Errorf("failed to create output file: %w", err)
		}

		// Use LimitReader to prevent decompression bombs
		limited := io.LimitReader(tr, maxBytecodeSize)
		_, copyErr := io.Copy(outFile, limited)
		closeErr := outFile.Close()

		if copyErr != nil {
			if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
				logger.Warn("failed to remove file during cleanup", "path", destPath, "error", rmErr)
			}
			return "", fmt.Errorf("failed to extract file: %w", copyErr)
		}

		if closeErr != nil {
			if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
				logger.Warn("failed to remove file during cleanup", "path", destPath, "error", rmErr)
			}
			return "", fmt.Errorf("failed to close output file: %w", closeErr)
		}

		// Verify it's a valid ELF
		if isELFFile(destPath) {
			return destPath, nil
		}

		// Not a valid ELF, remove it
		if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logger.Warn("failed to remove non-ELF file during cleanup", "path", destPath, "error", rmErr)
		}
	}

	return "", nil
}

// validateELF checks that the file is a valid eBPF ELF object.
func validateELF(path string, logger *slog.Logger) error {
	logger.Debug("validating ELF file", "path", path)

	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("invalid ELF file: %w", err)
	}

	defer f.Close()

	// Check machine type is BPF
	if f.Machine != elf.Machine(elfMachineBPF) {
		return fmt.Errorf("not a BPF ELF file: machine type is %v, expected eBPF (%d)", f.Machine, elfMachineBPF)
	}

	// Check endianness matches host
	hostEndian := getHostEndianness()
	fileEndian := "little"
	if f.Data == elf.ELFDATA2MSB {
		fileEndian = "big"
	}

	if fileEndian != hostEndian {
		return fmt.Errorf("ELF endianness mismatch: file is %s, host is %s", fileEndian, hostEndian)
	}

	// Check it's a relocatable object file
	if f.Type != elf.ET_REL {
		return fmt.Errorf("not a relocatable ELF file: type is %v, expected ET_REL", f.Type)
	}

	logger.Info("ELF validation successful", "path", path, "machine", "eBPF", "endianness", fileEndian)
	return nil
}

// isELFFile checks if a file is an ELF file by checking the magic number.
func isELFFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer f.Close()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}

	return magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F'
}

// isGzipFile checks if a file is gzip compressed.
func isGzipFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer f.Close()

	var magic [2]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}

	return magic[0] == 0x1f && magic[1] == 0x8b
}

// isTarFile checks if a file appears to be a tar archive.
func isTarFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer f.Close()

	// Check for ustar magic at offset 257
	var magic [5]byte
	if _, err := f.Seek(257, io.SeekStart); err != nil {
		return false
	}

	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}

	return string(magic[:]) == "ustar"
}

// getHostEndianness returns "little" or "big" based on the host architecture.
func getHostEndianness() string {
	buf := [2]byte{}
	binary.NativeEndian.PutUint16(buf[:], 0x0102)
	if buf[0] == 0x01 {
		return "big"
	}
	return "little"
}
