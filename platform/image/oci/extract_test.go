package oci

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A layer that sniffs as gzip (magic bytes) but fails to decompress must
// be reported as a failed extraction naming the layer, not flattened to
// the generic missing-bytecode message, so a corrupt, truncated, or
// malformed layer is diagnosable.
func TestExtractBytecode_CorruptGzipSurfacesReason(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// gzip magic (1f 8b) followed by too few bytes to form a header, so
	// the gzip reader fails; the sniff still routes it to the gzip path.
	if err := os.WriteFile(filepath.Join(dir, "layer.gz"), []byte{0x1f, 0x8b, 0x08, 0x00, 'n', 'o'}, 0o644); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := extractBytecode(dir, logger)
	if err == nil {
		t.Fatal("expected an error for a corrupt gzip layer")
	}
	if !strings.Contains(err.Error(), "layer.gz") {
		t.Errorf("error should name the failing layer and its reason, got: %v", err)
	}
}
