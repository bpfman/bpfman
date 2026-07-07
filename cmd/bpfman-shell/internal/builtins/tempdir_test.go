package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// tempdirCtx is a minimal driver.Ctx for testing the handler.
// Only Ctx and Args are read by HandleTempdir; the other fields
// stay at their zero values.
func tempdirCtx(t *testing.T, args ...runtime.Arg) driver.Ctx {
	return driver.Ctx{Ctx: t.Context(), Args: args}
}

func TestHandleTempdir_CreatesUniqueDirectory(t *testing.T) {
	t.Parallel()

	v, err := HandleTempdir(tempdirCtx(t, runtime.WordArg{Text: "bpfman-test"}))
	require.NoError(t, err)

	path, err := v.LookupValue("wd", "path")
	require.NoError(t, err)
	p, err := path.Scalar()
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(p) })

	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "tempdir result must be a directory")
	assert.True(t, strings.HasPrefix(filepath.Base(p), "bpfman-test."), "tempdir result %q must start with the requested prefix", p)
}

func TestHandleTempdir_DistinctInvocationsAreUnique(t *testing.T) {
	t.Parallel()

	v1, err := HandleTempdir(tempdirCtx(t, runtime.WordArg{Text: "bpfman-test"}))
	require.NoError(t, err)
	p1, err := v1.LookupValue("wd1", "path")
	require.NoError(t, err)
	s1, err := p1.Scalar()
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(s1) })

	v2, err := HandleTempdir(tempdirCtx(t, runtime.WordArg{Text: "bpfman-test"}))
	require.NoError(t, err)
	p2, err := v2.LookupValue("wd2", "path")
	require.NoError(t, err)
	s2, err := p2.Scalar()
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(s2) })

	assert.NotEqual(t, s1, s2, "concurrent invocations must produce distinct paths")
}

func TestHandleTempdir_RejectsMissingPrefix(t *testing.T) {
	t.Parallel()

	_, err := HandleTempdir(tempdirCtx(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PREFIX")
}

func TestHandleTempdir_RejectsEmptyPrefix(t *testing.T) {
	t.Parallel()

	_, err := HandleTempdir(tempdirCtx(t, runtime.QuotedArg{Text: ""}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestHandleTempdir_RejectsExtraArgs(t *testing.T) {
	t.Parallel()

	_, err := HandleTempdir(tempdirCtx(t,
		runtime.WordArg{Text: "bpfman-test"},
		runtime.WordArg{Text: "extra"},
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}
