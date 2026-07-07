package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

type guardSnapshot struct {
	Span     string
	Primary  string
	Args     string
	ArgSpans string
	Envelope Envelope
}

// mustReadInt extracts an int from structured runtime payloads
// whose scalar representation may be int, int64, or json.Number.
func mustReadInt(t *testing.T, v any) int {
	t.Helper()
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		n, err := x.Int64()
		require.NoError(t, err)
		return int(n)
	}
	t.Fatalf("expected int-like value, got %T", v)
	return 0
}

func cloneFailureSchedule(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	maps.Copy(out, in)
	return out
}

func scriptedCommandEnvelope(head string) Envelope {
	switch head {
	case "probe":
		return Envelope{ExitCode: 17, Stdout: "probe-stdout", Stderr: "probe-stderr"}
	case "cleanup":
		return Envelope{ExitCode: 29, Stdout: "cleanup-stdout", Stderr: "cleanup-stderr"}
	default:
		return Envelope{ExitCode: 1, Stderr: head + "-stderr"}
	}
}

func snapshotGuardFailure(err error) (guardSnapshot, bool) {
	var gf *GuardFailure
	if !errors.As(err, &gf) {
		return guardSnapshot{}, false
	}
	return guardSnapshot{
		Span:     renderSpan(gf.Span),
		Primary:  gf.Primary,
		Args:     renderArgv(gf.Args),
		ArgSpans: renderArgSpans(gf.Args),
		Envelope: gf.Envelope,
	}, true
}

func renderArgSpans(args []Arg) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, renderSpan(argSpan(arg)))
	}
	return strings.Join(parts, ",")
}

func argSpan(arg Arg) source.Span {
	switch v := arg.(type) {
	case WordArg:
		return v.Span
	case QuotedArg:
		return v.Span
	case ScalarValueArg:
		return v.Span
	case StructuredValueArg:
		return v.Span
	default:
		return source.Span{}
	}
}

func renderSpan(span source.Span) string {
	return fmt.Sprintf("%d:%d-%d:%d", span.Pos.Line, span.Pos.Col, span.End.Line, span.End.Col)
}
