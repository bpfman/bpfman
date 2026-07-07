package logging_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/logging"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    logging.Level
		wantErr bool
	}{
		{name: "trace", input: "trace", want: logging.LevelTrace},
		{name: "debug", input: "debug", want: logging.LevelDebug},
		{name: "info", input: "info", want: logging.LevelInfo},
		{name: "warn", input: "warn", want: logging.LevelWarn},
		{name: "warning", input: "warning", want: logging.LevelWarn},
		{name: "error", input: "error", want: logging.LevelError},
		{name: "err", input: "err", want: logging.LevelError},
		{name: "uppercase", input: "DEBUG", want: logging.LevelDebug},
		{name: "mixed case", input: "Info", want: logging.LevelInfo},
		{name: "with spaces", input: "  warn  ", want: logging.LevelWarn},
		{name: "invalid", input: "invalid", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := logging.ParseLevel(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLevel_ToSlog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level logging.Level
		want  slog.Level
	}{
		{logging.LevelTrace, slog.Level(-8)},
		{logging.LevelDebug, slog.LevelDebug},
		{logging.LevelInfo, slog.LevelInfo},
		{logging.LevelWarn, slog.LevelWarn},
		{logging.LevelError, slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.level.ToSlog())
		})
	}
}

func TestLevel_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level logging.Level
		want  string
	}{
		{logging.LevelTrace, "trace"},
		{logging.LevelDebug, "debug"},
		{logging.LevelInfo, "info"},
		{logging.LevelWarn, "warn"},
		{logging.LevelError, "error"},
		{logging.Level(99), "Level(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.level.String())
		})
	}
}
