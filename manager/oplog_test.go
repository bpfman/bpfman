package manager_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/bpfman/bpfman/manager"
)

func TestWithOpIDHandler_WithoutOpIDInContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewTextHandler(&buf, nil))
	logger := manager.WithOpIDHandler(baseLogger)

	// Without op_id in context, should not include op_id
	logger.InfoContext(context.Background(), "test message")
	if strings.Contains(buf.String(), "op_id") {
		t.Errorf("expected no op_id without context, got: %s", buf.String())
	}
}

func TestWithOpIDHandler_WithOpIDInContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewTextHandler(&buf, nil))
	logger := manager.WithOpIDHandler(baseLogger)

	ctx := manager.ContextWithOpID(context.Background(), 123)
	logger.InfoContext(ctx, "wrapped logger test")
	output := buf.String()
	if !strings.Contains(output, "op_id=123") {
		t.Errorf("expected op_id=123 in output, got: %s", output)
	}
}

func TestWithOpIDHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	// Verify op_id works after calling logger.With() which uses WithAttrs
	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewTextHandler(&buf, nil))
	logger := manager.WithOpIDHandler(baseLogger)

	// Add attributes like the server/manager do
	logger = logger.With("component", "test")

	ctx := manager.ContextWithOpID(context.Background(), 456)
	logger.InfoContext(ctx, "with attrs test")
	output := buf.String()
	if !strings.Contains(output, "op_id=456") {
		t.Errorf("expected op_id=456 in output after With(), got: %s", output)
	}
	if !strings.Contains(output, "component=test") {
		t.Errorf("expected component=test in output, got: %s", output)
	}
}
