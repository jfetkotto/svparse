package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

func TestProceduralItemsDump(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"assign", "assign y = a & b;", "(assign"},
		{"assign strength delay", "assign (strong0, pull1) #3 y = a, z = b;", "(assign"},
		{"always_ff", "always_ff @(posedge clk) q <= d;", "(always"},
		{"initial", "initial begin x = 1; #5 x = 0; end", "(initial"},
		{"final", "final $display(\"bye\");", "(final"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := moduleBody(t, c.src)
			if len(body) != 1 {
				t.Fatalf("want 1 item, got %d", len(body))
			}
			if got := ast.Dump(body[0]); !strings.Contains(got, c.want) {
				t.Fatalf("dump missing %q:\n%s", c.want, got)
			}
		})
	}
}

func TestContinuousAssignFields(t *testing.T) {
	body := moduleBody(t, "assign (strong0, pull1) #3 y = a, z = b;")
	ca, ok := body[0].(*ast.ContinuousAssign)
	if !ok {
		t.Fatalf("got %T", body[0])
	}
	if len(ca.DriveStrength) != 2 || ca.DriveStrength[0] != "strong0" || ca.DriveStrength[1] != "pull1" {
		t.Errorf("strength = %v", ca.DriveStrength)
	}
	if len(ca.Delay) != 1 || len(ca.Assigns) != 2 {
		t.Errorf("delay=%d assigns=%d", len(ca.Delay), len(ca.Assigns))
	}
	for _, a := range ca.Assigns {
		if a.Op != "=" || a.LHS == nil || a.RHS == nil {
			t.Errorf("bad assign %+v", a)
		}
	}
}

func TestAlwaysBodyIsTimedStatement(t *testing.T) {
	body := moduleBody(t, "always @(posedge clk) q <= d;")
	a := body[0].(*ast.Always)
	if a.Kind != "always" {
		t.Fatalf("kind %q", a.Kind)
	}
	if _, ok := a.Body.(*ast.TimedStmt); !ok {
		t.Fatalf("body %T", a.Body)
	}
}

// Statement errors inside an item are held back like those in subroutine
// bodies: the file reports nothing the skip itself would not have.
func TestProceduralItemBodyErrorsAreSuppressed(t *testing.T) {
	toks, _ := preprocessor.Preprocess("t.sv", "module m;\ninitial begin x = ; end\nendmodule", nil)
	p := &parser{toks: toks}
	p.parseBody("")
	if len(p.errs) != 0 {
		t.Fatalf("reported: %+v", p.errs)
	}
	if len(p.suppressed) == 0 {
		t.Fatal("expected a suppressed error")
	}
}

func TestProceduralItemPositionSpansKeywordToEnd(t *testing.T) {
	body := moduleBody(t, "initial begin\n x = 1;\nend")
	pos := body[0].Pos()
	if pos.Line != 1 || pos.Character != 0 || pos.EndLine != 3 || pos.EndCharacter != 3 {
		t.Fatalf("pos %+v", pos)
	}
}

// TestProceduralItemsOnCorpus checks every initial/always/final body in the
// vendored files is a well-formed tree and that items exist in bulk.
func TestProceduralItemsOnCorpus(t *testing.T) {
	items := 0
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
		f, _ := Parse(rel, toks)
		usesMacros := strings.Contains(string(data), "`define") || strings.Contains(string(data), "`include")
		ast.Inspect(f, func(n ast.Node) bool {
			var body ast.Stmt
			switch n := n.(type) {
			case *ast.Initial:
				body = n.Body
			case *ast.Final:
				body = n.Body
			case *ast.Always:
				body = n.Body
			case *ast.ContinuousAssign:
				items++
				return true
			default:
				return true
			}
			items++
			if usesMacros {
				checkSpansForward(t, rel, body)
			} else {
				checkSpans(t, rel, body)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if items < 500 {
		t.Fatalf("only %d items found: the walk is broken", items)
	}
	t.Logf("%d procedural items", items)
}
