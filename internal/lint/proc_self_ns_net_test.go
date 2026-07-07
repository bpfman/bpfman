// Package lint hosts repository-wide invariants enforced as Go
// tests. These tests have no production callers; they exist to
// fail builds when a contributor reintroduces a hazard the rest
// of the codebase has been shaped to avoid.
package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoDirectProcSelfNsNetReads asserts that no code outside
// ns/netns/ calls a stat or open primitive with the literal
// argument "/proc/self/ns/net".
//
// Why this is forbidden: that path resolves per-thread on Linux.
// Each OS thread has its own /proc/self/ns/net symlink target,
// and a goroutine that has called setns(CLONE_NEWNET) on its
// locked thread will read the new netns's inode rather than the
// process's startup netns. Under concurrent goroutines that
// switch and don't restore -- typically an upstream library bug
// -- any goroutine that lands on a poisoned thread and reads
// /proc/self/ns/net silently gets the wrong answer.
//
// All callers that want the process startup nsid go through
// ns/netns.CurrentNSID, which captures the value once on a
// pristine thread under a sync.Once and returns it for the
// lifetime of the process. Callers that genuinely need a
// specific netns's inode pass the explicit path to
// ns/netns.NSID, which stat's that path independent of thread
// state.
//
// The check is AST-aware: comments mentioning the path are
// fine. Only call expressions whose first argument is the
// literal "/proc/self/ns/net" trigger a failure.
func TestNoDirectProcSelfNsNetReads(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	exemptDir := filepath.Join(root, "ns", "netns")

	// Path-taking primitives that could read the namespace
	// symlink. The list is deliberately broader than today's
	// usage so a contributor reaching for any of them with the
	// forbidden literal trips the check.
	forbidden := map[string]struct{}{
		"os.Open":       {},
		"os.OpenFile":   {},
		"os.Stat":       {},
		"os.Lstat":      {},
		"os.Readlink":   {},
		"syscall.Open":  {},
		"syscall.Stat":  {},
		"syscall.Lstat": {},
		"unix.Open":     {},
		"unix.Stat":     {},
		"unix.Lstat":    {},
	}

	const target = "/proc/self/ns/net"

	type violation struct {
		file string
		line int
		call string
	}
	var violations []violation

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "node_modules", ".direnv", ".codex":
				return filepath.SkipDir
			}
			if path == exemptDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			// A file that does not parse is the compiler's
			// problem, not this lint's. Skip rather than
			// fail the lint with a parse-error noise.
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := callIdent(call.Fun)
			if fn == "" {
				return true
			}
			if _, forbid := forbidden[fn]; !forbid {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}

			if val != target {
				return true
			}
			pos := fset.Position(call.Pos())
			rel, _ := filepath.Rel(root, pos.Filename)
			violations = append(violations, violation{
				file: rel,
				line: pos.Line,
				call: fn,
			})
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk repo: %v", walkErr)
	}

	if len(violations) > 0 {
		var msg strings.Builder
		fmt.Fprintf(&msg, "found %d direct read(s) of %q outside ns/netns/:\n", len(violations), target)
		for _, v := range violations {
			fmt.Fprintf(&msg, "  %s:%d  %s(...)\n", v.file, v.line, v.call)
		}
		fmt.Fprint(&msg, "\nUse ns/netns.CurrentNSID() instead. ")
		fmt.Fprint(&msg, "It returns the cached process-startup value, captured once on a pristine thread; ")
		fmt.Fprint(&msg, "direct reads of /proc/self/ns/net are per-thread and unsafe under concurrent setns.")
		t.Fatal(msg.String())
	}
}

// callIdent returns the dotted name of a CallExpr's function
// position, e.g. "os.Open" for os.Open(...). Returns "" if the
// function is not a simple identifier or a single-level selector
// expression -- method calls on local values, function values
// stored in variables, etc. fall through, which is acceptable for
// this check: the forbidden primitives are always referenced via
// their package path.
func callIdent(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return x.Name + "." + f.Sel.Name
	}
	return ""
}

// moduleRoot walks up from the test's working directory until it
// finds the directory containing go.mod, and returns that path.
// Used so the AST walk can scan the whole module regardless of
// which package this test is invoked from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", cwd)
		}
		dir = parent
	}
}
