package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

func parseProp(t *testing.T, src string) (ast.Expr, []Error) {
	t.Helper()
	toks, _ := preprocessor.Preprocess("p.sv", src, nil)
	p := &parser{toks: toks}
	e := p.parsePropExpr()
	if tok := p.peek(); tok.Kind != token.KindEOF && len(p.errs) == 0 {
		p.errorf(tok, "trailing %q", tok.Text)
	}
	return e, p.errs
}

var propGolden = []struct{ src, want string }{
	{"a", "a"},
	{"a ##1 b", "(## a 1 b)"},
	{"##1 a", "(## _ 1 a)"},
	{"a ##[1:3] b", "(## a [1:3] b)"},
	{"a ##[1:$] b", "(## a [1:$] b)"},
	{"a ##[*] b", "(## a [*] b)"},
	{"a ##[+] b", "(## a [+] b)"},
	{"a ##(N+1) b", "(## a (paren (+ N 1)) b)"},
	{"a ##1 b ##2 c", "(## (## a 1 b) 2 c)"},
	{"a[*3]", "([*] a 3)"},
	{"a[*2:4]", "([*] a [2:4])"},
	{"a[*]", "([*] a [*])"},
	{"a[+]", "([*] a [+])"},
	{"a[->2]", "([->] a 2)"},
	{"a[=1:$]", "([=] a [1:$])"},
	{"(a ##1 b)[*2]", "([*] (paren (## a 1 b)) 2)"},
	{"a |-> b", "(|-> a b)"},
	{"a |=> b ##1 c", "(|=> a (## b 1 c))"},
	{"a |-> b |-> c", "(|-> a (|-> b c))"},
	{"a #-# b", "(#-# a b)"},
	{"a #=# b", "(#=# a b)"},
	{"a and b or c", "(or (and a b) c)"},
	{"a or b and c", "(or a (and b c))"},
	{"a intersect b within c", "(intersect a (within b c))"},
	{"a throughout b ##1 c", "(throughout a (## b 1 c))"},
	{"a iff b until c", "(until (iff a b) c)"},
	{"a s_until_with b", "(s_until_with a b)"},
	{"a implies b", "(implies a b)"},
	{"not a ##1 b", "(not (## a 1 b))"},
	{"not a and b", "(and (not a) b)"},
	{"nexttime a", "(nexttime a)"},
	{"nexttime [2] a", "(nexttime 2 a)"},
	{"s_nexttime a", "(s_nexttime a)"},
	{"always a", "(always a)"},
	{"always [1:3] a", "(always [1:3] a)"},
	{"s_always [1:3] a", "(s_always [1:3] a)"},
	{"eventually [1:3] a", "(eventually [1:3] a)"},
	{"s_eventually a", "(s_eventually a)"},
	{"always a |-> b", "(always (|-> a b))"},
	{"strong(a ##1 b)", "(strong (## a 1 b))"},
	{"weak(a)", "(weak a)"},
	{"first_match(a ##[1:2] b)", "(first_match (## a [1:2] b))"},
	{"first_match(a, x = 1)", "(first_match a (assign = x 1))"},
	{"accept_on (x) a", "(accept_on x a)"},
	{"sync_reject_on (x) a |-> b", "(sync_reject_on x (|-> a b))"},
	{"if (c) a else b", "(prop-if c a b)"},
	{"if (c) a", "(prop-if c a _)"},
	{"case (x) 1, 2: a; default: b; endcase", "(prop-case x (item [1 2] a) (item default b))"},
	{"@(posedge clk) a |-> b", "(clocked @(posedge clk) (|-> a b))"},
	{"disable iff (rst) a |-> b", "(disable-iff rst (|-> a b))"},
	{"@(posedge clk) disable iff (rst) a", "(clocked @(posedge clk) (disable-iff rst a))"},
	{"a ##1 @(posedge c2) b", "(## a 1 (clocked @(posedge c2) b))"},
	{"(a ##1 b, x = 1, f())", "(paren (match (## a 1 b) (assign = x 1) (call f)))"},
	{"(a + b) > c |-> d", "(|-> (> (paren (+ a b)) c) d)"},
	{"$rose(a) |=> $fell(b)", "(|=> (call $rose a) (call $fell b))"},
	{"s.triggered ##1 b", "(## (. s triggered) 1 b)"},
	{"a[0] ##1 b[3:0]", "(## (index a 0) 1 (range: b 3 0))"},
}

func TestPropExprGolden(t *testing.T) {
	for _, c := range propGolden {
		e, errs := parseProp(t, c.src)
		if len(errs) != 0 {
			t.Errorf("%q: errors %+v", c.src, errs)
			continue
		}
		if got := ast.Dump(e); strings.TrimSpace(got) != c.want {
			t.Errorf("%q:\n got: %s\nwant: %s", c.src, strings.TrimSpace(got), c.want)
		}
	}
}

// Printing a parsed property and parsing it again gives the same tree.
func TestPropExprRoundTrip(t *testing.T) {
	for _, c := range propGolden {
		e, errs := parseProp(t, c.src)
		if len(errs) != 0 {
			continue
		}
		printed := ast.ExprString(e)
		e2, errs2 := parseProp(t, printed)
		if len(errs2) != 0 {
			t.Errorf("%q printed as %q does not reparse: %+v", c.src, printed, errs2)
			continue
		}
		if ast.Dump(e) != ast.Dump(e2) {
			t.Errorf("%q printed as %q reparses differently:\n%s\n%s", c.src, printed, ast.Dump(e), ast.Dump(e2))
		}
	}
}

const svaSource = `module m;
  property p1(sequence s, local input int n = 1, untyped u);
    int x;
    @(posedge clk) disable iff (rst) (s ##1 a[*2]) |-> ##[1:3] b;
  endproperty
  sequence s1;
    a ##1 b[->2] ##0 (c, x = 1);
  endsequence
  sequence s2(a, b);
    first_match(a ##[1:$] b) throughout c;
  endsequence : s2
  a1: assert property (@(posedge clk) a |-> b) else $error("x");
  assume property (p1(s1, 2, 3));
  cover property (a ##1 b) $display("hit");
  cover sequence (@(posedge clk) s1);
  restrict property (@(posedge clk) a);
  assert property (p) else begin $error("a"); end
  initial begin
    expect (@(posedge clk) a ##1 b) $display("ok"); else $error("fail");
    assert property (if (c) a else b);
    assert property (case (x) 1: a; default: b; endcase);
    assert property (not nexttime [2] always [1:3] a |-> accept_on (r) strong(b) until weak(c));
  end
endmodule`

func TestSVADeclarationsAndAssertions(t *testing.T) {
	f, errs := parseSrc(t, svaSource)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	body := f.Decls[0].(*ast.Container).Body
	if len(body) != 10 {
		t.Fatalf("items = %d:\n%s", len(body), ast.Dump(f))
	}
	p1 := body[0].(*ast.PropertyDecl)
	if p1.Name != "p1" || len(p1.Ports) != 3 || len(p1.Decls) != 1 || p1.Body == nil {
		t.Fatalf("p1: %s", ast.Dump(p1))
	}
	if p1.Ports[0].Type.Name != "sequence" || p1.Ports[2].Type.Name != "untyped" || p1.Ports[1].Name != "n" {
		t.Errorf("ports: %+v", p1.Ports)
	}
	s2 := body[2].(*ast.SequenceDecl)
	if s2.Name != "s2" || len(s2.Ports) != 2 {
		t.Fatalf("s2: %s", ast.Dump(s2))
	}
	if ca := body[7].(*ast.AssertionItem).Body.(*ast.ConcurrentAssert); ca.Kind != "restrict" {
		t.Errorf("restrict: %s", ast.Dump(body[7]))
	}
	if ca := body[6].(*ast.AssertionItem).Body.(*ast.ConcurrentAssert); !ca.Sequence || ca.Kind != "cover" {
		t.Errorf("cover sequence: %s", ast.Dump(body[6]))
	}
}

func TestSVAErrorsAreSuppressedAndExtentsHold(t *testing.T) {
	f, errs := parseSrc(t, "module m;\nproperty p; a |-> ; endproperty\nassert property (a |-> );\nlogic after;\nendmodule")
	if len(errs) != 0 {
		t.Fatalf("reported: %+v", errs)
	}
	body := f.Decls[0].(*ast.Container).Body
	if len(body) != 3 {
		t.Fatalf("items:\n%s", ast.Dump(f))
	}
	if v, ok := body[2].(*ast.Variable); !ok || v.Name != "after" {
		t.Fatalf("after lost: %s", ast.Dump(body[2]))
	}
}

func TestParsePropExprNeverPanics(t *testing.T) {
	for _, src := range []string{"", "##", "##[", "a[*", "a[->", "(", "a ##", "first_match(", "case (", "if", "@", "disable iff", "always [", "a #-", "((((((("} {
		parseProp(t, src)
	}
	deep := strings.Repeat("not ", 5000) + "a"
	if _, errs := parseProp(t, deep); len(errs) == 0 {
		t.Error("expected a depth error")
	}
}

func FuzzParsePropExpr(f *testing.F) {
	for _, c := range propGolden {
		f.Add(c.src)
	}
	f.Add("a ##[1:$] (b[*2], x = 1) |-> not c")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		p := &parser{toks: toks}
		p.parsePropExpr()
	})
}

// TestSVAOnCorpus parses every property, sequence and assertion in the
// vendored files: the bodies must be well-formed trees, none may contain a
// BadExpr (the corpus holds no malformed assertion), and there must be many.
func TestSVAOnCorpus(t *testing.T) {
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
		check := func(n ast.Node) {
			if n == nil {
				return
			}
			ast.Inspect(n, func(m ast.Node) bool {
				if _, bad := m.(*ast.BadExpr); bad {
					t.Errorf("%s: BadExpr at line %d", rel, m.Pos().Line+1)
				}
				return true
			})
			if usesMacros {
				checkSpansForward(t, rel, n)
			} else {
				checkSpans(t, rel, n)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.PropertyDecl:
				counts["property"]++
				if n.Body != nil {
					check(n.Body)
				}
			case *ast.SequenceDecl:
				counts["sequence"]++
				if n.Body != nil {
					check(n.Body)
				}
			case *ast.ConcurrentAssert:
				counts["assertion"]++
				check(n)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts["property"] < 5 || counts["sequence"] < 15 || counts["assertion"] < 30 {
		t.Fatalf("too few constructs found: %v", counts)
	}
}
