package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

func schedSchedSwitchAvailable() bool {
	_, err := os.Stat(filepath.Join(tracingEventsPath, "sched", "sched_switch", "id"))
	return err == nil
}

func TestKernelAdapter_ListTracepoints(t *testing.T) {
	t.Parallel()

	if !schedSchedSwitchAvailable() {
		t.Skip("tracefs not available (running in container?)")
	}

	k := &kernelAdapter{}
	tps, err := k.ListTracepoints(context.Background())
	if err != nil {
		t.Fatalf("ListTracepoints: %v", err)
	}

	if len(tps) == 0 {
		t.Fatal("expected at least one tracepoint on a system with tracefs")
	}
	if !slices.Contains(tps, "sched/sched_switch") {
		t.Errorf("expected sched/sched_switch in the list, got %d entries starting with %v", len(tps), tps[:min(5, len(tps))])
	}
	if !sort.StringsAreSorted(tps) {
		t.Error("expected the list to be sorted")
	}
	for _, tp := range tps {
		if strings.Count(tp, "/") != 1 {
			t.Errorf("expected exactly one '/' in %q", tp)
		}
	}
}

func TestIsTracepointNotFoundError(t *testing.T) {
	t.Parallel()

	t.Run("os err not exist", func(t *testing.T) {
		t.Parallel()
		if !isTracepointNotFoundError(os.ErrNotExist) {
			t.Fatal("expected os.ErrNotExist to be treated as tracepoint not found")
		}
	})

	t.Run("wrapped os err not exist", func(t *testing.T) {
		t.Parallel()
		err := fmt.Errorf("attach tracepoint: %w", os.ErrNotExist)
		if !isTracepointNotFoundError(err) {
			t.Fatal("expected wrapped ENOENT to be treated as tracepoint not found")
		}
	})

	t.Run("other error", func(t *testing.T) {
		t.Parallel()
		if isTracepointNotFoundError(errors.New("permission denied")) {
			t.Fatal("did not expect unrelated errors to be treated as tracepoint not found")
		}
	})
}
