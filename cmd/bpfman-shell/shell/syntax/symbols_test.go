package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

func TestSymbols_ExtractsLocatedBindings(t *testing.T) {
	t.Parallel()

	src := `def helper(iface prog) {
  let link = $prog.link
  let out <- print $iface
  return $link
}
let loaded <- bpfman load
foreach item in $loaded {
  let (name value) = $item
  guard done <- print $name
}
`
	prog, err := parseSource(t, src)
	require.NoError(t, err)

	got := Symbols(prog)
	want := []Symbol{
		{
			Name:  "helper",
			Kind:  SymbolDef,
			Def:   pos(1, 5),
			Scope: span(1, 1, 10, 2),
		},
		{
			Name:  "iface",
			Kind:  SymbolParam,
			Def:   pos(1, 12),
			Scope: span(1, 1, 5, 2),
		},
		{
			Name:  "prog",
			Kind:  SymbolParam,
			Def:   pos(1, 18),
			Scope: span(1, 1, 5, 2),
		},
		{
			Name:  "link",
			Kind:  SymbolLet,
			Def:   pos(2, 7),
			Scope: span(1, 1, 5, 2),
		},
		{
			Name:  "out",
			Kind:  SymbolBind,
			Def:   pos(3, 7),
			Scope: span(1, 1, 5, 2),
		},
		{
			Name:  "loaded",
			Kind:  SymbolBind,
			Def:   pos(6, 5),
			Scope: span(1, 1, 10, 2),
		},
		{
			Name:  "item",
			Kind:  SymbolForEach,
			Def:   pos(7, 9),
			Scope: span(7, 1, 10, 2),
		},
		{
			Name:  "name",
			Kind:  SymbolDestructure,
			Def:   pos(8, 8),
			Scope: span(7, 1, 10, 2),
		},
		{
			Name:  "value",
			Kind:  SymbolDestructure,
			Def:   pos(8, 13),
			Scope: span(7, 1, 10, 2),
		},
		{
			Name:  "done",
			Kind:  SymbolBind,
			Def:   pos(9, 9),
			Scope: span(7, 1, 10, 2),
		},
	}
	require.Equal(t, want, got)
}

func TestSymbols_SkipsDiscards(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let (_ value) = $pair\nguard result <- print ok\nforeach (_ item) in $items {\n  print $item\n}\n")
	require.NoError(t, err)

	got := Symbols(prog)
	want := []Symbol{
		{Name: "value", Kind: SymbolDestructure, Def: pos(1, 8), Scope: span(1, 1, 5, 2)},
		{Name: "result", Kind: SymbolBind, Def: pos(2, 7), Scope: span(1, 1, 5, 2)},
		{Name: "item", Kind: SymbolForEach, Def: pos(3, 12), Scope: span(3, 1, 5, 2)},
	}
	require.Equal(t, want, got)
}

func pos(line, col int) source.Pos {
	return source.Pos{Line: line, Col: col}
}

func span(startLine, startCol, endLine, endCol int) source.Span {
	return source.Span{
		Pos: source.Pos{Line: startLine, Col: startCol},
		End: source.Pos{Line: endLine, Col: endCol},
	}
}
