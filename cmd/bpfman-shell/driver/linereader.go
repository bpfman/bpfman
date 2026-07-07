package driver

import (
	"bufio"
	"errors"
	"io"
)

// ErrInterrupt is returned when the user presses Ctrl-C.
var ErrInterrupt = errors.New("interrupted")

// LineReader provides line-oriented input for whole-program file
// and stdin execution.
type LineReader interface {
	// Readline returns the next line of input without its trailing
	// newline, or io.EOF once the input is exhausted.
	Readline() (string, error)

	// Close releases the underlying input source, if any.
	Close() error
}

// scannerReader wraps a bufio.Scanner to implement LineReader for
// non-interactive input (files, pipes).
type scannerReader struct {
	scanner *bufio.Scanner
	closer  io.Closer
}

// NewScannerReader creates a LineReader that reads lines from r. If
// closer is non-nil it is closed when the reader is closed; pass nil
// when reading from os.Stdin to avoid closing it.
func NewScannerReader(r io.Reader, closer io.Closer) LineReader {
	return &scannerReader{
		scanner: bufio.NewScanner(r),
		closer:  closer,
	}
}

// Readline returns the next scanned line, or io.EOF at end of input.
func (s *scannerReader) Readline() (string, error) {
	if s.scanner.Scan() {
		return s.scanner.Text(), nil
	}
	if err := s.scanner.Err(); err != nil {
		return "", err
	}

	return "", io.EOF
}

// Close closes the wrapped closer when one was supplied.
func (s *scannerReader) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
