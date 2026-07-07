package bpfmanbuiltin

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// TestBpfmanCommandResultContract locks the result-type contract for
// every typed bpfman command the static checker seals: the shape the
// checker infers for `<noun> <verb>` must equal the shape derived from
// the type the external dispatch decoder produces for that command. It
// guards against the static shape and the external decoder drifting
// apart, for example one side moving to ProgramListResult while the
// other still produces the old type.
//
// Only the program and link commands are covered: those are the ones
// the static checker gives a sealed typed shape. Image and dispatcher
// results are deliberately left unsealed by the checker, so there is
// no shape to agree with.
func TestBpfmanCommandResultContract(t *testing.T) {
	t.Parallel()

	link := bpfman.Link{Record: bpfman.LinkRecord{Kind: bpfman.LinkKindXDP, Details: bpfman.XDPDetails{}}}
	prog := bpfman.Program{Record: bpfman.ProgramRecord{Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP)}}

	cases := []struct {
		name   string
		words  []string
		sample any
	}{
		{"program load", []string{"program", "load"}, bpfman.LoadResult{Programs: []bpfman.Program{}}},
		{"program get", []string{"program", "get"}, prog},
		{"program list", []string{"program", "list"}, bpfman.ProgramListResult{Programs: []bpfman.ProgramListEntry{}}},
		{"link get", []string{"link", "get"}, link},
		{"link attach", []string{"link", "attach"}, link},
		{"link list", []string{"link", "list"}, bpfman.LinkListResult{Links: []bpfman.LinkRecord{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			static, ok := semantics.InferBindShape("bpfman", literalExprs(tc.words))
			require.True(t, ok, "bpfman bind shape must be registered")

			stdout, err := json.Marshal(tc.sample)
			require.NoError(t, err)
			v, err := decodeBpfmanResult(wordArgs(tc.words), stdout)
			require.NoError(t, err)
			require.NotNil(t, v.Origin(), "decoded value must retain its origin type")

			external := semantics.ShapeFromType(reflect.TypeOf(v.Origin()))
			assert.Equal(t, static, external, "static checker shape and external decoder type disagree for %q", tc.name)
		})
	}
}

func literalExprs(words []string) []syntax.Expr {
	exprs := make([]syntax.Expr, len(words))
	for i, w := range words {
		exprs[i] = &syntax.LiteralExpr{Text: w}
	}
	return exprs
}

func wordArgs(words []string) []runtime.Arg {
	args := make([]runtime.Arg, len(words))
	for i, w := range words {
		args[i] = word(w)
	}
	return args
}
