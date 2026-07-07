package driver

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatInput_CanonicalisesSourceWithoutImportExpansion(t *testing.T) {
	t.Parallel()

	src := "# Header prose.\n\nimport ./lib.bpfman\nif not (null $x) { print ($x + 1) }\n"
	r := NewScannerReader(strings.NewReader(src), nil)
	var out, errOut bytes.Buffer

	if FormatInput(r, &out, &errOut, "main.bpfman") {
		t.Fatalf("FormatInput reported issue: %s", errOut.String())
	}

	want := "# Header prose.\n\nimport ./lib.bpfman\nif not null $x {\n    print ($x + 1)\n}\n"
	if got := out.String(); got != want {
		t.Fatalf("FormatInput() =\n%s\nwant:\n%s", got, want)
	}
}

func TestSymbolsInput_StdinRendersLocalJSON(t *testing.T) {
	t.Parallel()

	src := "import ./lib.bpfman\ndef helper(x) {\n  let y = $x\n  return $y\n}\n"
	r := NewScannerReader(strings.NewReader(src), nil)
	var out, errOut bytes.Buffer

	if SymbolsInput(r, &out, &errOut, "-") {
		t.Fatalf("SymbolsInput reported issue: %s", errOut.String())
	}

	want := `{
  "version": 1,
  "file": "-",
  "symbols": [
    {
      "name": "helper",
      "kind": "def",
      "def": {
        "file": "-",
        "line": 2,
        "col": 5
      },
      "scope": {
        "file": "-",
        "start": {
          "line": 1,
          "col": 1
        },
        "end": {
          "line": 5,
          "col": 2
        }
      }
    },
    {
      "name": "x",
      "kind": "param",
      "def": {
        "file": "-",
        "line": 2,
        "col": 12
      },
      "scope": {
        "file": "-",
        "start": {
          "line": 2,
          "col": 1
        },
        "end": {
          "line": 5,
          "col": 2
        }
      }
    },
    {
      "name": "y",
      "kind": "let",
      "def": {
        "file": "-",
        "line": 3,
        "col": 7
      },
      "scope": {
        "file": "-",
        "start": {
          "line": 2,
          "col": 1
        },
        "end": {
          "line": 5,
          "col": 2
        }
      }
    }
  ],
  "errors": []
}
`
	if got := out.String(); got != want {
		t.Fatalf("SymbolsInput() =\n%s\nwant:\n%s", got, want)
	}
}

func TestSymbolsInput_FileQueryIncludesDirectImportedDefs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	main := filepath.Join(dir, "main.bpfman")
	lib := filepath.Join(dir, "lib.bpfman")
	require.NoError(t, os.WriteFile(lib, []byte("def imported(x) {\n  return $x\n}\n"), 0o644))
	src := "import ./lib.bpfman\nlet local = 1\n"
	require.NoError(t, os.WriteFile(main, []byte(src), 0o644))
	r := NewScannerReader(strings.NewReader(src), nil)
	var out, errOut bytes.Buffer

	assert.False(t, SymbolsInput(r, &out, &errOut, main), "unexpected diagnostics: %s", errOut.String())

	doc := decodeSymbolsDocument(t, out.String())
	require.Len(t, doc.Symbols, 2)
	assert.Empty(t, doc.Errors)
	assert.Equal(t, symbolObject{
		Name: "local",
		Kind: "let",
		Def:  locObject{File: main, Line: 2, Col: 5},
		Scope: scopeObject{
			File:  main,
			Start: posObject{Line: 1, Col: 1},
			End:   posObject{Line: 2, Col: 14},
		},
	}, doc.Symbols[0])
	assert.Equal(t, symbolObject{
		Name: "imported",
		Kind: "def",
		Def:  locObject{File: lib, Line: 1, Col: 5},
		Scope: scopeObject{
			File:  main,
			Start: posObject{Line: 1, Col: 1},
			End:   posObject{Line: 2, Col: 14},
		},
	}, doc.Symbols[1])
}

func TestSymbolsInput_FileQueryReportsBrokenImportButKeepsLocalSymbols(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	main := filepath.Join(dir, "main.bpfman")
	src := "import ./missing.bpfman\nlet local = 1\n"
	require.NoError(t, os.WriteFile(main, []byte(src), 0o644))
	r := NewScannerReader(strings.NewReader(src), nil)
	var out, errOut bytes.Buffer

	assert.False(t, SymbolsInput(r, &out, &errOut, main), "unexpected diagnostics: %s", errOut.String())

	doc := decodeSymbolsDocument(t, out.String())
	require.Len(t, doc.Symbols, 1)
	assert.Equal(t, "local", doc.Symbols[0].Name)
	require.Len(t, doc.Errors, 1)
	assert.Equal(t, main, doc.Errors[0].File)
	assert.Equal(t, 1, doc.Errors[0].Line)
	assert.Equal(t, 8, doc.Errors[0].Col)
	assert.Contains(t, doc.Errors[0].Message, "missing.bpfman")
}

func decodeSymbolsDocument(t *testing.T, src string) symbolsDocument {
	t.Helper()
	var doc symbolsDocument
	require.NoError(t, json.Unmarshal([]byte(src), &doc))
	return doc
}
