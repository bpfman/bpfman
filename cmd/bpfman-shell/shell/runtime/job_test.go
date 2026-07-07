package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func TestValueFromJob_OriginAndKind(t *testing.T) {
	t.Parallel()

	j := &Job{PID: 4321, Args: []string{"ip", "link", "show"}}
	v := ValueFromJob(j)

	assert.Equal(t, semantics.OriginJob, v.Kind())
	got, ok := v.Origin().(*Job)
	require.True(t, ok, "Origin() should be *Job, got %T", v.Origin())
	assert.Same(t, j, got, "Origin() must be the same pointer so wait/kill mutate the live job")
}

func TestValueFromJob_PIDFieldAccess(t *testing.T) {
	t.Parallel()

	v := ValueFromJob(&Job{PID: 4321})
	got, err := v.Lookup("$job", "pid")
	require.NoError(t, err)
	s, err := got.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "4321", s)
}

func TestValueFromJob_AbsentFieldsErrorRatherThanReturnEmpty(t *testing.T) {
	t.Parallel()

	// 'pid' is in the mirror; 'target_binary' lands there only
	// when the producer (start, fire) populated Job.TargetBinary.
	// The script reaches stdout/stderr/exit-code through 'wait',
	// not through $job.<field>. Confirm that fields not on the
	// mirror error rather than silently returning an empty
	// string; an empty string could flow into a downstream
	// empty target operands undetected.
	v := ValueFromJob(&Job{PID: 99})
	for _, field := range []string{"stdout", "stderr", "exit_code", "killed", "target_binary"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			_, err := v.LookupValue("$job", field)
			require.Error(t, err, "field %q should not resolve on a job without that producer field", field)
		})
	}
}

func TestValueFromJob_TargetBinaryWhenSet(t *testing.T) {
	t.Parallel()

	// fire kinds with NeedsBinary == true populate TargetBinary
	// with /proc/self/exe; plain start populates it with argv[0]
	// as best-effort identity. In both cases the path-walker
	// returns the value the producer set.
	v := ValueFromJob(&Job{PID: 7, TargetBinary: "/usr/local/bin/bpfman-shell"})
	got, err := v.Lookup("$job", "target_binary")
	require.NoError(t, err)
	s, err := got.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "/usr/local/bin/bpfman-shell", s)
}

func TestJob_MarkManagedRoundTrip(t *testing.T) {
	t.Parallel()

	j := &Job{PID: 1}
	assert.False(t, j.IsManaged(), "fresh job is unmanaged")
	j.MarkManaged()
	assert.True(t, j.IsManaged(), "MarkManaged sets Managed")
}

func TestOriginKind_JobString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "job", semantics.OriginJob.String())
}
