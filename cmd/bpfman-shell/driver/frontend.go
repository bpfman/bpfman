package driver

import (
	"fmt"
	"maps"
	"path/filepath"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// ParseAndExpand tokenises and parses src, then resolves any
// top-level import statements before returning the resulting
// program.
func ParseAndExpand(file, src string) (*syntax.Program, error) {
	return ParseAndExpandWithBase(file, "", src, 1)
}

// ParseAndExpandWithBase is ParseAndExpand plus an explicit baseDir
// for relative imports when file has no containing script.
func ParseAndExpandWithBase(file, baseDir, src string, startLine int) (*syntax.Program, error) {
	return parseAndExpandWithBaseTrace(file, baseDir, src, startLine, nil, nil)
}

func parseAndExpandWithBaseTrace(file, baseDir, src string, startLine int, visibleDefs map[string]check.DefStaticInfo, traceImport func(source.Pos, string)) (*syntax.Program, error) {
	prog, err := parseProgramAt(file, src, startLine)
	if err != nil {
		return nil, err
	}

	if err := validateImportPlacement(file, prog.Stmts, 0); err != nil {
		return nil, err
	}

	visibleDefs = cloneDefInfo(visibleDefs)
	recordTopLevelDefInfo(visibleDefs, prog.Stmts)

	stmts, err := expandDirectImports(file, baseDir, prog, visibleDefs, traceImport, nil, nil)
	if err != nil {
		return nil, err
	}

	return &syntax.Program{Span: prog.Span, Stmts: stmts}, nil
}

type directImport struct {
	Path string
	Span source.Span
	Prog *syntax.Program
}

func expandDirectImports(file, baseDir string, prog *syntax.Program, visibleDefs map[string]check.DefStaticInfo, traceImport func(source.Pos, string), visitImport func(directImport), tolerateError func(error) bool) ([]syntax.Stmt, error) {
	var out []syntax.Stmt
	handleErr := func(err error) bool {
		if tolerateError != nil && tolerateError(err) {
			return true
		}
		return false
	}

	for _, st := range prog.Stmts {
		path, span, isImport, err := importLiteralPath(st)
		if err != nil {
			err = syntaxError(file, span, err.Error())
			if handleErr(err) {
				continue
			}
			return nil, err
		}

		if !isImport {
			out = append(out, st)
			continue
		}
		if traceImport != nil {
			traceImport(span.Pos, "import "+path)
		}

		resolved := resolveImportPath(file, baseDir, path)
		lr, err := OpenScriptReader(resolved)
		if err != nil {
			err = syntaxError(file, span, err.Error())
			if handleErr(err) {
				continue
			}
			return nil, err
		}

		childSrc, err := SlurpReader(lr)
		lr.Close()
		if err != nil {
			err = syntaxError(file, span, err.Error())
			if handleErr(err) {
				continue
			}
			return nil, err
		}

		lib, err := parseImportProgram(resolved, childSrc, visibleDefs)
		if err != nil {
			if handleErr(err) {
				continue
			}
			return nil, err
		}

		if visitImport != nil {
			visitImport(directImport{Path: resolved, Span: span, Prog: lib})
		}
		out = append(out, lib.Stmts...)
		recordTopLevelDefInfo(visibleDefs, lib.Stmts)
	}
	return out, nil
}

func parseProgram(file, src string) (*syntax.Program, error) {
	return parseProgramAt(file, src, 1)
}

func parseProgramAt(file, src string, startLine int) (*syntax.Program, error) {
	tokens, err := syntax.TokeniseAt(source.Pos{File: file, Line: startLine, Col: 1}, src)
	if err != nil {
		return nil, err
	}

	return parseTokens(tokens)
}

func parseTokens(tokens []syntax.Token) (*syntax.Program, error) {
	prog, err := syntax.Parse(tokens)
	if err != nil {
		return nil, err
	}

	return prog, nil
}

func syntaxError(file string, span source.Span, msg string) error {
	if span.Pos.File == "" {
		span.Pos.File = file
		if span.End.File == "" {
			span.End.File = file
		}
	}
	return &syntax.SyntaxError{
		Span: span,
		Msg:  msg,
	}
}

func cloneDefInfo(src map[string]check.DefStaticInfo) map[string]check.DefStaticInfo {
	out := make(map[string]check.DefStaticInfo, len(src))
	maps.Copy(out, src)
	return out
}

func recordTopLevelDefInfo(dst map[string]check.DefStaticInfo, stmts []syntax.Stmt) {
	for _, st := range stmts {
		def, ok := st.(*syntax.DefStmt)
		if !ok {
			continue
		}
		name := def.Name.Text
		if _, exists := dst[name]; exists {
			continue
		}
		// ReturnShape is intentionally left open here:
		// the expanded-program check sees local def declarations
		// and infers their return shapes directly.
		dst[name] = check.DefStaticInfo{
			Arity:     len(def.Params),
			DeclPos:   def.Name.Pos,
			HasReturn: bodyHasReturn(def.Body),
		}
	}
}

func bodyHasReturn(stmts []syntax.Stmt) bool {
	for _, st := range stmts {
		switch n := st.(type) {
		case *syntax.ReturnStmt:
			return true
		case *syntax.IfStmt:
			if bodyHasReturn(n.Then) {
				return true
			}
			for _, br := range n.Elifs {
				if bodyHasReturn(br.Body) {
					return true
				}
			}
			if bodyHasReturn(n.Else) {
				return true
			}
		case *syntax.ForEachStmt:
			if bodyHasReturn(n.Body) {
				return true
			}
		case *syntax.PollStmt:
			if bodyHasReturn(n.Body) {
				return true
			}
		case *syntax.BindStmt:
			if n.Collect != nil && bodyHasReturn(n.Collect.Body) {
				return true
			}
		}
	}
	return false
}

func validateImportPlacement(file string, stmts []syntax.Stmt, depth int) error {
	for _, st := range stmts {
		_, span, isImport, err := importLiteralPath(st)
		if err != nil {
			return syntaxError(file, span, err.Error())
		}

		if isImport && depth != 0 {
			return syntaxError(file, span, "import must be declared at top level")
		}
		switch n := st.(type) {
		case *syntax.IfStmt:
			if err := validateImportPlacement(file, n.Then, depth+1); err != nil {
				return err
			}

			for _, br := range n.Elifs {
				if err := validateImportPlacement(file, br.Body, depth+1); err != nil {
					return err
				}
			}
			if err := validateImportPlacement(file, n.Else, depth+1); err != nil {
				return err
			}
		case *syntax.ForEachStmt:
			if err := validateImportPlacement(file, n.Body, depth+1); err != nil {
				return err
			}
		case *syntax.PollStmt:
			if err := validateImportPlacement(file, n.Body, depth+1); err != nil {
				return err
			}
		case *syntax.DefStmt:
			if err := validateImportPlacement(file, n.Body, depth+1); err != nil {
				return err
			}
		case *syntax.BindStmt:
			if n.Collect != nil {
				if err := validateImportPlacement(file, n.Collect.Body, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func importLiteralPath(st syntax.Stmt) (string, source.Span, bool, error) {
	cmd, ok := st.(*syntax.CommandStmt)
	if !ok || cmd == nil || len(cmd.Args) == 0 {
		return "", source.Span{}, false, nil
	}

	head, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || head.Quoted || head.Text != "import" {
		return "", source.Span{}, false, nil
	}

	if len(cmd.Args) != 2 {
		return "", cmd.Span, true, fmt.Errorf("import requires exactly one file argument")
	}
	path, ok := cmd.Args[1].(*syntax.LiteralExpr)
	if !ok {
		return "", cmd.Span, true, fmt.Errorf("import path must be a literal file argument")
	}

	return path.Text, path.Span, true, nil
}

// resolveImportPath mirrors import's surface semantics: a relative
// path inside a script resolves against that script's directory,
// while stdin-driven input resolves against baseDir when one is
// available.
func resolveImportPath(file, baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	switch file {
	case "", "-", "<stdin>":
		if baseDir != "" {
			return filepath.Join(baseDir, path)
		}
		return path
	default:
		return filepath.Join(filepath.Dir(file), path)
	}
}

func parseImportProgram(file, src string, visibleDefs map[string]check.DefStaticInfo) (*syntax.Program, error) {
	prog, err := parseProgram(file, src)
	if err != nil {
		return nil, err
	}

	issues := check.CheckImportLibraryWithDefs(prog, visibleDefs)
	if len(issues) > 0 {
		return nil, &syntax.SyntaxError{
			Span:  issues[0].Span,
			Msg:   issues[0].Msg,
			Cause: issues[0],
		}
	}

	return prog, nil
}
