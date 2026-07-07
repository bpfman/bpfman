package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func callProgInfo(t *testing.T, args ...string) (runtime.Value, error) {
	out := make([]runtime.Arg, len(args))
	for i, a := range args {
		out[i] = runtime.WordArg{Text: a}
	}
	return handleProgInfo(driver.Ctx{Ctx: t.Context(), Cmd: "proginfo", Args: out})
}

func TestHandleProgInfo_Usage(t *testing.T) {
	t.Parallel()
	_, err := callProgInfo(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage: proginfo id N | proginfo pinned PATH")
}

func TestHandleProgInfo_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	_, err := callProgInfo(t, "bogus", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown subcommand "bogus"`)
	assert.Contains(t, err.Error(), "id, pinned")
}

func TestHandleProgInfo_NonNumericID(t *testing.T) {
	t.Parallel()
	_, err := callProgInfo(t, "id", "abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}
