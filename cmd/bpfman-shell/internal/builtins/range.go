// The range pure builtin: 'range N' produces [0, 1, ..., N-1] as
// a sequence the foreach statement can iterate. Mirrors jq's
// range(N) semantics (0-indexed, upper bound exclusive); chosen
// over coreutils' 'seq' (1-indexed, inclusive) for consistency
// with the corpus's existing 'jq "[range(5)]"' idiom.
//
// Arity is fixed at 1: the pure-builtin registry holds a single
// arity per name.
package builtins

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "range",
		Handler:  HandleRange,
		Category: driver.CategoryIO,
		Usage:    "range <integer>",
		Summary:  "Produce the sequence [0, 1, ..., N-1] (jq-style range; assignable).",
		Detail: "Mirrors jq's 'range(N)' semantics: zero-indexed, upper bound " +
			"exclusive. range is a pure builtin and is called from expression " +
			"position only: 'foreach i in (range 5) { ... }' or 'let xs = range 5'. " +
			"The '<-' binding form is rejected because pure builtins produce no " +
			"result envelope. Negative bounds are rejected; the upper limit is " +
			"INT32_MAX to keep pathological scripts loud rather than OOM.",
	})
}

// HandleRange produces a Value carrying [json.Number("0"), ...,
// json.Number("N-1")] so foreach reads it as a list and downstream
// jq projections can apply tonumber without surprise.
//
//	range 0   -> []
//	range 5   -> [0, 1, 2, 3, 4]
//	range -1  -> error
func HandleRange(c driver.Ctx) (runtime.Value, error) {
	args := c.Args
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("range: expected exactly 1 argument, got %d", len(args))
	}
	text := strings.TrimSpace(driver.ArgText(args[0]))
	if text == "" {
		return runtime.Value{}, fmt.Errorf("range: empty argument")
	}
	if strings.HasPrefix(text, "-") {
		return runtime.Value{}, fmt.Errorf("range: negative bound is not allowed (got %q)", text)
	}
	n, err := strconv.ParseUint(text, 0, 64)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("range: invalid integer %q: %w", text, err)
	}

	// Cap at a sane upper bound to avoid pathological scripts
	// freezing the shell on 'range 4294967295'. math.MaxInt32
	// covers every realistic test loop and keeps the failure
	// mode loud and explicit rather than out-of-memory.
	if n > math.MaxInt32 {
		return runtime.Value{}, fmt.Errorf("range: bound %d exceeds the maximum of %d", n, int64(math.MaxInt32))
	}
	out := make([]any, 0, n)
	for i := range n {
		out = append(out, json.Number(strconv.FormatUint(i, 10)))
	}
	return runtime.ValueFromAny(out), nil
}
