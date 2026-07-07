package logging

import (
	"context"
	"log/slog"
)

// componentKey is the attribute key used for component names.
const componentKey = "component"

// filteringHandler is a slog.Handler that filters log records based on
// component-specific log levels defined in a Spec.
type filteringHandler struct {
	inner     slog.Handler
	spec      *Spec
	component string
}

// NewFilteringHandler creates a new slog.Handler that filters based on component levels.
func NewFilteringHandler(inner slog.Handler, spec *Spec) slog.Handler {
	return &filteringHandler{
		inner: inner,
		spec:  spec,
	}
}

// Enabled reports whether the handler handles records at the given level.
// It checks the level against the spec for the current component.
func (h *filteringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	componentLevel := h.spec.LevelFor(h.component)
	return level >= componentLevel.ToSlog()
}

// Handle handles the Record.
// It delegates to the inner handler if the record should be logged.
func (h *filteringHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.Enabled(ctx, r.Level) {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new Handler with the given attributes added.
// If a "component" attribute is found, it updates the component for filtering.
func (h *filteringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &filteringHandler{
		inner:     h.inner.WithAttrs(attrs),
		spec:      h.spec,
		component: h.component,
	}

	// Check if any of the attrs is a component
	for _, attr := range attrs {
		if attr.Key == componentKey {
			newHandler.component = attr.Value.String()
			break
		}
	}

	return newHandler
}

// WithGroup returns a new Handler with the given group appended to the receiver's groups.
func (h *filteringHandler) WithGroup(name string) slog.Handler {
	return &filteringHandler{
		inner:     h.inner.WithGroup(name),
		spec:      h.spec,
		component: h.component,
	}
}
