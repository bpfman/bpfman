package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func callLinkInfo(t *testing.T, args ...string) (runtime.Value, error) {
	out := make([]runtime.Arg, len(args))
	for i, a := range args {
		out[i] = runtime.WordArg{Text: a}
	}
	return handleLinkInfo(driver.Ctx{Ctx: t.Context(), Cmd: "linkinfo", Args: out})
}

func TestHandleLinkInfo_Usage(t *testing.T) {
	t.Parallel()
	_, err := callLinkInfo(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage: linkinfo id N")
}

func TestHandleLinkInfo_RequiresIdKeyword(t *testing.T) {
	t.Parallel()
	_, err := callLinkInfo(t, "show", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage: linkinfo id N")
}

func TestHandleLinkInfo_NonNumericID(t *testing.T) {
	t.Parallel()
	_, err := callLinkInfo(t, "id", "abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}
