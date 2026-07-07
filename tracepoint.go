package bpfman

import (
	"fmt"
	"strings"
)

// Tracepoint identifies a kernel tracepoint in "group/name" form,
// matching the layout under /sys/kernel/tracing/events/. Only shape is
// parsed here; existence is checked at attach time.
type Tracepoint struct {
	group string
	name  string
}

// ParseTracepoint parses a tracepoint identifier in "group/name" form.
func ParseTracepoint(s string) (Tracepoint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Tracepoint{}, fmt.Errorf("tracepoint cannot be empty")
	}
	if strings.ContainsAny(s, " \t\n") {
		return Tracepoint{}, fmt.Errorf("invalid tracepoint %q: whitespace not allowed", s)
	}

	group, name, ok := strings.Cut(s, "/")
	if !ok {
		return Tracepoint{}, fmt.Errorf("invalid tracepoint %q: expected group/name (e.g. sched/sched_switch)", s)
	}
	if strings.Contains(name, "/") {
		return Tracepoint{}, fmt.Errorf("invalid tracepoint %q: expected group/name (only one '/' allowed)", s)
	}
	if group == "" {
		return Tracepoint{}, fmt.Errorf("invalid tracepoint %q: group cannot be empty", s)
	}
	if name == "" {
		return Tracepoint{}, fmt.Errorf("invalid tracepoint %q: name cannot be empty", s)
	}

	return Tracepoint{group: group, name: name}, nil
}

// Group returns the tracepoint group, such as "sched".
func (t Tracepoint) Group() string { return t.group }

// Name returns the tracepoint name, such as "sched_switch".
func (t Tracepoint) Name() string { return t.name }

// String returns the tracepoint in "group/name" form.
func (t Tracepoint) String() string { return t.group + "/" + t.name }
