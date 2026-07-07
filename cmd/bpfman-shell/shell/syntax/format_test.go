package syntax

import (
	"strings"
	"testing"
)

func TestFormatProgramSource_AllStatementFormsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{"let", `let x = 1`},
		{"let destructure", `let (a _ c) = [1 2 3]`},
		{"let bind", `let out <- exec echo ok`},
		{"guard bind", `guard out <- exec true`},
		{"bind collect", `guard xs <- foreach x in (range 3) {
    echo $x
}`},
		{"defer", `defer exec cleanup`},
		{"if elif else", `if $x == 1 {
    print one
} elif not $ready {
    print waiting
} else {
    print done
}`},
		{"command", `print ok`},
		{"expr stmt", `($x |> jq ".id")`},
		{"foreach", `foreach (name value) in zip [a b] [1 2] {
    continue
    break
}`},
		{"poll retry", `poll timeout 1s every 10ms {
    retry "waiting" unless $ready
}`},
		{"def return", `def id(x) {
    return $x
}`},
		{"require expr", `require $x == 1`},
		{"assert command", `assert not ok exec false`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFormatProgramRoundTrip(t, tc.src)
		})
	}
}

func TestFormatProgramSource_CanonicalisesRedundantParens(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, `if not (null $prog.record.load.image_source) {
  assert not (null $prog.record.load.image_source)
}`)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	got := FormatProgramSource(prog)
	want := `if not null $prog.record.load.image_source {
    assert not null $prog.record.load.image_source
}
`
	if got != want {
		t.Fatalf("FormatProgramSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatProgramSource_ReparseableCompoundPositions(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`let xs = [($x + 1) (not null $y)]`,
		`print ($x + 1) [1 2 3]`,
		`let v = jq "." ($x + 1)`,
		`($x == 1)`,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	formatted := FormatProgramSource(prog)
	if _, err := parseSource(t, formatted); err != nil {
		t.Fatalf("reparse formatted source:\n%s\nerror: %v", formatted, err)
	}
	for _, want := range []string{
		`[($x + 1) (not null $y)]`,
		`print ($x + 1) [1 2 3]`,
		`jq "." ($x + 1)`,
		`($x == 1)`,
	} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted source missing %q:\n%s", want, formatted)
		}
	}
}

func TestFormatSource_PreservesCommentsAndBlankLines(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`# -*- mode: bpfman -*-`,
		`# Header prose.`,
		``,
		`if not (null $prog.record.load.image_source) {`,
		`    # Body prose.`,
		`    assert not (null $prog.record.load.image_source)`,
		`}`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	got := FormatSource(src, prog)
	want := strings.Join([]string{
		`# -*- mode: bpfman -*-`,
		`# Header prose.`,
		``,
		`if not null $prog.record.load.image_source {`,
		`    # Body prose.`,
		`    assert not null $prog.record.load.image_source`,
		`}`,
		``,
	}, "\n")
	if got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatSource_KeepsCommentAfterBlockAtTopLevel(t *testing.T) {
	t.Parallel()

	// A comment that follows a closed block belongs to the outer
	// scope, not inside the block. The input is already canonical, so
	// a correct formatter is idempotent here.
	src := strings.Join([]string{
		`if $x {`,
		`    print $y`,
		`}`,
		``,
		`# top-level comment after the block`,
		`print $z`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	if got := FormatSource(src, prog); got != src {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, src)
	}
}

func TestFormatSource_PreservesCommentOnlyFile(t *testing.T) {
	t.Parallel()

	src := "# licence header\n# copyright 2026\n"
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	if got := FormatSource(src, prog); got != src {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, src)
	}
}

func TestFormatSource_PreservesBlockHeaderLineComment(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`foreach x in (range 3) { # loop note`,
		`    echo $x`,
		`}`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	want := strings.Join([]string{
		`foreach x in range 3 { # loop note`,
		`    echo $x`,
		`}`,
		``,
	}, "\n")
	if got := FormatSource(src, prog); got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func assertFormatProgramRoundTrip(t *testing.T, src string) {
	t.Helper()
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	formatted := FormatProgramSource(prog)
	if _, err := parseSource(t, formatted); err != nil {
		t.Fatalf("reparse formatted source:\n%s\nerror: %v", formatted, err)
	}
}

func TestFormatSource_FormatsCommentedMultilineLeafStatement(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`assert $prog matches {`,
		`  # Keep this comment.`,
		`  id: (1 + 2)`,
		`}`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	want := strings.Join([]string{
		`# Keep this comment.`,
		`assert $prog matches {`,
		`    id: 1 + 2`,
		`}`,
		``,
	}, "\n")
	if got := FormatSource(src, prog); got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatSource_FormatsInlineCommentedLeafStatement(t *testing.T) {
	t.Parallel()

	src := `assert not (null $x) # Keep this attached to the assertion.
`
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	want := `assert not null $x # Keep this attached to the assertion.
`
	if got := FormatSource(src, prog); got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatSource_PreservesCommandContinuationInBind(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`guard loaded <- bpfman program load file \`,
		` testdata/bpf/fentry_counter.bpf.o \`,
		`        --programs "fentry:test_fentry:${fn.name}" \`,
		`  -m owner=e2e \`,
		`      -m purpose=load-and-get`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	want := strings.Join([]string{
		`guard loaded <- bpfman program load file \`,
		`    testdata/bpf/fentry_counter.bpf.o \`,
		`    --programs "fentry:test_fentry:${fn.name}" \`,
		`    -m owner=e2e \`,
		`    -m purpose=load-and-get`,
		``,
	}, "\n")
	if got := FormatSource(src, prog); got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatSource_PreservesCommandContinuationWithoutOptions(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`print alpha \`,
		`  beta \`,
		`    gamma`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	want := strings.Join([]string{
		`print alpha \`,
		`    beta \`,
		`    gamma`,
		``,
	}, "\n")
	if got := FormatSource(src, prog); got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatProgramSource_IndentsAssertMatchesBlock(t *testing.T) {
	t.Parallel()

	src := `assert $prog matches exhaustive {
record: matches {
name: "demo"
}
status: ok
}
`
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	got := FormatProgramSource(prog)
	want := `assert $prog matches exhaustive {
    record: matches {
        name: "demo"
    }
    status: ok
}
`
	if got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatSource_IndentsLetMatchesBlock(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		`let r = ($p matches {`,
		`id: 1`,
		`name: demo`,
		`})`,
		``,
	}, "\n")
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	want := strings.Join([]string{
		`let r = $p matches {`,
		`    id:   1`,
		`    name: demo`,
		`}`,
		``,
	}, "\n")
	if got := FormatSource(src, prog); got != want {
		t.Fatalf("FormatSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatProgramSource_AlignsMatchesBlockEntries(t *testing.T) {
	t.Parallel()

	src := `assert $m matches exhaustive {
id: $want
has_map_extra: false
name: demo
}`
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	got := FormatProgramSource(prog)
	want := `assert $m matches exhaustive {
    id:            $want
    has_map_extra: false
    name:          demo
}
`
	if got != want {
		t.Fatalf("FormatProgramSource() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatProgramSource_DoesNotPadBeforeNestedMatchesBlock(t *testing.T) {
	t.Parallel()

	src := `assert $m matches exhaustive {
handles: matches exhaustive {
pin_path: $pin
map_owner_id: null
}
state: ok
}`
	prog, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	got := FormatProgramSource(prog)
	want := `assert $m matches exhaustive {
    handles: matches exhaustive {
        pin_path:     $pin
        map_owner_id: null
    }
    state:   ok
}
`
	if got != want {
		t.Fatalf("FormatProgramSource() =\n%s\nwant:\n%s", got, want)
	}
}
