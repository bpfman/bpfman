package main

import (
	"errors"
	"io"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// failingWriter is an io.Writer that succeeds for the first N bytes, then
// fails with a chosen error. It can also simulate short writes with nil
// error (optional).
type failingWriter struct {
	// budget is how many bytes may be successfully written before we fail.
	// If budget is 0, the next Write fails immediately.
	budget int

	// failErr is returned once the budget is exhausted.
	failErr error

	// shortWriteEvery, if >0, simulates a short write with nil error
	// every Nth call (useful for testing io.ErrShortWrite handling).
	shortWriteEvery int

	writes int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++

	// Optional: simulate short write without error.
	if w.shortWriteEvery > 0 && (w.writes%w.shortWriteEvery) == 0 {
		if len(p) == 0 {
			return 0, nil
		}
		return 1, nil
	}

	if w.budget <= 0 {
		return 0, w.failErr
	}

	if len(p) <= w.budget {
		w.budget -= len(p)
		return len(p), nil
	}

	// Partial write then fail.
	n := w.budget
	w.budget = 0
	return n, w.failErr
}

var _ io.Writer = (*failingWriter)(nil)

func TestFailingWriter_FailsImmediately(t *testing.T) {
	t.Parallel()

	w := &failingWriter{
		budget:  0,
		failErr: syscall.ENOSPC,
	}
	n, err := w.Write([]byte("hello"))
	require.Equal(t, 0, n)
	require.True(t, errors.Is(err, syscall.ENOSPC))
}

func TestFailingWriter_PartialThenFail(t *testing.T) {
	t.Parallel()

	w := &failingWriter{
		budget:  3,
		failErr: syscall.ENOSPC,
	}
	n, err := w.Write([]byte("hello"))
	require.Equal(t, 3, n)
	require.True(t, errors.Is(err, syscall.ENOSPC))
}

func TestFailingWriter_SucceedsWithinBudget(t *testing.T) {
	t.Parallel()

	w := &failingWriter{
		budget:  10,
		failErr: syscall.ENOSPC,
	}
	n, err := w.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
}

func TestFailingWriter_ShortWriteNilError(t *testing.T) {
	t.Parallel()

	w := &failingWriter{
		budget:          10,
		failErr:         syscall.ENOSPC,
		shortWriteEvery: 1, // every write is a short write
	}
	n, err := w.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 1, n) // short write with nil error
}

func TestCLIWriteOut_PropagatesENOSPC(t *testing.T) {
	t.Parallel()

	c := &cli.CLI{Out: &failingWriter{budget: 0, failErr: syscall.ENOSPC}}
	err := c.WriteOut([]byte("x"))
	require.True(t, errors.Is(err, syscall.ENOSPC))
}

func TestCLIWriteOut_TreatsShortWriteAsError(t *testing.T) {
	t.Parallel()

	c := &cli.CLI{Out: &failingWriter{budget: 10, failErr: syscall.ENOSPC, shortWriteEvery: 1}}
	err := c.WriteOut([]byte("hello"))
	require.ErrorIs(t, err, io.ErrShortWrite)
}

func TestCLIWriteOut_PartialThenFailReturnsENOSPC(t *testing.T) {
	t.Parallel()

	c := &cli.CLI{Out: &failingWriter{budget: 3, failErr: syscall.ENOSPC}}
	err := c.WriteOut([]byte("hello"))
	require.True(t, errors.Is(err, syscall.ENOSPC))
}

func TestCLIPrintOut_PropagatesError(t *testing.T) {
	t.Parallel()

	c := &cli.CLI{Out: &failingWriter{budget: 0, failErr: syscall.EPIPE}}
	err := c.PrintOut("test output")
	require.True(t, errors.Is(err, syscall.EPIPE))
}

func TestCLIPrintOutf_PropagatesError(t *testing.T) {
	t.Parallel()

	c := &cli.CLI{Out: &failingWriter{budget: 0, failErr: syscall.EPIPE}}
	err := c.PrintOutf("test %s", "output")
	require.True(t, errors.Is(err, syscall.EPIPE))
}
