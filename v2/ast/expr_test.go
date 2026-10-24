package ast_test

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

func tk(texts ...string) []preprocessor.Token {
	toks := make([]preprocessor.Token, len(texts))
	for i, s := range texts {
		toks[i] = preprocessor.Token{Token: token.Token{Text: s}}
	}
	return toks
}

func id(name string) *ast.Ident { return &ast.Ident{Name: name} }

func lit(text string) *ast.Literal { return &ast.Literal{Kind: ast.LitInt, Text: text} }

func unbounded() *ast.Literal { return &ast.Literal{Kind: ast.LitUnbounded, Text: "$"} }

// exprCases has at least one expression of every expression node type, with
// its expected Dump. It drives the dump golden test, the Walk completeness
// check, and (via TestEveryExprTypeIsExercised) is itself checked for
// completeness against expr.go.
var exprCases = []struct {
	name string
	expr ast.Expr
	want string
}{
	{"ident", id("a"), "a\n"},
	{"literal", lit("8'hFF"), "8'hFF\n"},
	{"scoped", &ast.ScopedName{Scope: id("pkg"), Name: "x"}, "(:: pkg x)\n"},
	{"scoped nests left", &ast.ScopedName{Scope: &ast.ScopedName{Scope: id("pkg"), Name: "C"}, Name: "x"}, "(:: (:: pkg C) x)\n"},
	{"parameterized name", &ast.ParameterizedName{Name: "C", Overrides: []ast.ParamOverride{
		{ValueExpr: lit("8")}, {Name: "W", ValueExpr: lit("2")}, {Value: tk("4")}}},
		"(param-name C 8 .W 2 \"4\")\n"},
	{"package-qualified parameterized name", &ast.ParameterizedName{Scope: id("pkg"), Name: "C", Overrides: []ast.ParamOverride{{ValueExpr: lit("8")}}},
		"(param-name pkg::C 8)\n"},
	{"member select nests left", &ast.MemberSelect{X: &ast.MemberSelect{X: id("a"), Member: "b"}, Member: "c"}, "(. (. a b) c)\n"},
	{"index", &ast.Index{X: id("a"), Index: id("i")}, "(index a i)\n"},
	{"range colon", &ast.RangeSelect{X: id("a"), Kind: ast.RangeColon, Left: lit("7"), Right: lit("0")}, "(range: a 7 0)\n"},
	{"range plus", &ast.RangeSelect{X: id("a"), Kind: ast.RangePlus, Left: id("i"), Right: lit("4")}, "(range+: a i 4)\n"},
	{"range minus", &ast.RangeSelect{X: id("a"), Kind: ast.RangeMinus, Left: id("i"), Right: lit("4")}, "(range-: a i 4)\n"},
	{"paren", &ast.Paren{X: id("a")}, "(paren a)\n"},
	{"unary", &ast.Unary{Op: "~&", X: id("a")}, "(unary ~& a)\n"},
	{"binary precedence shows in nesting", &ast.Binary{Op: "+", X: id("a"), Y: &ast.Binary{Op: "*", X: id("b"), Y: id("c")}}, "(+ a (* b c))\n"},
	{"conditional", &ast.Conditional{Cond: id("c"), Then: id("t"), Else: id("e")}, "(?: c t e)\n"},
	{"inside", &ast.Inside{X: id("x"), Ranges: []ast.Expr{lit("1"), &ast.ValueRange{Lo: lit("2"), Hi: lit("3")}}}, "(inside x 1 [2:3])\n"},
	{"open value range", &ast.ValueRange{Lo: unbounded(), Hi: lit("5")}, "[$:5]\n"},
	{"concat", &ast.Concat{Elems: []ast.Expr{id("a"), id("b")}}, "(concat a b)\n"},
	{"replicate", &ast.Replicate{Count: lit("2"), Elems: []ast.Expr{id("a"), id("b")}}, "(repeat 2 a b)\n"},
	{"untyped assignment pattern", &ast.AssignPattern{Items: []ast.Expr{lit("1"), &ast.PatternItem{IsDefault: true, Value: lit("0")}}}, "(pattern 1 (item default 0))\n"},
	{"typed assignment pattern", &ast.AssignPattern{Type: id("T"), Items: []ast.Expr{&ast.PatternItem{Key: id("x"), Value: lit("1")}}}, "(pattern T' (item x 1))\n"},
	{"cast to builtin", &ast.Cast{Type: &ast.TypeExpr{Type: ast.Type{Name: "int"}}, X: id("x")}, "(cast <int> x)\n"},
	{"sign cast", &ast.Cast{Type: &ast.TypeExpr{Type: ast.Type{Signed: true}}, X: id("x")}, "(cast <signed> x)\n"},
	{"size cast", &ast.Cast{Type: lit("8"), X: id("x")}, "(cast 8 x)\n"},
	{"call with every argument form", &ast.Call{
		Func: id("f"),
		Args: []ast.Expr{id("a"), &ast.NamedArg{Name: "n", Value: id("v")}, &ast.NamedArg{Name: "m"}, &ast.Empty{}},
		With: &ast.Paren{X: id("c")},
	}, "(call f a .n(v) .m() (empty) with (paren c))\n"},
	{"system call", &ast.Call{Func: id("$clog2"), Args: []ast.Expr{id("N")}}, "(call $clog2 N)\n"},
	{"bare new", &ast.New{}, "(new)\n"},
	{"scoped new", &ast.New{Scope: id("C"), Args: []ast.Expr{id("a")}}, "(new C:: a)\n"},
	{"copy new", &ast.New{Copy: id("x")}, "(new-copy x)\n"},
	{"new array", &ast.NewArray{Size: lit("4"), Init: id("src")}, "(new-array 4 src)\n"},
	{"new array without init", &ast.NewArray{Size: lit("4")}, "(new-array 4)\n"},
	{"this", &ast.This{}, "this\n"},
	{"super", &ast.Super{}, "super\n"},
	{"mintypmax", &ast.MinTypMax{Min: lit("1"), Typ: lit("2"), Max: lit("3")}, "(mintypmax 1 2 3)\n"},
	{"streaming with slice and with-range", &ast.Streaming{Op: "<<", SliceSize: lit("8"), Elems: []ast.Expr{
		id("a"), &ast.WithRange{X: id("b"), Kind: ast.RangeColon, Left: lit("3"), Right: lit("0")}}},
		"(stream << slice=8 a (with b [3:0]))\n"},
	{"with-range single index", &ast.WithRange{X: id("b"), Left: id("i")}, "(with b [i])\n"},
	{"with-range plus", &ast.WithRange{X: id("b"), Kind: ast.RangePlus, Left: id("i"), Right: lit("4")}, "(with b [i+:4])\n"},
	{"tagged with value", &ast.Tagged{Member: "Valid", X: lit("5")}, "(tagged Valid 5)\n"},
	{"tagged without value", &ast.Tagged{Member: "Invalid"}, "(tagged Invalid)\n"},
	{"type reference", &ast.TypeRef{X: id("x")}, "(typeof x)\n"},
	{"data type in expression position", &ast.TypeExpr{Type: ast.Type{Name: "logic", PackedDims: []ast.Dim{{Left: tk("7"), Right: tk("0")}}}}, "<logic [7:0]>\n"},
	{"type or expr", &ast.TypeOrExpr{X: id("T")}, "(type-or-expr T)\n"},
	{"prefix incdec", &ast.IncDec{Op: "++", Prefix: true, X: id("i")}, "(pre++ i)\n"},
	{"postfix incdec", &ast.IncDec{Op: "--", X: id("i")}, "(post-- i)\n"},
	{"assignment expression", &ast.AssignExpr{Op: "+=", LHS: id("a"), RHS: lit("1")}, "(assign += a 1)\n"},
	{"dist", &ast.Dist{X: id("x"), Items: []ast.DistItem{
		{Value: lit("1")},
		{Value: &ast.ValueRange{Lo: lit("2"), Hi: lit("3")}, Weight: lit("4")},
		{Value: lit("5"), Weight: lit("6"), PerRange: true}}},
		"(dist x 1 (:= [2:3] 4) (:/ 5 6))\n"},
	{"bad expression", &ast.BadExpr{Tokens: tk("a", "+")}, "(bad \"a +\")\n"},
	{"constraint block", &ast.ConstraintBlock{Tokens: tk("x", ">", "0", ";")}, "(constraints \"x > 0 ;\")\n"},
	{"randomize with iterator and constraints",
		&ast.Call{Func: &ast.MemberSelect{X: id("obj"), Member: "randomize"}, With: &ast.Paren{X: id("v")},
			Constraints: &ast.ConstraintBlock{Tokens: tk("v", ">", "0", ";")}},
		"(call (. obj randomize) with (paren v) (constraints \"v > 0 ;\"))\n"},
}

func TestExprDump(t *testing.T) {
	for _, c := range exprCases {
		if got := ast.Dump(c.expr); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

// fileHoldingAllExprs returns a File in which every case expression is
// reachable, as the parsed value of a parameter override on a class-typed
// variable (the one declaration slot that holds a parsed expression so far).
func fileHoldingAllExprs() *ast.File {
	var overrides []ast.ParamOverride
	for _, c := range exprCases {
		overrides = append(overrides, ast.ParamOverride{ValueExpr: c.expr})
	}
	return &ast.File{Path: "exprs.sv", Decls: []ast.Decl{
		&ast.Variable{Name: "v", Type: ast.Type{Name: "cls", ParamOverrides: overrides}},
	}}
}

func TestExprDumpInsideATree(t *testing.T) {
	f := &ast.File{Path: "t.sv", Decls: []ast.Decl{
		&ast.Variable{Name: "v", Type: ast.Type{Name: "cls", ParamOverrides: []ast.ParamOverride{
			{Name: "W", ValueExpr: &ast.Binary{Op: "-", X: id("N"), Y: lit("1")}},
		}}},
	}}
	want := "(file \"t.sv\"\n  (variable v <cls #(.W (- N 1))>))\n"
	if got := ast.Dump(f); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExprWalkOrderIsSourceOrder(t *testing.T) {
	e := &ast.Conditional{
		Cond: &ast.Binary{Op: "<", X: id("a"), Y: id("b")},
		Then: &ast.Call{Func: &ast.MemberSelect{X: id("o"), Member: "m"}, Args: []ast.Expr{id("x")}},
		Else: &ast.Paren{X: id("z")},
	}
	var names []string
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			names = append(names, n.Name)
		case *ast.MemberSelect:
			names = append(names, "."+n.Member)
		}
		return true
	})
	// Pre-order: a MemberSelect is visited before the X it selects from.
	want := []string{"a", "b", ".m", "o", "x", "z"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestExprPositions(t *testing.T) {
	// An expression's Position spans its whole text, and Dump only shows it
	// on request.
	b := &ast.Binary{Op: "+", X: id("a"), Y: id("b")}
	b.Position = ast.Position{Line: 3, Character: 4, EndLine: 3, EndCharacter: 9}
	if got, want := ast.DumpWithPositions(b), "(+ a b) @3:4-3:9\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := ast.Dump(b), "(+ a b)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if b.Pos() != b.Position {
		t.Error("Pos() must return the embedded Position")
	}
}

// exprNodeTypes reads expr.go and returns every struct type that embeds
// exprBase, i.e. every expression node.
func exprNodeTypes(t *testing.T) []string {
	t.Helper()
	f, err := goparser.ParseFile(gotoken.NewFileSet(), "expr.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing expr.go: %v", err)
	}
	var names []string
	for _, d := range f.Decls {
		gd, ok := d.(*goast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*goast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*goast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					if ident, ok := field.Type.(*goast.Ident); ok && ident.Name == "exprBase" {
						names = append(names, ts.Name.Name)
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// TestEveryExprTypeIsExercised keeps exprCases honest: adding an expression
// node to expr.go without a dump/walk case here fails, so a new node cannot
// ship without Walk and Dump support being tested for it. DistItem is
// covered inside the "dist" case.
func TestEveryExprTypeIsExercised(t *testing.T) {
	seen := make(map[string]bool)
	checkWalkComplete(t, fileHoldingAllExprs(), seen)
	types := exprNodeTypes(t)
	if len(types) < 30 {
		t.Fatalf("found only %d expression types in expr.go (%v): the scan is broken", len(types), types)
	}
	for _, name := range types {
		if !seen[name] {
			t.Errorf("expression type %s is not exercised by exprCases", name)
		}
	}
}

func TestDumpNeverFallsBackForExprs(t *testing.T) {
	for _, c := range exprCases {
		if got := ast.Dump(c.expr); strings.Contains(got, "UNHANDLED") {
			t.Errorf("%s: dump hit the fallback: %s", c.name, got)
		}
	}
}

func TestExprStringHandlesEveryNode(t *testing.T) {
	for _, c := range exprCases {
		if got := ast.ExprString(c.expr); strings.Contains(got, "unhandled") {
			t.Errorf("%s: ExprString hit its fallback: %s", c.name, got)
		}
	}
}

func TestTokenSourceKeepsTokensApart(t *testing.T) {
	// Touching tokens may be written without a space only if they still lex as
	// two tokens. "matches" followed by "0" would merge into one identifier, and
	// a carriage return between them is zero-width so they look touching.
	toks := adjacentToks("matches", "0")
	if got := ast.ExprString(&ast.BadExpr{Tokens: toks}); got != "matches 0" {
		t.Errorf("got %q, want a space kept between tokens that would merge", got)
	}
	if got := ast.ExprString(&ast.BadExpr{Tokens: adjacentToks("a", "+", "b")}); got != "a+b" {
		t.Errorf("got %q, want touching tokens that lex apart left touching", got)
	}
}

func TestExprStringExamples(t *testing.T) {
	cases := []struct {
		expr ast.Expr
		want string
	}{
		{&ast.Binary{Op: "+", X: id("a"), Y: &ast.Binary{Op: "*", X: id("b"), Y: id("c")}}, "a + b * c"},
		{&ast.Unary{Op: "-", X: &ast.Unary{Op: "-", X: id("a")}}, "- - a"},
		{&ast.Cast{Type: lit("8"), X: id("x")}, "8'(x)"},
		{&ast.Cast{Type: &ast.TypeOrExpr{X: id("T")}, X: id("x")}, "T'(x)"},
		{&ast.Call{Func: id("f"), Args: []ast.Expr{id("a"), &ast.Empty{}, id("c")}}, "f(a, , c)"},
		{&ast.RangeSelect{X: id("a"), Kind: ast.RangePlus, Left: id("i"), Right: lit("4")}, "a[i +: 4]"},
		{&ast.Streaming{Op: ">>", Elems: []ast.Expr{id("a")}}, "{>> {a}}"},
		{&ast.Ident{Name: `\a+b`}, `\a+b `},
		{&ast.TypeExpr{Type: ast.Type{Name: "bit", PackedDims: []ast.Dim{{Left: adjacentToks("0", "'", "(", "0", ")")}}}}, "bit [0'(0)]"},
		{&ast.BadExpr{Tokens: tk("a", "+")}, "a +"},
		{&ast.TypeExpr{Type: ast.Type{Name: "bit", PackedDims: []ast.Dim{{Left: tk(`\esc`)}}}}, `bit [\esc ]`},
	}
	for _, c := range cases {
		if got := ast.ExprString(c.expr); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// adjacentToks builds tokens laid out back to back on one line, as if they
// had been written with no whitespace.
func adjacentToks(texts ...string) []preprocessor.Token {
	toks := make([]preprocessor.Token, len(texts))
	col := 0
	for i, s := range texts {
		toks[i] = preprocessor.Token{Token: token.Token{Text: s, Character: col}}
		col += len(s)
	}
	return toks
}
