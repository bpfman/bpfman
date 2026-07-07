package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/lock"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWithWriterLockPreservesParentDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		layout, err := fs.New(dir)
		require.NoError(t, err)

		held := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- lock.Run(context.Background(), filepath.Join(dir, ".lock"), func(context.Context, lock.WriterScope) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held

		s := &Server{
			layout: layout,
			logger: testLogger(),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		var ran bool
		_, err = withWriterLock(ctx, s, func(context.Context, lock.WriterScope) (struct{}, error) {
			ran = true
			return struct{}{}, nil
		})

		close(release)
		require.NoError(t, <-done)

		require.False(t, ran)
		require.Equal(t, codes.DeadlineExceeded, status.Code(err))
		require.False(t, strings.Contains(err.Error(), "writer lock"), "parent deadline must not be relabelled as a writer lock timeout")
	})
}
