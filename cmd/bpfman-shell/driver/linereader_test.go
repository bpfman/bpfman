package driver

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScannerReader(t *testing.T) {
	t.Parallel()

	input := "line one\nline two\n"
	lr := NewScannerReader(strings.NewReader(input), nil)
	defer lr.Close()

	s, err := lr.Readline()
	require.NoError(t, err)
	assert.Equal(t, "line one", s)

	s, err = lr.Readline()
	require.NoError(t, err)
	assert.Equal(t, "line two", s)

	_, err = lr.Readline()
	assert.ErrorIs(t, err, io.EOF)
}
