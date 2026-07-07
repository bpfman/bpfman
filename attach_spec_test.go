package bpfman

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXDPAttachSpecPriorityParsing(t *testing.T) {
	t.Parallel()

	zero, err := NewXDPAttachSpec(1, "eth0", 0)
	require.NoError(t, err)
	assert.Equal(t, 0, zero.Priority())

	explicit, err := NewXDPAttachSpec(1, "eth0", 25)
	require.NoError(t, err)
	assert.Equal(t, 25, explicit.Priority())

	_, err = NewXDPAttachSpec(1, "eth0", -1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAttachSpec)
	assert.Contains(t, err.Error(), "priority must be non-negative")
}

func TestTCAttachSpecPriorityParsing(t *testing.T) {
	t.Parallel()

	zero, err := NewTCAttachSpec(1, "eth0", TCDirectionIngress, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, zero.Priority())

	explicit, err := NewTCAttachSpec(1, "eth0", TCDirectionIngress, 25)
	require.NoError(t, err)
	assert.Equal(t, 25, explicit.Priority())

	_, err = NewTCAttachSpec(1, "eth0", TCDirectionIngress, -1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAttachSpec)
	assert.Contains(t, err.Error(), "priority must be non-negative")
}

func TestXDPAttachSpecRejectsMissingFields(t *testing.T) {
	t.Parallel()

	_, err := NewXDPAttachSpec(0, "eth0", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "programID is required")

	_, err = NewXDPAttachSpec(1, "", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ifname is required")
}

func TestXDPAttachSpecProceedOnCodesValidateActions(t *testing.T) {
	t.Parallel()

	spec, err := NewXDPAttachSpec(1, "eth0", 0)
	require.NoError(t, err)

	got, err := spec.WithProceedOnCodes([]int32{2, 31})
	require.NoError(t, err)
	assert.Equal(t, []int32{2, 31}, got.ProceedOn())

	_, err = spec.WithProceedOnCodes([]int32{5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown XDP action code 5")
}

func TestTCAttachSpecProceedOnCodesValidateActions(t *testing.T) {
	t.Parallel()

	spec, err := NewTCAttachSpec(1, "eth0", TCDirectionIngress, 0)
	require.NoError(t, err)

	got, err := spec.WithProceedOnCodes([]int32{-1, 30})
	require.NoError(t, err)
	assert.Equal(t, []int32{-1, 30}, got.ProceedOn())

	_, err = spec.WithProceedOnCodes([]int32{9})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown TC action code 9")
}

// Priority is stored verbatim, matching Rust's raw ordering: 0 is a
// real priority that sorts before positive values, and only a negative
// value is rejected.
func TestTCXAttachSpecPriorityParsing(t *testing.T) {
	t.Parallel()

	omitted, err := NewTCXAttachSpec(1, "eth0", TCDirectionIngress, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, omitted.Priority())

	explicit, err := NewTCXAttachSpec(1, "eth0", TCDirectionIngress, 25)
	require.NoError(t, err)
	assert.Equal(t, 25, explicit.Priority())

	_, err = NewTCXAttachSpec(1, "eth0", TCDirectionIngress, -1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAttachSpec)
	assert.Contains(t, err.Error(), "priority must be non-negative")
}

// uprobe pid and containerPid are parsed at construction: 0 means
// unset, a positive value is kept, and a negative value is rejected.
func TestUprobeAttachSpecPidParsing(t *testing.T) {
	t.Parallel()

	unset, err := NewUprobeAttachSpec(1, "/bin/example", 0, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, unset.Pid())
	assert.EqualValues(t, 0, unset.ContainerPid())

	set, err := NewUprobeAttachSpec(1, "/bin/example", 42, 7)
	require.NoError(t, err)
	assert.EqualValues(t, 42, set.Pid())
	assert.EqualValues(t, 7, set.ContainerPid())

	_, err = NewUprobeAttachSpec(1, "/bin/example", -1, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAttachSpec)
	assert.Contains(t, err.Error(), "pid must be non-negative")

	_, err = NewUprobeAttachSpec(1, "/bin/example", 0, -1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAttachSpec)
	assert.Contains(t, err.Error(), "container pid must be non-negative")
}
