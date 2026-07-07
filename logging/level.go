package logging

import (
	"fmt"
	"log/slog"
	"strings"
)

// Level represents a log level with support for trace level below slog's built-in levels.
// Values match slog.Level constants for debug through error.
type Level int

const (
	// LevelTrace is the most verbose level, below debug.
	LevelTrace Level = -8
	// LevelDebug is for debug messages. Matches slog.LevelDebug.
	LevelDebug Level = -4
	// LevelInfo is for informational messages. Matches slog.LevelInfo.
	LevelInfo Level = 0
	// LevelWarn is for warning messages. Matches slog.LevelWarn.
	LevelWarn Level = 4
	// LevelError is for error messages. Matches slog.LevelError.
	LevelError Level = 8
)

// ParseLevel parses a string into a Level.
// Supported values: trace, debug, info, warn, error (case-insensitive).
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace, nil
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error", "err":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %q", s)
	}
}

// ToSlog converts Level to slog.Level.
func (l Level) ToSlog() slog.Level {
	return slog.Level(l)
}

// String returns the string representation of the level.
func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "trace"
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return fmt.Sprintf("Level(%d)", l)
	}
}
