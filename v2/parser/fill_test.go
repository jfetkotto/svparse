package parser

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
)

// rawSpanSiblings maps every raw expression span field in package ast to the
// field holding its parsed form. TestEveryRawSpanFieldHasASibling fails if a
// new []preprocessor.Token field appears in ast without an entry here, so a
// new raw span cannot ship without fillExprs (and this file) learning it.
var rawSpanSiblings = map[string]string{
	"Dim.Left":            "Dim.LeftExpr",
	"Dim.Right":           "Dim.RightExpr",
	"Variable.Initial":    "Variable.InitialExpr",
	"Parameter.Default":   "Parameter.DefaultExpr",
	"EnumMember.Value":    "EnumMember.ValueExpr",
	"Port.Default":        "Port.DefaultExpr",
	"Arg.Default":         "Arg.DefaultExpr",
	"Class.ExtendsArgs":   "Class.ExtendsArgExprs",
	"ParamOverride.Value": "ParamOverride.ValueExpr",
	"PortConnection.Expr": "PortConnection.ConnExpr",
}

// notDeclarationSpans are []preprocessor.Token fields that are not raw
// expression spans of a declaration.
var notDeclarationSpans = map[string]bool{
	"BadExpr.Tokens": true, // the tokens a BadExpr stands for
	"RawStmt.Tokens": true, // the tokens a recognised-but-unmodeled statement stands for
	"BadStmt.Tokens": true, // the tokens a BadStmt stands for
	// An inline constraint block keeps its text beside the parsed Items, and a
	// constraint item that could not be parsed keeps what it covered.
	"ConstraintBlock.Tokens": true,
	"ConstraintRaw.Tokens":   true,
}

func TestEveryRawSpanFieldHasASibling(t *testing.T) {
	files, err := filepath.Glob("../ast/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no ast sources found: %v", err)
	}
	var found []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := goparser.ParseFile(gotoken.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
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
					arr, ok := field.Type.(*goast.ArrayType)
					if !ok {
						continue
					}
					sel, ok := arr.Elt.(*goast.SelectorExpr)
					if !ok || sel.Sel.Name != "Token" {
						continue
					}
					for _, name := range field.Names {
						found = append(found, ts.Name.Name+"."+name.Name)
					}
				}
			}
		}
	}
	sort.Strings(found)
	if len(found) < len(rawSpanSiblings) {
		t.Fatalf("scan found only %v: it is broken", found)
	}
	for _, f := range found {
		if _, ok := rawSpanSiblings[f]; !ok && !notDeclarationSpans[f] {
			t.Errorf("ast field %s is a raw token span with no parsed sibling in rawSpanSiblings", f)
		}
	}
}

// parseDecls preprocesses and parses src, returning every preprocessor and
// parser error together.
func parseDecls(t *testing.T, src string) (*ast.File, []string) {
	t.Helper()
	toks, ppErrs := preprocessor.Preprocess("t.sv", src, nil)
	f, errs := Parse("t.sv", toks)
	var msgs []string
	for _, e := range ppErrs {
		msgs = append(msgs, e.Message)
	}
	for _, e := range errs {
		msgs = append(msgs, e.Message)
	}
	return f, msgs
}

// find returns the first node of type T in f.
func find[T ast.Node](f *ast.File) (found T, ok bool) {
	ast.Inspect(f, func(n ast.Node) bool {
		if v, isT := n.(T); isT && !ok {
			found, ok = v, true
		}
		return !ok
	})
	return found, ok
}

// spanOracle checks a raw span against its parsed sibling: present exactly
// when the span is non-empty, and a BadExpr over the whole span exactly when
// ParseExpr rejects it.
func spanOracle(t *testing.T, where string, raw []preprocessor.Token, got ast.Expr, wildcard bool) {
	t.Helper()
	if len(raw) == 0 {
		if got != nil {
			t.Errorf("%s: empty span but sibling is %T", where, got)
		}
		return
	}
	if wildcard {
		if got != nil {
			t.Errorf("%s: wildcard dimension should have no expression, got %T", where, got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s: span %q has no parsed sibling", where, strings.Join(tokenTexts(raw), " "))
		return
	}
	_, errs := ParseExpr(raw)
	bad, isBad := got.(*ast.BadExpr)
	switch {
	case len(errs) > 0 && !isBad:
		t.Errorf("%s: ParseExpr rejects %q but the sibling is %T", where, tokenTexts(raw), got)
	case len(errs) == 0 && isBad:
		t.Errorf("%s: ParseExpr accepts %q but the sibling is a BadExpr", where, strings.Join(tokenTexts(raw), " "))
	case isBad && len(bad.Tokens) != len(raw):
		t.Errorf("%s: BadExpr covers %d of %d tokens", where, len(bad.Tokens), len(raw))
	}
}

// TestFillExprsCoversEveryRawSpanInTheCorpus walks all 1014 vendored files:
// every raw span must have its sibling, parsed or BadExpr as ParseExpr says.
func TestFillExprsCoversEveryRawSpanInTheCorpus(t *testing.T) {
	slots := 0
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(corpusDir, path)
		toks, _ := preprocessor.Preprocess(rel, string(data), corpusIncludeResolver{})
		f, _ := Parse(rel, toks)

		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			at := func(kind string) string { return rel + ":" + strconv.Itoa(n.Pos().Line+1) + " " + kind }
			check := func(kind string, raw []preprocessor.Token, e ast.Expr) {
				slots++
				spanOracle(t, at(kind), raw, e, false)
			}
			dims := func(kind string, ds []ast.Dim) {
				for _, d := range ds {
					slots += 2
					spanOracle(t, at(kind+" Left"), d.Left, d.LeftExpr, d.Wildcard)
					spanOracle(t, at(kind+" Right"), d.Right, d.RightExpr, false)
				}
			}
			switch n := n.(type) {
			case *ast.Variable:
				check("Variable.Initial", n.Initial, n.InitialExpr)
				dims("Variable dim", n.UnpackedDims)
			case *ast.Parameter:
				check("Parameter.Default", n.Default, n.DefaultExpr)
				dims("Parameter dim", n.UnpackedDims)
			case *ast.EnumMember:
				check("EnumMember.Value", n.Value, n.ValueExpr)
			case *ast.Port:
				check("Port.Default", n.Default, n.DefaultExpr)
				dims("Port dim", n.UnpackedDims)
			case *ast.PortDecl:
				dims("PortDecl dim", n.UnpackedDims)
			case *ast.Arg:
				check("Arg.Default", n.Default, n.DefaultExpr)
				dims("Arg dim", n.UnpackedDims)
			case *ast.Instance:
				dims("Instance dim", n.UnpackedDims)
			case *ast.Type:
				dims("Type dim", n.PackedDims)
			case *ast.ParamOverride:
				check("ParamOverride.Value", n.Value, n.ValueExpr)
			case *ast.PortConnection:
				check("PortConnection.Expr", n.Expr, n.ConnExpr)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if slots < 3000 {
		t.Fatalf("only %d span slots checked: the walk is broken", slots)
	}
}

func TestFillMalformedSpanBecomesBadExprWithoutNewErrors(t *testing.T) {
	// "1 + * 2" cannot be an expression, but the declaration parser has always
	// accepted it as an opaque span, and filling must not start complaining.
	f, errs := parseDecls(t, "module m; localparam int P = 1 + * 2; endmodule")
	if len(errs) != 0 {
		t.Fatalf("a malformed expression must not add diagnostics, got %v", errs)
	}
	p, ok := find[*ast.Parameter](f)
	if !ok {
		t.Fatal("no parameter")
	}
	bad, isBad := p.DefaultExpr.(*ast.BadExpr)
	if !isBad {
		t.Fatalf("DefaultExpr = %T, want *ast.BadExpr", p.DefaultExpr)
	}
	if len(bad.Tokens) != 4 || len(p.Default) != 4 {
		t.Errorf("BadExpr covers %d tokens, raw span has %d, want both 4", len(bad.Tokens), len(p.Default))
	}
	if bad.Pos().Character != p.Default[0].Character {
		t.Errorf("BadExpr starts at %d, want the span start %d", bad.Pos().Character, p.Default[0].Character)
	}
}

func TestFillKeepsRawSpansAlongsideParsedOnes(t *testing.T) {
	f, _ := parseDecls(t, "module m; localparam int P = 1 + 2; endmodule")
	p, _ := find[*ast.Parameter](f)
	if len(p.Default) != 3 || p.DefaultExpr == nil {
		t.Errorf("raw span has %d tokens, DefaultExpr = %v: both must be populated", len(p.Default), p.DefaultExpr)
	}
}

func TestFillDimensions(t *testing.T) {
	f, errs := parseDecls(t, `module m;
  int wild [*];
  logic [W-1:0] range;
  logic lone [N];
  logic queue [$];
  logic [3:0][7:0] two;
endmodule`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	got := dropFileLine(ast.Dump(f))
	want := `(module m
  (variable wild <int> [*])
  (variable range <logic [(- W 1):0]>)
  (variable lone <logic> [(type-or-expr N)])
  (variable queue <logic> [$])
  (variable two <logic [3:0] [7:0]>))`
	if got != want {
		t.Errorf("dimension dump\n got:\n%s\nwant:\n%s", got, want)
	}

	w, _ := find[*ast.Variable](f)
	if !w.UnpackedDims[0].Wildcard || w.UnpackedDims[0].LeftExpr != nil || len(w.UnpackedDims[0].Left) != 1 {
		t.Errorf("wildcard dimension = %+v: want Wildcard with no expression and the raw * kept", w.UnpackedDims[0])
	}
}

func TestFillSharedTypeDimensionsAreIndependentNodes(t *testing.T) {
	// "logic [W-1:0] a, b;" gives a and b equal Types sharing one dimension
	// array. Each must own its expression nodes, or the tree is a graph.
	f, _ := parseDecls(t, "module m; logic [W-1:0] a, b; endmodule")
	var vars []*ast.Variable
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			vars = append(vars, v)
		}
		return true
	})
	if len(vars) != 2 {
		t.Fatalf("got %d variables", len(vars))
	}
	a, b := vars[0].Type.PackedDims[0].LeftExpr, vars[1].Type.PackedDims[0].LeftExpr
	if a == nil || b == nil || a == b {
		t.Fatalf("expression nodes must be distinct, got %p and %p", a, b)
	}
	if ast.Dump(a) != ast.Dump(b) {
		t.Errorf("but equal in content: %s vs %s", ast.Dump(a), ast.Dump(b))
	}
}

func TestFillTypeParameterDefaultsAreTypes(t *testing.T) {
	f, errs := parseDecls(t, "module m #(parameter type T = int, parameter type U = foo_t, parameter int N = 4) (); endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var got []string
	ast.Inspect(f, func(n ast.Node) bool {
		if p, ok := n.(*ast.Parameter); ok {
			got = append(got, p.Name+"="+strings.TrimSpace(ast.Dump(p.DefaultExpr)))
		}
		return true
	})
	want := "T=<int> U=(type-or-expr foo_t) N=4"
	if strings.Join(got, " ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, " "), want)
	}
}

func TestFillInstantiationParamsAndConnections(t *testing.T) {
	f, errs := parseDecls(t, "module m; leaf #(.W(8), .T(int), 3) u (.clk(clk), .d(a + 1), .e(), .*); endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	inst, _ := find[*ast.Instantiation](f)
	var overrides, conns []string
	for _, o := range inst.ParamOverrides {
		overrides = append(overrides, o.Name+":"+strings.TrimSpace(ast.Dump(o.ValueExpr)))
	}
	for _, c := range inst.Instances[0].Connections {
		conns = append(conns, c.Name+":"+strings.TrimSpace(dumpOrNil(c.ConnExpr)))
	}
	if got, want := strings.Join(overrides, " "), "W:8 T:<int> :3"; got != want {
		t.Errorf("overrides %q, want %q", got, want)
	}
	if got, want := strings.Join(conns, " "), "clk:clk d:(+ a 1) e:<nil> :<nil>"; got != want {
		t.Errorf("connections %q, want %q", got, want)
	}
}

func dumpOrNil(e ast.Expr) string {
	if e == nil {
		return "<nil>"
	}
	return ast.Dump(e)
}

func TestFillClassExtendsArguments(t *testing.T) {
	f, errs := parseDecls(t, "class c extends base(1, x + 2); endclass")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	c, _ := find[*ast.Class](f)
	if len(c.ExtendsArgExprs) != 2 {
		t.Fatalf("got %d argument expressions, want 2", len(c.ExtendsArgExprs))
	}
	if got := strings.TrimSpace(ast.Dump(c.ExtendsArgExprs[1])); got != "(+ x 2)" {
		t.Errorf("second argument = %s", got)
	}
	if !strings.Contains(ast.Dump(c), "extends-args=[1 (+ x 2)]") {
		t.Errorf("dump: %s", ast.Dump(c))
	}
}

func TestFillEnumValuesAndDefaults(t *testing.T) {
	f, errs := parseDecls(t, `module m;
  typedef enum { A = 1, B = A + 1 } e_t;
  function int f(int x = 3, y = x * 2); endfunction
endmodule`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var got []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.EnumMember:
			got = append(got, n.Name+"="+strings.TrimSpace(dumpOrNil(n.ValueExpr)))
		case *ast.Arg:
			got = append(got, n.Name+"="+strings.TrimSpace(dumpOrNil(n.DefaultExpr)))
		}
		return true
	})
	if want := "A=1 B=(+ A 1) x=3 y=(* x 2)"; strings.Join(got, " ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, " "), want)
	}
}

// dropFileLine strips the "(file ...)" wrapper from a Dump of a single
// top-level declaration, leaving that declaration's own dump with its
// indentation reduced by one level and the file's closing parenthesis gone.
func dropFileLine(dump string) string {
	lines := strings.Split(strings.TrimRight(dump, "\n"), "\n")
	body := lines[1:]
	for i, l := range body {
		body[i] = strings.TrimPrefix(l, "  ")
	}
	out := strings.Join(body, "\n")
	return strings.TrimSuffix(out, ")")
}
