package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/logging"
)

func TestFilteringHandler_Enabled(t *testing.T) {
	t.Parallel()

	spec := &logging.Spec{
		BaseLevel: logging.LevelWarn,
		Components: map[string]logging.Level{
			"manager": logging.LevelDebug,
			"store":   logging.LevelTrace,
		},
	}

	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: logging.LevelTrace.ToSlog()})
	handler := logging.NewFilteringHandler(inner, spec)

	// Base handler (no component) uses warn level
	assert.False(t, handler.Enabled(context.Background(), slog.LevelDebug))
	assert.False(t, handler.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, handler.Enabled(context.Background(), slog.LevelWarn))
	assert.True(t, handler.Enabled(context.Background(), slog.LevelError))

	// Manager component uses debug level
	managerHandler := handler.WithAttrs([]slog.Attr{slog.String("component", "manager")})
	assert.True(t, managerHandler.Enabled(context.Background(), slog.LevelDebug))
	assert.True(t, managerHandler.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, managerHandler.Enabled(context.Background(), slog.LevelWarn))
	assert.False(t, managerHandler.Enabled(context.Background(), logging.LevelTrace.ToSlog()))

	// Store component uses trace level
	storeHandler := handler.WithAttrs([]slog.Attr{slog.String("component", "store")})
	assert.True(t, storeHandler.Enabled(context.Background(), logging.LevelTrace.ToSlog()))
	assert.True(t, storeHandler.Enabled(context.Background(), slog.LevelDebug))
}

func TestFilteringHandler_Handle(t *testing.T) {
	t.Parallel()

	spec := &logging.Spec{
		BaseLevel: logging.LevelWarn,
		Components: map[string]logging.Level{
			"manager": logging.LevelDebug,
		},
	}

	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: logging.LevelTrace.ToSlog()})
	handler := logging.NewFilteringHandler(inner, spec)

	ctx := context.Background()

	// Debug message without component should be filtered
	buf.Reset()
	r := slog.NewRecord(testTime(), slog.LevelDebug, "debug message", 0)
	err := handler.Handle(ctx, r)
	require.NoError(t, err)
	assert.Empty(t, buf.String())

	// Warn message without component should pass
	buf.Reset()
	r = slog.NewRecord(testTime(), slog.LevelWarn, "warn message", 0)
	err = handler.Handle(ctx, r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "warn message")

	// Debug message with manager component should pass
	managerHandler := handler.WithAttrs([]slog.Attr{slog.String("component", "manager")})
	buf.Reset()
	r = slog.NewRecord(testTime(), slog.LevelDebug, "manager debug", 0)
	err = managerHandler.Handle(ctx, r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "manager debug")
}

func TestFilteringHandler_WithGroup(t *testing.T) {
	t.Parallel()

	spec := &logging.Spec{
		BaseLevel: logging.LevelInfo,
		Components: map[string]logging.Level{
			"manager": logging.LevelDebug,
		},
	}

	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: logging.LevelTrace.ToSlog()})
	handler := logging.NewFilteringHandler(inner, spec)

	// WithGroup should preserve the component
	managerHandler := handler.WithAttrs([]slog.Attr{slog.String("component", "manager")})
	groupHandler := managerHandler.WithGroup("request")

	// Should still use manager's debug level
	assert.True(t, groupHandler.Enabled(context.Background(), slog.LevelDebug))
}

func TestFilteringHandler_Integration(t *testing.T) {
	t.Parallel()

	spec, err := logging.ParseSpec("warn,manager=debug,store=trace")
	require.NoError(t, err)

	var buf bytes.Buffer
	logger, err := logging.New(logging.Options{
		CLISpec: spec.String(),
		Output:  &buf,
	})
	require.NoError(t, err)

	// Root logger uses warn level
	buf.Reset()
	logger.Debug("root debug")
	assert.Empty(t, buf.String())

	buf.Reset()
	logger.Warn("root warn")
	assert.Contains(t, buf.String(), "root warn")

	// Manager logger uses debug level
	managerLogger := logger.With("component", "manager")

	buf.Reset()
	managerLogger.Debug("manager debug")
	assert.Contains(t, buf.String(), "manager debug")

	buf.Reset()
	managerLogger.Info("manager info")
	assert.Contains(t, buf.String(), "manager info")

	// Store logger uses trace level
	storeLogger := logger.With("component", "store")

	buf.Reset()
	storeLogger.Log(context.Background(), logging.LevelTrace.ToSlog(), "store trace")
	assert.Contains(t, buf.String(), "store trace")

	// Server logger (not in spec) falls back to warn
	serverLogger := logger.With("component", "server")

	buf.Reset()
	serverLogger.Debug("server debug")
	assert.Empty(t, buf.String())

	buf.Reset()
	serverLogger.Warn("server warn")
	assert.Contains(t, buf.String(), "server warn")
}

func TestNew_Precedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      logging.Options
		wantLevel logging.Level
	}{
		{
			name:      "cli takes precedence over env",
			opts:      logging.Options{CLISpec: "error", EnvSpec: "debug", ConfigSpec: "info"},
			wantLevel: logging.LevelError,
		},
		{
			name:      "env takes precedence over config",
			opts:      logging.Options{EnvSpec: "debug", ConfigSpec: "info"},
			wantLevel: logging.LevelDebug,
		},
		{
			name:      "config used when nothing else specified",
			opts:      logging.Options{ConfigSpec: "warn"},
			wantLevel: logging.LevelWarn,
		},
		{
			name:      "default is warn",
			opts:      logging.Options{},
			wantLevel: logging.LevelWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			tt.opts.Output = &buf

			logger, err := logging.New(tt.opts)
			require.NoError(t, err)

			// Check that the expected level is enabled
			ctx := context.Background()

			buf.Reset()
			logger.Log(ctx, tt.wantLevel.ToSlog(), "test message")
			assert.NotEmpty(t, buf.String(), "expected level %s should be logged", tt.wantLevel)

			// Check that the level below is not enabled
			if tt.wantLevel > logging.LevelTrace {
				belowLevel := logging.Level(int(tt.wantLevel) - 4)
				buf.Reset()
				logger.Log(ctx, belowLevel.ToSlog(), "test message below")
				assert.Empty(t, buf.String(), "level %s below %s should not be logged", belowLevel, tt.wantLevel)
			}
		})
	}
}

func TestNew_InvalidSpec(t *testing.T) {
	t.Parallel()

	_, err := logging.New(logging.Options{CLISpec: "invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log spec")
}

func TestParseFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    logging.Format
		wantErr bool
	}{
		{"text", logging.FormatText, false},
		{"json", logging.FormatJSON, false},
		{"TEXT", logging.FormatText, false},
		{"JSON", logging.FormatJSON, false},
		{"", logging.FormatText, false},
		{"invalid", logging.FormatText, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := logging.ParseFormat(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNew_JSONFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := logging.New(logging.Options{
		CLISpec: "info",
		Format:  logging.FormatJSON,
		Output:  &buf,
	})
	require.NoError(t, err)

	logger.Info("test message", "key", "value")
	output := buf.String()

	// JSON output should contain these elements
	assert.True(t, strings.HasPrefix(output, "{"))
	assert.Contains(t, output, `"msg":"test message"`)
	assert.Contains(t, output, `"key":"value"`)
}

func testTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}
