package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
)

func TestDefaultTCProceedOn(t *testing.T) {
	t.Parallel()

	want, err := dispatcher.ProceedOnMask(dispatcher.DispatcherTypeTCIngress, bpfman.TCActionPipe.Int32(), bpfman.TCActionDispatcherReturn.Int32())
	require.NoError(t, err)
	assert.Equal(t, want, DefaultTCProceedOn)
}

func TestTCProceedOnMaskSingleAction(t *testing.T) {
	t.Parallel()

	got, err := dispatcher.ProceedOnMask(dispatcher.DispatcherTypeTCIngress, 0)
	require.NoError(t, err)
	assert.Equal(t, uint32(1<<1), got)

	got, err = dispatcher.ProceedOnMask(dispatcher.DispatcherTypeTCIngress, 3)
	require.NoError(t, err)
	assert.Equal(t, uint32(1<<4), got)
}

func TestTCProceedOnMaskSupportsUnspec(t *testing.T) {
	t.Parallel()

	got, err := dispatcher.ProceedOnMask(dispatcher.DispatcherTypeTCIngress, -1)
	require.NoError(t, err)
	assert.Equal(t, uint32(1<<0), got)
}

func TestTCProceedOnMaskRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	_, err := dispatcher.ProceedOnMask(dispatcher.DispatcherTypeTCIngress, 32)
	require.Error(t, err)
}
