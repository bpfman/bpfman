package manager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// TestDetach_NonExistentLink_ReturnsNotFound verifies that:
//
//	Given an empty manager with no links,
//	When I attempt to detach a non-existent link ID,
//	Then the manager returns ErrLinkNotFound as a plain error.
//
// Preflight failures from Detach return plain errors because no
// operation state is created until the plan executes.
func TestDetach_NonExistentLink_ReturnsNotFound(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	err := fix.Detach(ctx, bpfman.LinkID(999))
	require.Error(t, err, "Detach of non-existent link should fail")

	var notFound bpfman.ErrLinkNotFound
	assert.True(t, errors.As(err, &notFound), "expected ErrLinkNotFound, got %T", err)
	assert.Equal(t, bpfman.LinkID(999), notFound.LinkID)
}

// TestDetach_KernelOnlyLinkID_ReturnsNotFound verifies that:
//
//	Given a link that exists in the kernel but is not managed by bpfman,
//	When I attempt to detach using the same numeric value,
//	Then the manager treats that value as a bpfman LinkID and returns
//	ErrLinkNotFound.
//
// Preflight failures from Detach return plain errors because no
// operation state is created until the plan executes.
func TestDetach_KernelOnlyLinkID_ReturnsNotFound(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Inject a link directly into the kernel (bypassing bpfman)
	const kernelOnlyLinkID = 42
	fix.Kernel.InjectKernelLink(kernelOnlyLinkID, bpfman.LinkKindTracepoint)

	err := fix.Detach(ctx, bpfman.LinkID(kernelOnlyLinkID))
	require.Error(t, err, "Detach of kernel-only link should fail")

	var notFound bpfman.ErrLinkNotFound
	assert.True(t, errors.As(err, &notFound), "expected ErrLinkNotFound, got %T", err)
	assert.Equal(t, bpfman.LinkID(kernelOnlyLinkID), notFound.LinkID)
}

// TestUnload_NonExistentProgram_ReturnsNotFound verifies that:
//
//	Given an empty manager with no programs,
//	When I attempt to unload a non-existent program ID,
//	Then the manager returns ErrProgramNotFound.
//
// Preflight failures from Unload return plain errors because no
// operation state is created until the plan executes.
func TestUnload_NonExistentProgram_ReturnsNotFound(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	err := fix.Unload(ctx, 999)
	require.Error(t, err, "Unload of non-existent program should fail")

	var notFound bpfman.ErrProgramNotFound
	assert.True(t, errors.As(err, &notFound), "expected ErrProgramNotFound, got %T", err)
	assert.Equal(t, kernel.ProgramID(999), notFound.ID)
}

// TestUnload_KernelOnlyProgram_ReturnsNotManaged verifies that:
//
//	Given a program that exists in the kernel but is not managed by bpfman,
//	When I attempt to unload it,
//	Then the manager returns ErrProgramNotManaged.
//
// Preflight failures from Unload return plain errors because no
// operation state is created until the plan executes.
func TestUnload_KernelOnlyProgram_ReturnsNotManaged(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	// Inject a program directly into the kernel (bypassing bpfman)
	const kernelOnlyProgID = 42
	fix.Kernel.InjectKernelProgram(kernelOnlyProgID, "orphan_prog", bpfman.ProgramTypeTracepoint)

	err := fix.Unload(ctx, kernelOnlyProgID)
	require.Error(t, err, "Unload of kernel-only program should fail")

	var notManaged bpfman.ErrProgramNotManaged
	assert.True(t, errors.As(err, &notManaged), "expected ErrProgramNotManaged, got %T", err)
	assert.Equal(t, kernel.ProgramID(kernelOnlyProgID), notManaged.ID)
}
