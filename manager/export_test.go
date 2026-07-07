package manager

import (
	"context"

	"github.com/bpfman/bpfman/lock"
)

// NewExecutorForTest exposes the unexported newExecutor for black-box tests.
var NewExecutorForTest = newExecutor

// ReapDeadProgramRecordsForTest exposes the unexported reap for black-box
// tests, acquiring the writer lock the way the lockless load path does.
func (m *Manager) ReapDeadProgramRecordsForTest(ctx context.Context) error {
	return lock.Run(ctx, m.rt.Layout().LockPath(), func(runCtx context.Context, writeLock lock.WriterScope) error {
		return m.reapDeadProgramRecords(runCtx, writeLock)
	})
}
