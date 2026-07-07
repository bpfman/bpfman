// Whole-input inspection mechanism shared by the embedding CLI's
// parse-only modes. These pipelines slurp one full source unit,
// parse it as a whole program, and render either the AST or the
// canonical lowered IR without involving the manager or runtime.

package driver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/lower"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// ASTInput is the framework half of the --ast pipeline: slurp the
// whole input from r, parse it as one program, and dump the AST to
// out. Errors are written to errOut with a file:line prefix; returns
// true when any error was emitted so the embedding CLI can exit
// non-zero without printing a second message.
func ASTInput(r LineReader, out io.Writer, errOut io.Writer, file string) bool {
	return renderWholeProgramInput(r, out, errOut, file, func(out io.Writer, prog *syntax.Program) error {
		return syntax.DumpAST(out, prog)
	})
}

// LoweredInput is the framework half of the --lowered pipeline:
// slurp the whole input from r, parse it as one program, lower it to
// the canonical IR, and dump the lowered form to out. Errors are
// written to errOut with a file:line prefix; returns true when any
// error was emitted so the embedding CLI can exit non-zero without
// printing a second message.
func LoweredInput(r LineReader, out io.Writer, errOut io.Writer, file string) bool {
	return renderWholeProgramInput(r, out, errOut, file, func(out io.Writer, prog *syntax.Program) error {
		lp, err := lower.Lower(prog)
		if err != nil {
			return err
		}

		return ir.Dump(out, lp)
	})
}

// SymbolsInput is the framework half of the --symbols pipeline:
// slurp the whole input, extract the symbols visible from the queried
// source unit, and render a stable JSON payload for editor tooling.
// Stdin-backed queries stay local because there is no reliable import
// base. File-backed queries include direct imported top-level defs and
// report import failures in the JSON document without suppressing the
// root file's own symbols.
func SymbolsInput(r LineReader, out io.Writer, errOut io.Writer, file string) bool {
	src, err := SlurpReader(r)
	if err != nil {
		fmt.Fprintf(errOut, "%s: %v\n", file, err)
		return true
	}

	if strings.TrimSpace(src) == "" {
		return false
	}

	reportErr := func(err error) bool {
		loc := SourceLoc{File: file, Line: 1}
		fmt.Fprintf(errOut, "%serror: %v\n", loc, err)
		return true
	}

	prog, parseErr := parseProgram(file, src)
	if parseErr != nil {
		return reportErr(parseErr)
	}

	doc := localSymbolsDocument(file, prog)
	if symbolsInputHasImportBase(file) {
		doc = visibleSymbolsDocument(file, prog)
	}
	if err := writeSymbolsJSON(out, doc); err != nil {
		return reportErr(err)
	}

	return false
}

// FormatInput is the framework half of the fmt pipeline: slurp the
// whole input, parse it without import expansion, and render the
// canonical source form to out. Import statements are source, not
// execution-time definitions, so formatting preserves them rather
// than splicing imported libraries into the result.
func FormatInput(r LineReader, out io.Writer, errOut io.Writer, file string) bool {
	formatted, hadIssue := FormatInputString(r, errOut, file)
	if hadIssue {
		return true
	}

	_, err := io.WriteString(out, formatted)
	if err != nil {
		fmt.Fprintf(errOut, "%s: %v\n", file, err)
		return true
	}

	return false
}

// FormatInputString is FormatInput's string-returning form, used by
// the CLI's write-back mode.
func FormatInputString(r LineReader, errOut io.Writer, file string) (string, bool) {
	src, err := SlurpReader(r)
	if err != nil {
		fmt.Fprintf(errOut, "%s: %v\n", file, err)
		return "", true
	}

	if strings.TrimSpace(src) == "" {
		return "", false
	}
	reportErr := func(err error) (string, bool) {
		loc := SourceLoc{File: file, Line: 1}
		fmt.Fprintf(errOut, "%serror: %v\n", loc, err)
		return "", true
	}
	prog, parseErr := parseProgram(file, src)
	if parseErr != nil {
		return reportErr(parseErr)
	}

	originalLowered, err := loweredDump(prog)
	if err != nil {
		return reportErr(err)
	}

	formatted := syntax.FormatSource(src, prog)
	formattedProg, parseErr := parseProgram(file, formatted)
	if parseErr != nil {
		return reportErr(parseErr)
	}

	formattedLowered, err := loweredDump(formattedProg)
	if err != nil {
		return reportErr(err)
	}

	if originalLowered != formattedLowered {
		return reportErr(fmt.Errorf("internal formatter error: formatted source changed lowered form"))
	}

	return formatted, false
}

func loweredDump(prog *syntax.Program) (string, error) {
	lp, err := lower.Lower(prog)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := ir.Dump(&out, lp); err != nil {
		return "", err
	}

	return out.String(), nil
}

type symbolsDocument struct {
	Version int                 `json:"version"`
	File    string              `json:"file"`
	Symbols []symbolObject      `json:"symbols"`
	Errors  []symbolErrorObject `json:"errors"`
}

type symbolObject struct {
	Name  string      `json:"name"`
	Kind  string      `json:"kind"`
	Def   locObject   `json:"def"`
	Scope scopeObject `json:"scope"`
}

type scopeObject struct {
	File  string    `json:"file"`
	Start posObject `json:"start"`
	End   posObject `json:"end"`
}

type locObject struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type posObject struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type symbolErrorObject struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

func localSymbolsDocument(file string, prog *syntax.Program) symbolsDocument {
	doc := symbolsDocument{
		Version: 1,
		File:    file,
		Symbols: symbolObjects(file, syntax.Symbols(prog)),
		Errors:  []symbolErrorObject{},
	}
	return doc
}

func visibleSymbolsDocument(file string, prog *syntax.Program) symbolsDocument {
	doc := localSymbolsDocument(file, prog)
	rootScope := syntax.ProgramScope(prog)
	visibleDefs := cloneDefInfo(nil)
	recordTopLevelDefInfo(visibleDefs, prog.Stmts)
	_, _ = expandDirectImports(
		file,
		"",
		prog,
		visibleDefs,
		nil,
		func(imp directImport) {
			doc.Symbols = append(doc.Symbols, importedDefSymbols(file, rootScope, imp.Prog.Stmts)...)
		},
		func(err error) bool {
			doc.Errors = append(doc.Errors, symbolError(file, err))
			return true
		},
	)
	return doc
}

func symbolsInputHasImportBase(file string) bool {
	switch file {
	case "", "-", "<stdin>":
		return false
	default:
		return true
	}
}

func importedDefSymbols(fallbackFile string, scope source.Span, stmts []syntax.Stmt) []symbolObject {
	var out []symbolObject
	for _, st := range stmts {
		def, ok := st.(*syntax.DefStmt)
		if !ok {
			continue
		}
		out = append(out, symbolObject{
			Name:  def.Name.Text,
			Kind:  string(syntax.SymbolDef),
			Def:   locDTO(def.Name.Pos),
			Scope: scopeDTOWithFallback(scope, fallbackFile),
		})
	}
	return out
}

func symbolObjects(fallbackFile string, symbols []syntax.Symbol) []symbolObject {
	out := make([]symbolObject, 0, len(symbols))
	for _, sym := range symbols {
		out = append(out, symbolObject{
			Name:  sym.Name,
			Kind:  string(sym.Kind),
			Def:   locDTOWithFallback(sym.Def, fallbackFile),
			Scope: scopeDTOWithFallback(sym.Scope, fallbackFile),
		})
	}
	return out
}

func symbolError(fallbackFile string, err error) symbolErrorObject {
	var se *syntax.SyntaxError
	if errors.As(err, &se) {
		file := se.Span.Pos.File
		if file == "" {
			file = fallbackFile
		}
		msg := se.Msg
		if msg == "" {
			msg = se.Error()
		}
		return symbolErrorObject{
			File:    file,
			Line:    se.Span.Pos.Line,
			Col:     se.Span.Pos.Col,
			Message: msg,
		}
	}

	return symbolErrorObject{File: fallbackFile, Line: 1, Col: 1, Message: err.Error()}
}

func writeSymbolsJSON(out io.Writer, doc symbolsDocument) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func locDTO(pos source.Pos) locObject {
	return locDTOWithFallback(pos, pos.File)
}

func locDTOWithFallback(pos source.Pos, fallbackFile string) locObject {
	file := pos.File
	if file == "" {
		file = fallbackFile
	}
	return locObject{File: file, Line: pos.Line, Col: pos.Col}
}

func scopeDTOWithFallback(scope source.Span, fallbackFile string) scopeObject {
	file := scope.Pos.File
	if file == "" {
		file = fallbackFile
	}
	return scopeObject{
		File:  file,
		Start: posDTO(scope.Pos),
		End:   posDTO(scope.End),
	}
}

func posDTO(pos source.Pos) posObject {
	return posObject{Line: pos.Line, Col: pos.Col}
}

func renderWholeProgramInput(r LineReader, out io.Writer, errOut io.Writer, file string, render func(io.Writer, *syntax.Program) error) bool {
	src, err := SlurpReader(r)
	if err != nil {
		fmt.Fprintf(errOut, "%s: %v\n", file, err)
		return true
	}

	if strings.TrimSpace(src) == "" {
		return false
	}

	reportErr := func(err error) bool {
		loc := SourceLoc{File: file, Line: 1}
		fmt.Fprintf(errOut, "%serror: %v\n", loc, err)
		return true
	}

	prog, parseErr := ParseAndExpand(file, src)
	if parseErr != nil {
		return reportErr(parseErr)
	}

	if renderErr := render(out, prog); renderErr != nil {
		return reportErr(renderErr)
	}

	return false
}
