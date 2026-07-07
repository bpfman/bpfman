package syntax

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// SymbolKind classifies a binding site in bpfman-shell source.
type SymbolKind string

const (
	// SymbolDef is a user-defined command name introduced by `def`.
	SymbolDef SymbolKind = "def"

	// SymbolParam is a parameter declared in a `def` parameter list.
	SymbolParam SymbolKind = "param"

	// SymbolLet is a name bound by `let NAME = EXPR`.
	SymbolLet SymbolKind = "let"

	// SymbolDestructure is one name bound by a destructuring `let (a
	// b) = EXPR`.
	SymbolDestructure SymbolKind = "destructure"

	// SymbolBind is the target of a `let NAME <- CMD` or `guard NAME
	// <- CMD` bind.
	SymbolBind SymbolKind = "bind"

	// SymbolForEach is a loop variable introduced by `foreach`.
	SymbolForEach SymbolKind = "foreach"
)

// Symbol is one named binding in a parsed program, as surfaced to
// editor tooling.
type Symbol struct {
	// Name is the bound identifier's spelling.
	Name string

	// Kind classifies the binding site that introduced Name.
	Kind SymbolKind

	// Def is the position of the identifier token that introduced
	// the name.
	Def source.Pos

	// Scope is the source range over which the binding is visible
	// according to the syntax-level scope model.
	Scope source.Span
}

// Symbols extracts every binding and definition symbol from prog in
// source order. It is deliberately pure: callers own all I/O, import
// expansion, filtering, and rendering.
func Symbols(prog *Program) []Symbol {
	if prog == nil {
		return nil
	}
	var out []Symbol
	w := symbolWalker{out: &out}
	root := ProgramScope(prog)
	w.walkStmts(prog.Stmts, root)
	return out
}

type symbolWalker struct {
	out *[]Symbol
}

func (w symbolWalker) emit(id Ident, kind SymbolKind, scope source.Span) {
	if id.Text == "" || id.Text == "_" {
		return
	}
	*w.out = append(*w.out, Symbol{
		Name:  id.Text,
		Kind:  kind,
		Def:   id.Pos,
		Scope: scope,
	})
}

func (w symbolWalker) walkStmts(stmts []Stmt, scope source.Span) {
	for _, st := range stmts {
		w.walkStmt(st, scope)
	}
}

func (w symbolWalker) walkStmt(st Stmt, scope source.Span) {
	switch n := st.(type) {
	case *LetStmt:
		w.emit(n.Name, SymbolLet, scope)
	case *LetDestructureStmt:
		for _, name := range n.Names {
			w.emit(name, SymbolDestructure, scope)
		}
	case *BindStmt:
		w.emit(n.Target, SymbolBind, scope)
		if n.Collect != nil {
			w.walkForEach(n.Collect)
		}
	case *IfStmt:
		w.walkStmts(n.Then, scope)
		for _, br := range n.Elifs {
			w.walkStmts(br.Body, scope)
		}
		w.walkStmts(n.Else, scope)
	case *ForEachStmt:
		w.walkForEach(n)
	case *PollStmt:
		w.walkStmts(n.Body, scope)
	case *DefStmt:
		w.emit(n.Name, SymbolDef, scope)
		defScope := n.Span
		for _, param := range n.Params {
			w.emit(param.Name, SymbolParam, defScope)
		}
		w.walkStmts(n.Body, defScope)
	}
}

func (w symbolWalker) walkForEach(fe *ForEachStmt) {
	feScope := fe.Span
	for _, name := range fe.Names {
		w.emit(name, SymbolForEach, feScope)
	}
	w.walkStmts(fe.Body, feScope)
}

// ProgramScope returns the root visibility span for prog. Program.Span
// records the first token, not the whole file, so the root scope is
// synthesised from line 1, column 1 through the end of the last
// top-level statement.
func ProgramScope(prog *Program) source.Span {
	if prog == nil {
		return source.Span{}
	}
	start := source.Pos{File: prog.Pos.File, Line: 1, Col: 1}
	end := start
	if len(prog.Stmts) > 0 {
		last := stmtSpan(prog.Stmts[len(prog.Stmts)-1])
		if last.End.Line != 0 {
			end = last.End
		} else if last.Pos.Line != 0 {
			end = last.Pos
		}
	}
	return source.Span{Pos: start, End: end}
}

func stmtSpan(st Stmt) source.Span {
	switch n := st.(type) {
	case *LetStmt:
		return n.Span
	case *LetDestructureStmt:
		return n.Span
	case *BindStmt:
		return n.Span
	case *DeferStmt:
		return n.Span
	case *IfStmt:
		return n.Span
	case *CommandStmt:
		return n.Span
	case *ExprStmt:
		return n.Span
	case *ForEachStmt:
		return n.Span
	case *BreakStmt:
		return n.Span
	case *ContinueStmt:
		return n.Span
	case *PollStmt:
		return n.Span
	case *RetryStmt:
		return n.Span
	case *AssertStmt:
		return n.Span
	case *DefStmt:
		return n.Span
	case *ReturnStmt:
		return n.Span
	default:
		return source.Span{}
	}
}
