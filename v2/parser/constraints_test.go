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

func parseConstraints(src string) ([]ast.ConstraintItem, []Error) {
	toks, _ := preprocessor.Preprocess("c.sv", src, nil)
	p := &parser{toks: toks}
	items := p.parseConstraintItems(toks)
	return items, p.suppressed
}

func dumpItems(items []ast.ConstraintItem) string {
	var parts []string
	for _, it := range items {
		parts = append(parts, strings.TrimSpace(ast.Dump(it)))
	}
	return strings.Join(parts, "\n")
}

var constraintGolden = []struct{ src, want string }{
	{"x > 0;", "(constraint-expr (> x 0))"},
	{"soft x == 1;", "(constraint-expr soft (== x 1))"},
	{"x inside {[1:5], 7};", "(constraint-expr (inside x [1:5] 7))"},
	{"x dist {1 := 3, [2:4] :/ 2, 9};", "(constraint-expr (dist x (:= 1 3) (:/ [2:4] 2) 9))"},
	{"soft x dist {default :/ 1};", "(constraint-expr soft (dist x (:/ default 1)))"},
	{"a -> b == 1;", "(implies a\n  (constraint-expr (== b 1)))"},
	{"a && b -> { c > 0; d < 1; }", "(implies (&& a b)\n  (constraint-set\n    (constraint-expr (> c 0))\n    (constraint-expr (< d 1))))"},
	{"a -> b -> c;", "(implies a\n  (implies b\n    (constraint-expr c)))"},
	{"a <-> b;", "(constraint-expr (<-> a b))"},
	{"if (m) x > 0; else if (n) { x < 0; } else x == 0;", "(constraint-if m\n  (constraint-expr (> x 0))\n  (constraint-if n\n    (constraint-set\n      (constraint-expr (< x 0)))\n    (constraint-expr (== x 0))))"},
	{"foreach (arr[i, j]) arr[i][j] > 0;", "(constraint-foreach arr [i,j]\n  (constraint-expr (> (index (index arr i) j) 0)))"},
	{"foreach (arr[,j]) { arr[j] != 0; }", "(constraint-foreach arr [,j]\n  (constraint-set\n    (constraint-expr (!= (index arr j) 0))))"},
	{"solve a, b before c;", "(solve [a b] before [c])"},
	{"disable soft x;", "(disable-soft x)"},
	{"unique { a, b[1], [2:3] };", "(unique [a (index b 1) [2:3]])"},
	{"{a, b} == 3;", "(constraint-expr (== (concat a b) 3))"},
	{"x == (y ? 1 : 2);", "(constraint-expr (== x (paren (?: y 1 2))))"},
	{"", ""},
}

func TestConstraintItemsGolden(t *testing.T) {
	for _, c := range constraintGolden {
		items, errs := parseConstraints(c.src)
		if len(errs) != 0 {
			t.Errorf("%q: errors %+v", c.src, errs)
			continue
		}
		if got := dumpItems(items); got != c.want {
			t.Errorf("%q:\n got: %s\nwant: %s", c.src, got, c.want)
		}
	}
}

func TestConstraintDeclarationAndInlineBlock(t *testing.T) {
	f, errs := parseSrc(t, `class C;
  rand int x, y;
  constraint c { x < y; if (x > 3) y == 0; }
  function void f();
    void'(randomize(x) with { x dist {1 := 1, 2 := 2}; });
  endfunction
endclass`)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	var con *ast.Constraint
	var blk *ast.ConstraintBlock
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Constraint:
			con = n
		case *ast.ConstraintBlock:
			blk = n
		}
		return true
	})
	if con == nil || len(con.Items) != 2 {
		t.Fatalf("constraint: %v", con)
	}
	if blk == nil || len(blk.Items) != 1 || len(blk.Tokens) == 0 {
		t.Fatalf("inline block: %+v", blk)
	}
}

func TestConstraintErrorsAreReportedAndRecover(t *testing.T) {
	f, errs := parseSrc(t, "class C;\nconstraint c { x > ; y < 3; solve a; }\nint after;\nendclass")
	if len(errs) == 0 {
		t.Fatal("expected the malformed items to be reported")
	}
	var con *ast.Constraint
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.Constraint); ok {
			con = c
		}
		return true
	})
	if con == nil || len(con.Items) != 3 {
		t.Fatalf("items: %s", ast.Dump(f))
	}
	if !strings.Contains(ast.Dump(f), "after") {
		t.Fatalf("declaration after the constraint lost: %s", ast.Dump(f))
	}
}

func TestConstraintNestingIsBounded(t *testing.T) {
	src := strings.Repeat("a -> { ", 3000) + "b;" + strings.Repeat(" }", 3000)
	items, _ := parseConstraints(src)
	if len(items) == 0 {
		t.Fatal("no items")
	}
}

func FuzzParseConstraintItems(f *testing.F) {
	for _, c := range constraintGolden {
		f.Add(c.src)
	}
	f.Add("a -> { if (b) c; else foreach (d[e]) f dist {1}; }")
	f.Fuzz(func(t *testing.T, src string) {
		parseConstraints(src)
	})
}

// TestConstraintsOnCorpus parses every constraint block in the vendored files:
// the items must be well-formed trees without BadExpr or ConstraintRaw, and
// there must be many.
func TestConstraintsOnCorpus(t *testing.T) {
	counts := map[string]int{}
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
		check := func(items []ast.ConstraintItem) {
			for _, it := range items {
				ast.Inspect(it, func(m ast.Node) bool {
					switch m.(type) {
					case *ast.BadExpr, *ast.ConstraintRaw:
						t.Errorf("%s: %T at line %d", rel, m, m.Pos().Line+1)
					}
					return true
				})
				if usesMacros {
					checkSpansForward(t, rel, it)
				} else {
					checkSpans(t, rel, it)
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Constraint:
				counts["constraint"]++
				check(n.Items)
			case *ast.ConstraintBlock:
				counts["inline"]++
				check(n.Items)
			case *ast.Dist:
				counts["dist"]++
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts["constraint"] < 100 || counts["inline"] < 10 || counts["dist"] < 3 {
		t.Fatalf("too few found: %v", counts)
	}
}
