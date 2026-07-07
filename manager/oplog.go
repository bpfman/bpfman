package manager

import (
	"context"
	"log/slog"
)

// opIDHandler wraps a slog.Handler to automatically extract op_id from
// context and add it to log records. Use with InfoContext, WarnContext, etc.
type opIDHandler struct {
	slog.Handler
}

// Handle extracts op_id from context and adds it to the record.
func (h opIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if opID := OpIDFromContext(ctx); opID != 0 {
		r.AddAttrs(slog.Uint64("op_id", opID))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes, maintaining the wrapper.
func (h opIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return opIDHandler{h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group, maintaining the wrapper.
func (h opIDHandler) WithGroup(name string) slog.Handler {
	return opIDHandler{h.Handler.WithGroup(name)}
}

// WithOpIDHandler wraps a logger's handler to extract op_id from context.
func WithOpIDHandler(logger *slog.Logger) *slog.Logger {
	return slog.New(opIDHandler{logger.Handler()})
}
