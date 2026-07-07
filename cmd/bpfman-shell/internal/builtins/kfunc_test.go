package builtins

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func callKfunc(t *testing.T, args ...runtime.Arg) (runtime.Value, error) {
	return handleKfunc(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "kfunc",
		Args: args,
	})
}

func kfuncWords(args ...string) []runtime.Arg {
	out := make([]runtime.Arg, len(args))
	for i, arg := range args {
		out[i] = runtime.WordArg{Text: arg}
	}
	return out
}

func kfuncArg(f *runtime.Kfunc) runtime.Arg {
	return runtime.StructuredValueArg{
		Name:  "fn",
		Value: runtime.ValueFromKfunc(f),
	}
}

func TestHandleKfunc_NoSubcommand(t *testing.T) {
	t.Parallel()
	_, err := callKfunc(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subcommand required")
}

func TestHandleKfunc_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	_, err := callKfunc(t, kfuncWords("probe")...)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown subcommand "probe"`)
	assert.Contains(t, err.Error(), "acquire, fire, release")
}

func TestHandleKfunc_ReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	fn := &runtime.Kfunc{
		Index:   7,
		Name:    "bpfman_e2e_target_7",
		Trigger: "/sys/kernel/debug/bpfman_e2e/trigger_007",
		Count:   "/sys/kernel/debug/bpfman_e2e/count_007",
	}

	v, err := callKfunc(t, append(kfuncWords("release"), kfuncArg(fn))...)
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginEnvelope, v.Kind())

	v, err = callKfunc(t, append(kfuncWords("release"), kfuncArg(fn))...)
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginEnvelope, v.Kind())
}

func TestHandleKfunc_FireRejectsReleasedHandle(t *testing.T) {
	t.Parallel()
	fn := &runtime.Kfunc{Name: "bpfman_e2e_target_0"}
	fn.MarkReleased()

	_, err := callKfunc(t, append(kfuncWords("fire"), kfuncArg(fn), runtime.WordArg{Text: "1"})...)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has been released")
}

func TestHandleKfunc_ReleaseRejectsWrongValue(t *testing.T) {
	t.Parallel()
	arg := runtime.StructuredValueArg{Name: "x", Value: runtime.StringValue("not a kfunc")}

	_, err := callKfunc(t, append(kfuncWords("release"), arg)...)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a $fn argument")
}

func TestReleaseKfuncSlot_HandlesSlotZero(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "slot-zero-*.lock")
	require.NoError(t, err)

	fn := &runtime.Kfunc{
		Index:   0,
		Name:    "bpfman_e2e_target_0",
		Trigger: "/sys/kernel/debug/bpfman_e2e/trigger_000",
		Count:   "/sys/kernel/debug/bpfman_e2e/count_000",
	}
	require.NoError(t, releaseKfuncSlot(&kfuncLease{slot: 0, lockFile: f}, fn))

	body, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	assert.Contains(t, string(body), `"slot":0`)
	assert.Contains(t, string(body), `"released_at":`)
}

func TestValueFromKfuncShape(t *testing.T) {
	t.Parallel()
	fn := &runtime.Kfunc{
		Index:   3,
		Name:    "bpfman_e2e_target_3",
		Trigger: "/sys/kernel/debug/bpfman_e2e/trigger_003",
		Count:   "/sys/kernel/debug/bpfman_e2e/count_003",
	}
	v := runtime.ValueFromKfunc(fn)

	assert.Equal(t, semantics.OriginKfunc, v.Kind())
	assert.Equal(t, fn, v.Origin())
	assert.Equal(t, "3", valuePathString(t, v, "index"))
	assert.Equal(t, fn.Name, valuePathString(t, v, "name"))
	assert.Equal(t, fn.Trigger, valuePathString(t, v, "trigger"))
	assert.Equal(t, fn.Count, valuePathString(t, v, "count"))
}

func valuePathString(t *testing.T, v runtime.Value, path string) string {
	t.Helper()
	got, err := v.LookupValue("fn", path)
	require.NoError(t, err)
	s, err := got.Scalar()
	require.NoError(t, err)
	return s
}
