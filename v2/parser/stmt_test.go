package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
)

func parseStmtSrc(src string) (ast.Stmt, []Error) {
	toks, _ := lexExpr(src)
	return ParseStmt(toks)
}

// stmtGolden pairs source with the expected ast.Dump. Each row was checked
// against the LRM by hand: assignment versus comparison, the dangling else,
// case and loop forms, timing and event controls, block-local declarations.
var stmtGolden = []struct{ src, want string }{
	{"a = b + 1;", "(assign = a (+ b 1))\n"},
	{"a <= b;", "(assign <= a b)\n"},
	{"a += 2;", "(assign += a 2)\n"},
	{"q <= #1 d;", "(assign <= q d ctl=#1)\n"},
	{"q = @(posedge clk) d;", "(assign = q d ctl=@(posedge clk))\n"},
	{"q <= repeat (3) @(clk) d;", "(assign <= q d ctl=repeat(3) @(clk))\n"},
	{"i++;", "(expr (post++ i))\n"},
	{"--i;", "(expr (pre-- i))\n"},
	{"f(x, y);", "(expr (call f x y))\n"},
	{"obj.m(1);", "(expr (call (. obj m) 1))\n"},
	{"void'(f());", "(expr (cast <void> (call f)))\n"},
	{"$display(\"hi\");", "(expr (call $display \"hi\"))\n"},
	{"{a, b} = c;", "(assign = (concat a b) c)\n"},
	{"arr[i] <= v;", "(assign <= (index arr i) v)\n"},
	{"a.b.c = d;", "(assign = (. (. a b) c) d)\n"},
	{"cb.sig <= ##1 val;", "(assign <= (. cb sig) val ctl=##1)\n"},
	{"if (a) b = 1; else c = 2;", "(if cond=a\n  (assign = b 1)\n  (assign = c 2))\n"},
	{"if (a) if (b) x = 1; else x = 2;", "(if cond=a\n  (if cond=b\n    (assign = x 1)\n    (assign = x 2)))\n"},
	{"if (a) begin if (b) x = 1; end else x = 2;", "(if cond=a\n  (block begin\n    (if cond=b\n      (assign = x 1)))\n  (assign = x 2))\n"},
	{"unique if (a) ; else if (b) ; else ;", "(if unique cond=a\n  (null)\n  (if cond=b\n    (null)\n    (null)))\n"},
	{"priority if (a) x = 1;", "(if priority cond=a\n  (assign = x 1))\n"},
	{"case (s) 1, 2: x = 1; default: x = 0; endcase", "(case subject=s\n  (item 1 2\n    (assign = x 1))\n  (item default\n    (assign = x 0)))\n"},
	{"priority casez (s) 2'b1?: x = 1; endcase", "(casez priority subject=s\n  (item 2'b1?\n    (assign = x 1)))\n"},
	{"case (v) inside [1:3], 5: x = 1; endcase", "(case inside subject=v\n  (item [1:3] 5\n    (assign = x 1)))\n"},
	{"case (s) default x = 1; endcase", "(case subject=s\n  (item default\n    (assign = x 1)))\n"},
	{"unique0 casex (s) 4'b1x0z: ; endcase", "(casex unique0 subject=s\n  (item 4'b1x0z\n    (null)))\n"},
	{"for (int i = 0; i < 4; i++) x = i;", "(for cond=(< i 4) step=[(expr (post++ i))]\n  (variable i <int> = 0)\n  (assign = x i))\n"},
	{"for (int i = 0, j = 9; i < j; i++, j--) ;", "(for cond=(< i j) step=[(expr (post++ i)) (expr (post-- j))]\n  (variable i <int> = 0)\n  (variable j <int> = 9)\n  (null))\n"},
	{"for (i = 0, j = 9; i < j; i++) ;", "(for init=[(assign = i 0) (assign = j 9)] cond=(< i j) step=[(expr (post++ i))]\n  (null))\n"},
	{"for (;;) break;", "(for\n  (break))\n"},
	{"for (var int i = 0; i < 2; i += 1) ;", "(for cond=(< i 2) step=[(assign += i 1)]\n  (variable i <int> = 0)\n  (null))\n"},
	{"for (my_t i = 0; ; ) ;", "(for\n  (variable i <my_t> = 0)\n  (null))\n"},
	{"foreach (arr[i, j]) s += arr[i][j];", "(foreach arr vars=[i j]\n  (assign += s (index (index arr i) j)))\n"},
	{"foreach (a[, j]) ;", "(foreach a vars=[_ j]\n  (null))\n"},
	{"foreach (p.q[k]) ;", "(foreach (. p q) vars=[k]\n  (null))\n"},
	{"while (c) x = 1;", "(while cond=c\n  (assign = x 1))\n"},
	{"do x++; while (x < 3);", "(do-while cond=(< x 3)\n  (expr (post++ x)))\n"},
	{"do begin x++; end while (x < 3);", "(do-while cond=(< x 3)\n  (block begin\n    (expr (post++ x))))\n"},
	{"repeat (3) x = 1;", "(repeat count=3\n  (assign = x 1))\n"},
	{"forever #5 clk = ~clk;", "(forever\n  (timed #5\n    (assign = clk (unary ~ clk))))\n"},
	{"#10 x = 1;", "(timed #10\n  (assign = x 1))\n"},
	{"#(a + 1) x = 1;", "(timed #(paren (+ a 1))\n  (assign = x 1))\n"},
	{"#d x = 1;", "(timed #d\n  (assign = x 1))\n"},
	{"@(posedge clk or negedge rst) q <= 0;", "(timed @(posedge clk or negedge rst)\n  (assign <= q 0))\n"},
	{"@* y = a;", "(timed @*\n  (assign = y a))\n"},
	{"@(*) y = a;", "(timed @*\n  (assign = y a))\n"},
	{"@(* ) y = a;", "(timed @*\n  (assign = y a))\n"},
	{"@clk x = 1;", "(timed @(clk)\n  (assign = x 1))\n"},
	{"@pkg::ev x = 1;", "(timed @((:: pkg ev))\n  (assign = x 1))\n"},
	{"##2 x = 1;", "(timed ##2\n  (assign = x 1))\n"},
	{"@(a iff en, b) ;", "(timed @(a iff en or b)\n  (null))\n"},
	{"@(edge s) ;", "(timed @(edge s)\n  (null))\n"},
	{"wait (ready) x = 1;", "(wait cond=ready\n  (assign = x 1))\n"},
	{"wait fork;", "(wait-fork)\n"},
	{"wait_order (a, b) ok = 1; else bad = 1;", "(wait-order a b\n  (assign = ok 1)\n  (assign = bad 1))\n"},
	{"wait_order (a, b) else bad = 1;", "(wait-order a b else-only\n  (assign = bad 1))\n"},
	{"-> ev;", "(trigger -> ev)\n"},
	{"->> #5 ev;", "(trigger ->> #5 ev)\n"},
	{"->> @(clk) ev;", "(trigger ->> @(clk) ev)\n"},
	{"assign a = b;", "(proc-assign a b)\n"},
	{"deassign a;", "(proc-deassign a)\n"},
	{"force a = 1;", "(proc-force a 1)\n"},
	{"release a;", "(proc-release a)\n"},
	{"return;", "(return)\n"},
	{"return x + 1;", "(return (+ x 1))\n"},
	{"break;", "(break)\n"},
	{"continue;", "(continue)\n"},
	{"disable blk;", "(disable blk)\n"},
	{"disable fork;", "(disable-fork)\n"},
	{"assert (a) else $error(\"x\");", "(assert cond=a else-only\n  (expr (call $error \"x\")))\n"},
	{"assert #0 (a);", "(assert #0 cond=a\n  (null))\n"},
	{"assume final (a) ok = 1;", "(assume final cond=a\n  (assign = ok 1))\n"},
	{"cover (a) hit++;", "(cover cond=a\n  (expr (post++ hit)))\n"},
	{"assert (a) ok = 1; else bad = 1;", "(assert cond=a\n  (assign = ok 1)\n  (assign = bad 1))\n"},
	{"randcase 3: x = 1; 1: x = 2; endcase", "(randcase\n  (item 3\n    (assign = x 1))\n  (item 1\n    (assign = x 2)))\n"},
	{"begin : main int t; t = 1; end : main", "(block begin label=main end=main\n  (variable t <int>)\n  (assign = t 1))\n"},
	{"fork a = 1; b = 2; join_any", "(block fork join_any\n  (assign = a 1)\n  (assign = b 2))\n"},
	{"fork join_none", "(block fork join_none)\n"},
	{"fork : f a = 1; join : f", "(block fork join label=f end=f\n  (assign = a 1))\n"},
	{"name: begin end", "(labeled name\n  (block begin))\n"},
	{"begin int a, b = 2; my_t c; pkg::t d; cls #(4) e; typedef int word; parameter P = 3; x = P; end", "(block begin\n  (variable a <int>)\n  (variable b <int> = 2)\n  (variable c <my_t>)\n  (variable d <pkg::t>)\n  (variable e <cls #(4)>)\n  (typedef word\n    (type-alias <int>))\n  (parameter P = 3)\n  (assign = x P))\n"},
	{"begin : l1 begin : l2 x = 1; end end", "(block begin label=l1\n  (block begin label=l2\n    (assign = x 1)))\n"},
	{"if (x matches tagged Valid .v) y = v;", "(raw \"if ( x matches tagged Valid . v ) y = v ;\")\n"},
	{"case (x) matches tagged Valid .v: y = v; endcase", "(raw \"case ( x ) matches tagged Valid . v : y = v ; endcase\")\n"},
	{"assert property (@(posedge clk) a |-> b);", "(assert spec=(clocked @(posedge clk) (|-> a b))\n  (null))\n"},
	{"randsequence (main) main: a; endsequence", "(randsequence start=main\n  (production main\n    (rule\n      (item a))))\n"},
	{"randsequence (main) main: a := 2 | rand join b c; endsequence", "(randsequence start=main\n  (production main\n    (rule weight=2\n      (item a))\n    (rule\n      (rs-join\n        (item b)\n        (item c)))))\n"},
	{"randsequence (main) main: a b ; endsequence", "(randsequence start=main\n  (production main\n    (rule\n      (item a)\n      (item b))))\n"},
	{"randsequence (main) main: @ ; endsequence", "(raw \"randsequence ( main ) main : @ ; endsequence\")\n"},
	{";", "(null)\n"},
}

func TestParseStmtGolden(t *testing.T) {
	for _, c := range stmtGolden {
		s, errs := parseStmtSrc(c.src)
		if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %+v", c.src, errs)
			continue
		}
		if got := ast.Dump(s); got != c.want {
			t.Errorf("%q:\n got: %q\nwant: %q", c.src, got, c.want)
		}
	}
}

// TestParseStmtRoundTrip prints each parsed statement back to source and
// reparses it: the tree must be identical and printing idempotent.
func TestParseStmtRoundTrip(t *testing.T) {
	for _, c := range stmtGolden {
		s, _ := parseStmtSrc(c.src)
		printed := ast.StmtString(s)
		if strings.Contains(printed, "/*unhandled*/") {
			t.Errorf("%q printed with an unhandled node: %s", c.src, printed)
			continue
		}
		s2, errs := parseStmtSrc(printed)
		if len(errs) != 0 {
			t.Errorf("%q printed as %q, which fails to reparse: %+v", c.src, printed, errs)
			continue
		}
		if a, b := ast.Dump(s), ast.Dump(s2); a != b {
			t.Errorf("%q printed as %q, reparsed differently:\n first: %s second: %s", c.src, printed, a, b)
		}
		if again := ast.StmtString(s2); again != printed {
			t.Errorf("%q: printing is not idempotent: %q then %q", c.src, printed, again)
		}
	}
}

// TestParseStmtMalformed pins how recovery shapes the tree: the first error
// reported and the statement that survives.
func TestParseStmtMalformed(t *testing.T) {
	cases := []struct{ src, wantErr, wantDump string }{
		{"a = ;", "expected an expression", "(assign = a (bad \"\"))\n"},
		{"a = 1", "expected ';'", "(assign = a 1)\n"},
		{"begin a = 1;", "unterminated begin block", "(block begin\n  (assign = a 1))\n"},
		{"begin a = 1; endmodule", "expected end before \"endmodule\"", "(block begin\n  (assign = a 1))\n"},
		{"if (a b = 1;", "expected ')'", "(if cond=a\n  (assign = b 1))\n"},
		{"case (a) 1 x = 1; endcase", "expected ':' after the case item", "(case subject=a\n  (item 1\n    (assign = x 1)))\n"},
		{") x = 1;", "expected a statement, found \")\"", "(bad \") x = 1 ;\")\n"},
		{"x = 1 y = 2;", "expected ';'", "(assign = x 1)\n"},
		{"for (int i = 0; i < 3 i++) ;", "expected ';' after the for condition",
			"(for cond=(< i 3) step=[(expr (post++ i))]\n  (variable i <int> = 0)\n  (null))\n"},
		{"do x = 1; (a);", "expected 'while' after the body of do", "(do-while cond=(bad \"\")\n  (assign = x 1))\n"},
		{"begin a = ; b = 2; end", "expected an expression", "(block begin\n  (assign = a (bad \"\"))\n  (assign = b 2))\n"},
		{"begin a = 1 b = 2; c = 3; end", "expected ';'", "(block begin\n  (assign = a 1)\n  (assign = c 3))\n"},
		// A missing ';' before a statement keyword must not cost that statement.
		{"begin a = 1 if (x) y = 2; end", "expected ';'", "(block begin\n  (assign = a 1)\n  (if cond=x\n    (assign = y 2)))\n"},
		{"begin if (x) end", "expected a statement, found \"end\"", "(block begin\n  (if cond=x\n    (bad \"\")))\n"},
		{"fork a = 1; end", "expected join before \"end\"", "(block fork\n  (assign = a 1))\n"},
		{"case (s) 1: x = 1;", "unterminated case statement", "(case subject=s\n  (item 1\n    (assign = x 1)))\n"},
		{"foreach (a[i) ;", "expected ']'", "(foreach a vars=[i]\n  (null))\n"},
		{"begin : ; end", "expected an identifier", "(block begin\n  (null))\n"},
		{"else x = 1;", "expected a statement, found \"else\"", "(bad \"else x = 1 ;\")\n"},
	}
	for _, c := range cases {
		s, errs := parseStmtSrc(c.src)
		if len(errs) == 0 {
			t.Errorf("%q: expected an error containing %q, got none (tree %s)", c.src, c.wantErr, ast.Dump(s))
			continue
		}
		if !strings.Contains(errs[0].Message, c.wantErr) {
			t.Errorf("%q: first error %q, want substring %q", c.src, errs[0].Message, c.wantErr)
		}
		if got := ast.Dump(s); got != c.wantDump {
			t.Errorf("%q:\n got: %q\nwant: %q", c.src, got, c.wantDump)
		}
	}
}

func TestParseStmtNeverReturnsNil(t *testing.T) {
	for _, src := range []string{"", ")", "end", "endcase", "} } }", "@", "#", "if", "begin", "for", "((((", "else else"} {
		s, _ := parseStmtSrc(src) // must not panic or hang
		if s == nil {
			t.Errorf("%q: nil statement", src)
		}
	}
}

func TestParseStmtSpansCoverTheWholeStatementIncludingItsSemicolon(t *testing.T) {
	s, errs := parseStmtSrc("a = b + 1;")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if p := s.Pos(); p.Line != 0 || p.Character != 0 || p.EndLine != 0 || p.EndCharacter != 10 {
		t.Errorf("span = %+v, want 0:0 to 0:10 (through the ';')", p)
	}
	b, _ := parseStmtSrc("begin\n  x = 1;\nend")
	if p := b.Pos(); p.Line != 0 || p.EndLine != 2 || p.EndCharacter != 3 {
		t.Errorf("block span = %+v, want lines 0 to 2 ending at column 3", p)
	}
}

func TestParseStmtSpansAreNestedAndForward(t *testing.T) {
	for _, c := range stmtGolden {
		s, _ := parseStmtSrc(c.src)
		checkSpans(t, c.src, s)
	}
}

func TestParseStmtVisitsEveryNodeOnce(t *testing.T) {
	for _, c := range stmtGolden {
		s, _ := parseStmtSrc(c.src)
		counts := map[ast.Node]int{}
		ast.Inspect(s, func(n ast.Node) bool {
			if n != nil {
				counts[n]++
			}
			return true
		})
		for n, k := range counts {
			if k > 1 {
				t.Errorf("%q: %T visited %d times", c.src, n, k)
			}
		}
	}
}

func TestParseStmtDeepNestingDoesNotOverflowTheStack(t *testing.T) {
	const n = 100000
	cases := map[string]string{
		"begin blocks": strings.Repeat("begin ", n) + "x = 1;",
		"if chain":     strings.Repeat("if (a) ", n) + "x = 1;",
		"else if":      "if (a) x = 1; " + strings.Repeat("else if (a) x = 1; ", n),
		"labels":       strings.Repeat("l: ", n) + "x = 1;",
		"while":        strings.Repeat("while (a) ", n) + "x = 1;",
		"timing":       strings.Repeat("#1 ", n) + "x = 1;",
		"forks":        strings.Repeat("fork ", n),
		"case":         strings.Repeat("case (a) 1: ", n),
		"for":          strings.Repeat("for (;;) ", n),
		"parens":       "x = " + strings.Repeat("(", n) + "1;",
	}
	for name, src := range cases {
		s, errs := parseStmtSrc(src)
		if s == nil {
			t.Errorf("%s: nil", name)
		}
		if len(errs) == 0 && name != "else if" {
			t.Errorf("%s: expected an error for %d-deep nesting", name, n)
		}
	}
}

func TestParseStmtLegitimateNestingIsAccepted(t *testing.T) {
	const depth = 200
	src := strings.Repeat("begin ", depth) + "x = 1;" + strings.Repeat(" end", depth)
	if _, errs := parseStmtSrc(src); len(errs) != 0 {
		t.Errorf("%d nested blocks should parse: %+v", depth, errs[0])
	}
}

func TestParseStmtLongElseIfChainIsLinear(t *testing.T) {
	const n = 20000
	src := "if (a) x = 1; " + strings.Repeat("else if (a) x = 1; ", n)
	s, errs := parseStmtSrc(src)
	if len(errs) != 0 {
		t.Fatal(errs[0])
	}
	count := 0
	ast.Inspect(s, func(node ast.Node) bool {
		if _, ok := node.(*ast.If); ok {
			count++
		}
		return true
	})
	if count != n+1 {
		t.Errorf("found %d ifs, want %d", count, n+1)
	}
}

func TestParseStmtTrailingTokensAreReported(t *testing.T) {
	s, errs := parseStmtSrc("x = 1; y = 2;")
	if got := ast.Dump(s); got != "(assign = x 1)\n" {
		t.Errorf("tree %q", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "unexpected \"y\" after statement") {
		t.Errorf("errors: %+v", errs)
	}
}

func TestParseStmtFillsExpressionSiblings(t *testing.T) {
	// A standalone statement gets the same fill pass a file does: a block-local
	// declaration's initializer is parsed.
	s, errs := parseStmtSrc("begin int a = 1 + 2; end")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	b := s.(*ast.Block)
	v := b.Decls[0].(*ast.Variable)
	if v.InitialExpr == nil || strings.TrimSpace(ast.Dump(v.InitialExpr)) != "(+ 1 2)" {
		t.Errorf("InitialExpr = %v", v.InitialExpr)
	}
}

func FuzzParseStmtNeverPanics(f *testing.F) {
	for _, c := range stmtGolden {
		f.Add(c.src)
	}
	f.Add("")
	f.Add("begin begin")
	f.Add("if (a) else")
	f.Add("case (a) default")
	f.Add("for (int i = 0; i < 4;")
	f.Fuzz(func(t *testing.T, src string) {
		toks, lexOK := lexExpr(src)
		s, errs := ParseStmt(toks) // must not panic or hang
		if s == nil {
			t.Fatal("nil statement")
		}
		checkSpans(t, src, s)

		// A clean parse must survive printing and reparsing unchanged.
		if lexOK && len(errs) == 0 {
			printed := ast.StmtString(s)
			if strings.Contains(printed, "/*unhandled*/") {
				return
			}
			toks2, lexOK2 := lexExpr(printed)
			s2, errs2 := ParseStmt(toks2)
			if !lexOK2 || len(errs2) != 0 {
				t.Fatalf("%q printed as %q, which no longer parses cleanly: %+v", src, printed, errs2)
			}
			if a, b := ast.Dump(s), ast.Dump(s2); a != b {
				t.Fatalf("%q printed as %q, reparsed differently:\n%s\n%s", src, printed, a, b)
			}
		}
	})
}

func TestElseIfChainLinks(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"qualifiers on later links", "if (a) x = 1; else unique if (b) x = 2; else priority if (c) x = 3; else x = 4;",
			"(if cond=a\n  (assign = x 1)\n  (if unique cond=b\n    (assign = x 2)\n    (if priority cond=c\n      (assign = x 3)\n      (assign = x 4))))\n"},
		{"chain ending without else", "if (a) x = 1; else if (b) x = 2;",
			"(if cond=a\n  (assign = x 1)\n  (if cond=b\n    (assign = x 2)))\n"},
		{"a block as the else keeps nesting honest", "if (a) x = 1; else begin if (b) x = 2; end",
			"(if cond=a\n  (assign = x 1)\n  (block begin\n    (if cond=b\n      (assign = x 2))))\n"},
		{"a pattern link in the middle stays raw", "if (a) x = 1; else if (v matches .y) x = 2; else x = 3;",
			"(if cond=a\n  (assign = x 1)\n  (raw \"if ( v matches . y ) x = 2 ; else x = 3 ;\"))\n"},
	}
	for _, c := range cases {
		s, errs := parseStmtSrc(c.src)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors %+v", c.name, errs)
			continue
		}
		if got := ast.Dump(s); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

func TestElseIfChainSpansAllEndWhereTheChainEnds(t *testing.T) {
	s, _ := parseStmtSrc("if (a) x = 1; else if (b) x = 2; else x = 3;")
	end := s.Pos()
	n := 0
	for cur := s; ; n++ {
		i, ok := cur.(*ast.If)
		if !ok {
			break
		}
		if p := i.Pos(); p.EndLine != end.EndLine || p.EndCharacter != end.EndCharacter {
			t.Errorf("link %d ends at %d:%d, want the chain's end %d:%d", n, p.EndLine, p.EndCharacter, end.EndLine, end.EndCharacter)
		}
		if i.Else == nil {
			break
		}
		cur = i.Else
	}
}

func TestParseStmtNestingOverflowAtTheEndOfInput(t *testing.T) {
	// When the depth limit is hit with no tokens left, keeping "the rest of the
	// statement" raw must not index past the end of the input. (Found by the
	// fuzzer: a long run of "#0" delays.)
	for _, src := range []string{
		strings.Repeat("#0", 600),
		strings.Repeat("l:", 600),
		strings.Repeat("begin ", 600),
	} {
		s, errs := parseStmtSrc(src)
		if s == nil || len(errs) == 0 {
			t.Errorf("%.20q...: statement %v, %d errors", src, s, len(errs))
		}
	}
}

func TestForHeaderWithoutItsClosingParenStaysInsideTheStatement(t *testing.T) {
	// The ')' is missing, so the header cannot be told from the body by
	// brackets. A for header has two ';', and a third one ends the statement.
	cases := []struct{ src, wantNext string }{
		{"begin for (i = 0; i < 3; i++ x = 1; y = 2; end", "(assign = y 2)"},
		{"begin for (int i = 0; i < 3; i++ x = 1; y = 2; end", "(assign = y 2)"},
	}
	for _, c := range cases {
		s, errs := parseStmtSrc(c.src)
		if len(errs) == 0 {
			t.Errorf("%q: expected an error", c.src)
		}
		if got := ast.Dump(s); !strings.Contains(got, c.wantNext) {
			t.Errorf("%q: the statement after the broken header was lost:\n%s", c.src, got)
		}
	}
}
