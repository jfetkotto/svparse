package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/lexer"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// lexExpr lexes src into the preprocessed-token form ParseExpr takes (no
// trailing EOF), and reports whether the lexer itself complained.
func lexExpr(src string) (toks []preprocessor.Token, lexOK bool) {
	raw, errs := lexer.Lex(src)
	for _, t := range raw {
		if t.Kind != token.KindEOF {
			toks = append(toks, preprocessor.Token{Token: t, File: "t.sv"})
		}
	}
	return toks, len(errs) == 0
}

func parseExprSrc(src string) (ast.Expr, []Error) {
	toks, _ := lexExpr(src)
	return ParseExpr(toks)
}

// exprGolden pairs source with the expected ast.Dump (one line, trailing
// newline trimmed). Each row was checked against the LRM by hand: operator
// precedence and associativity (table 11-2), cast and select forms, and
// the type-or-expression wrapping.
var exprGolden = []struct{ src, want string }{
	// Precedence and associativity (LRM 11.3, table 11-2).
	{"a + b * c", "(+ a (* b c))"},
	{"a * b + c", "(+ (* a b) c)"},
	{"a - b - c", "(- (- a b) c)"},
	{"a ** b ** c", "(** (** a b) c)"}, // ** is left-associative
	{"-a ** b", "(** (unary - a) b)"},  // unary binds tighter than **
	{"!a && b || c", "(|| (&& (unary ! a) b) c)"},
	{"a < b == c > d", "(== (< a b) (> c d))"},
	{"a & b ^ c | d", "(| (^ (& a b) c) d)"},
	{"a << 2 + 1", "(<< a (+ 2 1))"},
	{"a ==? b", "(==? a b)"},
	{"a !== b", "(!== a b)"},
	{"a >>> 2", "(>>> a 2)"},
	{"a ? b : c ? d : e", "(?: a b (?: c d e))"}, // right-associative
	{"a ? b ? c : d : e", "(?: a (?: b c d) e)"},
	{"a -> b -> c", "(-> a (-> b c))"}, // right-associative
	{"a <-> b", "(<-> a b)"},
	{"a || b -> c", "(-> (|| a b) c)"},
	{"a ? b : c -> d", "(-> (?: a b c) d)"}, // ?: binds tighter than ->
	{"x ? {a} : {b}", "(?: x (concat a) (concat b))"},

	// Unary, increment/decrement, assignment expressions, min:typ:max.
	{"&a | ~^b", "(| (unary & a) (unary ~^ b))"},
	{"- - a", "(unary - (unary - a))"},
	{"a++ + --b", "(+ (post++ a) (pre-- b))"},
	{"(a = b)", "(paren (assign = a b))"},
	{"(a += b + 1)", "(paren (assign += a (+ b 1)))"},
	{"(1:2:3)", "(paren (mintypmax 1 2 3))"},

	// inside.
	{"x inside {1, [2:3], 4}", "(inside x 1 [2:3] 4)"},
	{"a inside {1} && b", "(&& (inside a 1) b)"},
	{"x inside {[$:5]}", "(inside x [$:5])"},

	// Casts (LRM 6.24.1).
	{"8'(x)", "(cast 8 x)"},
	{"int'(x)", "(cast <int> x)"},
	{"signed'(x)", "(cast <signed> x)"},
	{"const'(x)", "(cast <const> x)"},
	{"T'(x)", "(cast (type-or-expr T) x)"},
	{"pkg::T'(x)", "(cast (type-or-expr (:: pkg T)) x)"},
	{"(W+1)'(x)", "(cast (paren (+ W 1)) x)"},
	{"string'(a)", "(cast <string> a)"},
	{"logic signed [7:0]'(x)", "(cast <logic signed [7:0]> x)"},
	{"unsigned'(x)", "(cast <unsigned> x)"},

	// Names, scopes, hierarchy.
	{"$unit::x", "(:: $unit x)"},
	{"pkg::C#(8)::x", "(:: (param-name pkg::C 8) x)"},
	{"C#(.W(8), int)::new(1)", "(new (param-name C .W 8 <int>):: 1)"},
	{"$root.a.b", "(. (. $root a) b)"},
	{"a.b[3].c", "(. (index (. a b) 3) c)"},
	{"local::x", "(:: local x)"},

	// Selects.
	{"a[7:0]", "(range: a 7 0)"},
	{"a[i+:4]", "(range+: a i 4)"},
	{"a[i-:4][1]", "(index (range-: a i 4) 1)"},
	{"q[$]", "(index q $)"},
	{"a[b ? c : d]", "(index a (?: b c d))"},

	// Calls (LRM 13, 20).
	{"f(a, .n(v), , c)", "(call f a .n(v) (empty) c)"},
	{"f()", "(call f)"},
	{"obj.m(1).n(2)", "(call (. (call (. obj m) 1) n) 2)"},
	{"$bits(T)", "(call $bits (type-or-expr T))"},
	{"$bits(logic [7:0])", "(call $bits <logic [7:0]>)"},
	{"$bits(a + b)", "(call $bits (+ a b))"},
	{"$clog2(N)", "(call $clog2 N)"}, // not a type-accepting function
	{"q.find(x) with (x > 3)", "(call (. q find) x with (paren (> x 3)))"},
	{"super.new(x)", "(call (. super new) x)"},

	// Concatenation, replication, streaming (LRM 11.4.12, 11.4.14).
	{"{a, b}", "(concat a b)"},
	{"{2{a, b}}", "(repeat 2 a b)"},
	{"{}", "(concat)"},
	{"{<<8{a, b with [3:0]}}", "(stream << slice=8 a (with b [3:0]))"},
	{"{>>{a}}", "(stream >> a)"},
	{"{<<{a with [i +: 4]}}", "(stream << (with a [i+:4]))"},

	// Assignment patterns (LRM 10.9).
	{"'{1, 2}", "(pattern 1 2)"},
	{"'{}", "(pattern)"},
	{"'{default: 0}", "(pattern (item default 0))"},
	{"'{a: 1, b: 2}", "(pattern (item a 1) (item b 2))"},
	{"T'{x: 1}", "(pattern T' (item x 1))"},
	{"'{2{1}}", "(pattern (repeat 2 1))"},
	{"int'{default: 1}", "(pattern <int>' (item default 1))"},
	{"'{k: x ? 1 : 2}", "(pattern (item k (?: x 1 2)))"},

	// new, this, super, tagged, type, literals.
	{"new", "(new)"},
	{"new(1, .a(2))", "(new 1 .a(2))"},
	{"new[4]", "(new-array 4)"},
	{"new[4](src)", "(new-array 4 src)"},
	{"new src", "(new-copy src)"},
	{"this.x", "(. this x)"},
	{"tagged Valid 5", "(tagged Valid 5)"},
	{"tagged Invalid", "(tagged Invalid)"},
	{"tagged Valid 5 + 1", "(+ (tagged Valid 5) 1)"}, // the operand is a primary
	{"type(x)", "(typeof (type-or-expr x))"},
	{"type(logic [3:0])", "(typeof <logic [3:0]>)"},
	{"$bits(int [N-1:0])", "(call $bits <int [(- N 1):0]>)"},
	{"$bits(bit [$clog2(N)-1:0][3:0])", "(call $bits <bit [(- (call $clog2 N) 1):0] [3:0]>)"},
	{"$bits(string [])", "(call $bits <string []>)"},
	{"null", "null"},
	{"$", "$"},
	{"1.5 + 10ns", "(+ 1.5 10ns)"},
	{`"s" + 'x`, `(+ "s" 'x)`},
	{"8'hFF", "8'hFF"},

	// Attribute instances are skipped.
	{"a + (* keep *) b", "(+ a b)"},
}

func TestParseExprGolden(t *testing.T) {
	for _, c := range exprGolden {
		e, errs := parseExprSrc(c.src)
		if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %+v", c.src, errs)
			continue
		}
		if got := strings.TrimSuffix(ast.Dump(e), "\n"); got != c.want {
			t.Errorf("%q:\n got: %s\nwant: %s", c.src, got, c.want)
		}
	}
}

// TestParseExprRoundTrip prints each parsed expression back to source and
// reparses it: the tree must be identical, and printing must be idempotent.
func TestParseExprRoundTrip(t *testing.T) {
	for _, c := range exprGolden {
		e, _ := parseExprSrc(c.src)
		printed := ast.ExprString(e)
		e2, errs := parseExprSrc(printed)
		if len(errs) != 0 {
			t.Errorf("%q printed as %q, which fails to reparse: %+v", c.src, printed, errs)
			continue
		}
		if a, b := ast.Dump(e), ast.Dump(e2); a != b {
			t.Errorf("%q printed as %q, reparsed differently:\n first: %s second: %s", c.src, printed, a, b)
		}
		if again := ast.ExprString(e2); again != printed {
			t.Errorf("%q: printing is not idempotent: %q then %q", c.src, printed, again)
		}
	}
}

func TestParseExprErrors(t *testing.T) {
	cases := []struct {
		src      string
		wantErr  string // substring of the first error
		wantDump string
	}{
		{"", "expected an expression, found end of input", `(bad "")`},
		{"a +", "expected an expression, found end of input", `(+ a (bad ""))`},
		{"(a", "expected ')'", "(paren a)"},
		{"a ? b", "expected ':' in conditional expression", `(?: a b (bad ""))`},
		{"a ? : c", "expected an expression, found \":\"", `(?: a (bad "") c)`},
		{"a )", "unexpected \")\" after expression", "a"},
		{"{a, b", "expected '}' to close the concatenation", "(concat a b)"},
		{"f(a, b", "expected ')' to close the argument list", "(call f a b)"},
		{"a[1", "expected ']'", "(index a 1)"},
		{"a.", "expected a member name after '.'", "(. a)"},
		{"pkg::", "expected a name after '::'", "(:: pkg)"},
		{"x inside 1", "expected '{' after inside", "(inside x)"},
		{"tagged", "expected a union member name", "(tagged)"},
		{"type x", "expected '(' after type", `(typeof (bad ""))`},
		{"a +* b", "expected an expression, found \"*\"", `(+ a (bad "*"))`},
		{"(1:2", "expected ':' in a min:typ:max triple", `(paren (mintypmax 1 2 (bad "")))`},
		{"'{a: }", "expected an expression, found \"}\"", `(pattern (item a (bad "")))`},
		{"signed T", "signed/unsigned cannot qualify the user-defined type \"T\"", "<T signed>"},
		{"$bits(logic [7:0)", "expected ']'", "(call $bits <logic [7:0]>)"},
		{"$bits(logic ['()", "expected an expression", `(call $bits <logic [(bad "'")]>)`},
	}
	for _, c := range cases {
		e, errs := parseExprSrc(c.src)
		if len(errs) == 0 {
			t.Errorf("%q: expected an error containing %q, got none (tree %s)", c.src, c.wantErr, ast.Dump(e))
			continue
		}
		if !strings.Contains(errs[0].Message, c.wantErr) {
			t.Errorf("%q: first error %q, want substring %q", c.src, errs[0].Message, c.wantErr)
		}
		if got := strings.TrimSuffix(ast.Dump(e), "\n"); got != c.wantDump {
			t.Errorf("%q:\n got: %s\nwant: %s", c.src, got, c.wantDump)
		}
	}
}

func TestParseExprNeverReturnsNilOrLosesTokens(t *testing.T) {
	// Even nonsense yields a non-nil tree, and a BadExpr keeps the tokens it
	// swallowed so a consumer can still report them.
	e, errs := parseExprSrc("@ # ? ;")
	if e == nil || len(errs) == 0 {
		t.Fatalf("got %v with %d errors", e, len(errs))
	}
	var bad *ast.BadExpr
	ast.Inspect(e, func(n ast.Node) bool {
		if b, ok := n.(*ast.BadExpr); ok && bad == nil {
			bad = b
		}
		return true
	})
	if bad == nil || len(bad.Tokens) == 0 || bad.Tokens[0].Text != "@" {
		t.Errorf("expected a BadExpr holding the offending '@' token, got %+v in %s", bad, ast.Dump(e))
	}
}

func TestParseExprSpans(t *testing.T) {
	// Line 1 is "  fn(bb)": fn at columns 2-4, '(' at 4, bb at 5-7, ')' at 7-8.
	e, errs := parseExprSrc("a +\n  fn(bb)")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want := map[string]ast.Position{
		"binary": {File: "t.sv", Line: 0, Character: 0, EndLine: 1, EndCharacter: 8},
		"a":      {File: "t.sv", Line: 0, Character: 0, EndLine: 0, EndCharacter: 1},
		"call":   {File: "t.sv", Line: 1, Character: 2, EndLine: 1, EndCharacter: 8}, // through the ')'
		"fn":     {File: "t.sv", Line: 1, Character: 2, EndLine: 1, EndCharacter: 4},
		"bb":     {File: "t.sv", Line: 1, Character: 5, EndLine: 1, EndCharacter: 7},
	}
	got := make(map[string]ast.Position)
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Binary:
			got["binary"] = n.Pos()
		case *ast.Call:
			got["call"] = n.Pos()
		case *ast.Ident:
			got[n.Name] = n.Pos()
		}
		return true
	})
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: span %+v, want %+v", name, got[name], w)
		}
	}
}

// nameOnlyPosition reports whether n's Position covers only its name token,
// so its children may lie outside it.
func nameOnlyPosition(n ast.Node) bool {
	switch n.(type) {
	case *ast.ParamOverride, *ast.Type:
		return true
	}
	return false
}

// within reports whether inner lies inside outer (inclusive).
func within(inner, outer ast.Position) bool {
	before := func(l1, c1, l2, c2 int) bool { return l1 < l2 || (l1 == l2 && c1 <= c2) }
	return before(outer.Line, outer.Character, inner.Line, inner.Character) &&
		before(inner.EndLine, inner.EndCharacter, outer.EndLine, outer.EndCharacter)
}

// checkSpans asserts every node's span is well formed and nested inside its
// parent's. The exceptions are the children of a ParamOverride and of a Type:
// by convention those nodes are positioned on their name token alone (see
// ast.ParamOverride and ast.Type), so a value expression or a dimension bound
// lies outside them.
func checkSpans(t *testing.T, src string, e ast.Expr) {
	t.Helper()
	var stack []ast.Node
	ast.Inspect(e, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		p := n.Pos()
		if p.EndLine < p.Line || (p.EndLine == p.Line && p.EndCharacter < p.Character) {
			t.Errorf("%q: %T has a backwards span %+v", src, n, p)
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			if !nameOnlyPosition(parent) && !within(p, parent.Pos()) {
				t.Errorf("%q: %T %+v escapes its parent %T %+v", src, n, p, parent, parent.Pos())
			}
		}
		stack = append(stack, n)
		return true
	})
}

func TestParseExprMissingOperandSitsInsideItsParent(t *testing.T) {
	// "0?0 0": the conditional is missing its ':' and else-branch. The
	// placeholder for the missing branch is zero-width at the start of the
	// next token (column 4), and the conditional widens to contain it.
	e, _ := parseExprSrc("0?0 0")
	checkSpans(t, "0?0 0", e)
	cond, ok := e.(*ast.Conditional)
	if !ok {
		t.Fatalf("got %T", e)
	}
	bad, ok := cond.Else.(*ast.BadExpr)
	if !ok {
		t.Fatalf("else branch is %T, want a BadExpr placeholder", cond.Else)
	}
	if p := bad.Pos(); p.Character != 4 || p.EndCharacter != 4 {
		t.Errorf("placeholder at %+v, want zero-width at column 4", p)
	}
	if p := cond.Pos(); p.EndCharacter != 4 {
		t.Errorf("conditional ends at column %d, want it widened to 4 to contain the placeholder", p.EndCharacter)
	}
}

func TestParseExprMissingLeftOperandSitsInsideItsParent(t *testing.T) {
	// "'{ :0": the item has no key, so its placeholder is created before
	// the item has consumed anything and must not precede the item.
	e, _ := parseExprSrc("'{ :0")
	checkSpans(t, "'{ :0", e)
}

func TestParseExprSpansAreNestedAndForward(t *testing.T) {
	for _, c := range exprGolden {
		e, _ := parseExprSrc(c.src)
		checkSpans(t, c.src, e)
	}
}

func TestParseExprDeepNestingDoesNotOverflowTheStack(t *testing.T) {
	const n = 100000
	cases := map[string]string{
		"parens":      strings.Repeat("(", n) + "a" + strings.Repeat(")", n),
		"unary":       strings.Repeat("- ", n) + "a",
		"incdec":      strings.Repeat("++", n) + "a",
		"conditional": strings.Repeat("a ? b : ", n) + "c",
		"implication": strings.Repeat("a -> ", n) + "c",
		"tagged":      strings.Repeat("tagged A ", n),
		"new":         strings.Repeat("new ", n),
		"braces":      strings.Repeat("{", n),
		"patterns":    strings.Repeat("'{", n),
		"call args":   strings.Repeat("f(", n),
		"index":       strings.Repeat("a[", n),
		"type refs":   strings.Repeat("type(", n),
		"param lists": strings.Repeat("C#(", n),
		"streaming":   strings.Repeat("{<<{", n),
	}
	for name, src := range cases {
		e, errs := parseExprSrc(src)
		if e == nil {
			t.Errorf("%s: nil tree", name)
		}
		if len(errs) == 0 {
			t.Errorf("%s: expected an error for %d-deep nesting", name, n)
		}
	}
}

func TestParseExprLegitimateNestingIsAccepted(t *testing.T) {
	const depth = 300
	src := strings.Repeat("(", depth) + "a" + strings.Repeat(")", depth)
	if _, errs := parseExprSrc(src); len(errs) != 0 {
		t.Errorf("%d nested parentheses should parse: %+v", depth, errs)
	}
}

func TestParseExprLongFlatChainIsLinear(t *testing.T) {
	const n = 100000
	e, errs := parseExprSrc("a" + strings.Repeat(" + a", n))
	if len(errs) != 0 {
		t.Fatal(errs[0])
	}
	count := 0
	ast.Inspect(e, func(node ast.Node) bool {
		if node != nil {
			count++
		}
		return true
	})
	if want := 2*n + 1; count != want {
		t.Errorf("got %d nodes, want %d", count, want)
	}
}

func TestParseExprTrailingTokensAreReportedNotSwallowed(t *testing.T) {
	e, errs := parseExprSrc("a b")
	if got := strings.TrimSpace(ast.Dump(e)); got != "a" {
		t.Errorf("tree %s, want just a", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "unexpected \"b\" after expression") {
		t.Errorf("errors: %+v", errs)
	}
}

func FuzzParseExprNeverPanics(f *testing.F) {
	for _, c := range exprGolden {
		f.Add(c.src)
	}
	f.Add("")
	f.Add("(((")
	f.Add("a ? b")
	f.Add("{<<{")
	f.Add(`\esc+ape `)
	f.Fuzz(func(t *testing.T, src string) {
		toks, lexOK := lexExpr(src)
		e, errs := ParseExpr(toks) // must not panic or hang
		if e == nil {
			t.Fatal("nil tree")
		}
		checkSpans(t, src, e)

		// A clean parse must survive printing and reparsing unchanged.
		if lexOK && len(errs) == 0 {
			printed := ast.ExprString(e)
			toks2, lexOK2 := lexExpr(printed)
			e2, errs2 := ParseExpr(toks2)
			if !lexOK2 || len(errs2) != 0 {
				t.Fatalf("%q printed as %q, which no longer parses cleanly: %+v", src, printed, errs2)
			}
			if a, b := ast.Dump(e), ast.Dump(e2); a != b {
				t.Fatalf("%q printed as %q, reparsed differently:\n%s\n%s", src, printed, a, b)
			}
		}
	})
}
