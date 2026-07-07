package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

// getProgram fetches a program from the store, translating
// platform.ErrRecordNotFound into the domain error ErrProgramNotFound.
func (m *Manager) getProgram(ctx context.Context, id kernel.ProgramID) (bpfman.ProgramRecord, error) {
	rec, err := m.store.Get(ctx, id)
	if err == nil {
		return rec, nil
	}
	if errors.Is(err, platform.ErrRecordNotFound) {
		return rec, bpfman.ErrProgramNotFound{ID: id}
	}
	return rec, fmt.Errorf("get program %d: %w", id, err)
}

// getLink fetches a link from the store, translating
// platform.ErrRecordNotFound into the domain error ErrLinkNotFound.
func (m *Manager) getLink(ctx context.Context, id bpfman.LinkID) (bpfman.LinkRecord, error) {
	rec, err := m.store.GetLink(ctx, id)
	if err == nil {
		return rec, nil
	}
	if errors.Is(err, platform.ErrRecordNotFound) {
		return rec, bpfman.ErrLinkNotFound{LinkID: id}
	}
	return rec, fmt.Errorf("get link %d: %w", id, err)
}
