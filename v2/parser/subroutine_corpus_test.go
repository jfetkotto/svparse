package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

// undefinedUVMMacro matches the one error the corpus's function and task bodies
// still produce: a statement that starts with a UVM macro ("`uvm_info(...)"),
// whose definition lives in a library that is not vendored, so the macro is
// never expanded and its name is left where a statement should be.
var undefinedUVMMacro = regexp.MustCompile(`^expected a statement, found "uvm_[a-z_]+"$`)

// TestSubroutineBodiesOnCorpus parses every function and task body in all
// vendored files: 377 real bodies. Any error the body parser would report
// (kept as suppressed, since reporting is a separate change) other than an
// undefined UVM macro fails the test, and every body must be a well-formed tree.
func TestSubroutineBodiesOnCorpus(t *testing.T) {
	bodies, nonEmpty := 0, 0
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(corpusDir, path)
		rel = filepath.ToSlash(rel)
		toks, _ := preprocessor.Preprocess(rel, string(data), corpusIncludeResolver{})
		p := &parser{toks: toks}
		f := &ast.File{Path: rel}
		f.Decls, _, _ = p.parseBody("")
		ownTypes(f)
		fillExprs(f)

		// Procedural items (initial, always, assign, ...) hold suppressed
		// errors of their own, many from the corpus's deliberately illegal
		// files; only errors outside them come from subroutine bodies.
		var items []ast.Position
		ast.Inspect(f, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.Initial, *ast.Final, *ast.Always, *ast.ContinuousAssign:
				items = append(items, n.Pos())
			}
			return true
		})
		inItem := func(e Error) bool {
			for _, it := range items {
				after := e.Line > it.Line || (e.Line == it.Line && e.Character >= it.Character)
				before := e.Line < it.EndLine || (e.Line == it.EndLine && e.Character <= it.EndCharacter)
				if after && before {
					return true
				}
			}
			return false
		}
		for _, e := range p.suppressed {
			if !undefinedUVMMacro.MatchString(e.Message) && !inItem(e) {
				t.Errorf("%s:%d: unexpected body error: %s", rel, e.Line+1, e.Message)
			}
		}

		usesMacros := strings.Contains(string(data), "`define") || strings.Contains(string(data), "`include")
		ast.Inspect(f, func(n ast.Node) bool {
			var stmts []ast.Stmt
			switch n := n.(type) {
			case *ast.Function:
				if n.Prototype {
					return true
				}
				stmts = n.Stmts
				bodies++
				if len(n.Stmts)+len(n.Decls) > 0 {
					nonEmpty++
				}
			case *ast.Task:
				if n.Prototype {
					return true
				}
				stmts = n.Stmts
				bodies++
				if len(n.Stmts)+len(n.Decls) > 0 {
					nonEmpty++
				}
			default:
				return true
			}
			for _, s := range stmts {
				if usesMacros {
					checkSpansForward(t, rel, s)
				} else {
					checkSpans(t, rel, s)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if bodies < 300 || nonEmpty < 300 {
		t.Fatalf("only %d bodies (%d non-empty) found: the walk is broken", bodies, nonEmpty)
	}
}
