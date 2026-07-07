package bpfman

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTracepoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		group string
		name  string
	}{
		{"sched/sched_switch", "sched", "sched_switch"},
		{"syscalls/sys_enter_kill", "syscalls", "sys_enter_kill"},
		{"  sched/sched_switch  ", "sched", "sched_switch"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			tp, err := ParseTracepoint(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.group, tp.Group())
			assert.Equal(t, tt.name, tp.Name())
			assert.Equal(t, tt.group+"/"+tt.name, tp.String())
		})
	}
}

func TestParseTracepointRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		errContains string
	}{
		{"empty", "", "cannot be empty"},
		{"whitespace only", "   ", "cannot be empty"},
		{"no slash", "sched_switch", "expected group/name"},
		{"empty group", "/sched_switch", "group cannot be empty"},
		{"empty name", "sched/", "name cannot be empty"},
		{"only slash", "/", "group cannot be empty"},
		{"extra slash", "a/b/c", "only one '/' allowed"},
		{"internal space", "sched /sched_switch", "whitespace not allowed"},
		{"internal tab", "sched/sched\tswitch", "whitespace not allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTracepoint(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}
