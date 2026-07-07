package ebpf

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
)

// tracingEventsPath is the base path for tracepoint events.
const tracingEventsPath = "/sys/kernel/tracing/events"

// ListTracepoints returns every kernel tracepoint visible under
// /sys/kernel/tracing/events/, formatted as "group/name" and sorted
// lexicographically. Entries that are not tracepoints (e.g. the
// top-level enable/filter/header_page files, and per-group enable/
// filter files) are skipped because they do not contain an id file.
// Returns an empty slice when tracefs is unavailable; callers should
// treat that as "cannot validate" rather than "no tracepoints exist".
func (k *kernelAdapter) ListTracepoints(_ context.Context) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(tracingEventsPath, "*", "*", "id"))
	if err != nil {
		return nil, fmt.Errorf("scan tracepoints: %w", err)
	}

	results := make([]string, 0, len(matches))
	for _, idPath := range matches {
		name := filepath.Base(filepath.Dir(idPath))
		group := filepath.Base(filepath.Dir(filepath.Dir(idPath)))
		results = append(results, group+"/"+name)
	}
	slices.Sort(results)
	return results, nil
}
