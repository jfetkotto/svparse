package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func moduleBody(t *testing.T, src string) []ast.Decl {
	t.Helper()
	f, errs := parseSrc(t, "module top;\n"+src+"\nendmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	c := f.Decls[0].(*ast.Container)
	return c.Body
}

func TestVariableUwireDeclaration(t *testing.T) {
	// "uwire" (IEEE 1800-2017 Annex B) is a reserved word, not a plain
	// identifier -- confirm it dispatches through the ordinary net-type
	// keyword path (parseDecl's isVariableStartKeyword check) rather than
	// being misparsed as an instantiation.
	body := moduleBody(t, "uwire w;")
	v := body[0].(*ast.Variable)
	if v.Type.Name != "uwire" || v.Name != "w" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableSingleDeclaration(t *testing.T) {
	body := moduleBody(t, "logic [7:0] data;")
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	v := body[0].(*ast.Variable)
	if v.Type.Name != "logic" || v.Name != "data" || len(v.Type.PackedDims) != 1 {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableWithConstQualifier(t *testing.T) {
	body := moduleBody(t, "const int limit = 10;")
	v := body[0].(*ast.Variable)
	if v.Type.Name != "int" || v.Name != "limit" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableWithConstVarQualifier(t *testing.T) {
	// "const var" -- const's own optional "var", both consumed.
	body := moduleBody(t, "const var int limit = 10;")
	v := body[0].(*ast.Variable)
	if v.Type.Name != "int" || v.Name != "limit" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableWithConstAndUserDefinedType(t *testing.T) {
	// "const" is itself an unambiguous enough signal that the identifier
	// type doesn't need the general instantiation-disambiguation logic --
	// same reasoning as TestClassWithRandUserDefinedType.
	body := moduleBody(t, "const packet_t pkt = new;")
	v := body[0].(*ast.Variable)
	if v.Type.Name != "packet_t" || v.Name != "pkt" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableWithVarQualifier(t *testing.T) {
	body := moduleBody(t, "var logic [7:0] data;")
	v := body[0].(*ast.Variable)
	if v.Type.Name != "logic" || v.Name != "data" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableMultiNameSharesOneType(t *testing.T) {
	body := moduleBody(t, "logic [7:0] a, b, c;")
	if len(body) != 3 {
		t.Fatalf("expected 3 decls, got %+v", body)
	}
	for i, name := range []string{"a", "b", "c"} {
		v := body[i].(*ast.Variable)
		if v.Name != name || v.Type.Name != "logic" || len(v.Type.PackedDims) != 1 {
			t.Fatalf("unexpected variable %d: %+v", i, v)
		}
	}
}

func TestVariableWithInitialValue(t *testing.T) {
	body := moduleBody(t, "int count = 0;")
	v := body[0].(*ast.Variable)
	if v.Name != "count" || len(v.Initial) != 1 || v.Initial[0].Text != "0" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableWithUnpackedArrayDim(t *testing.T) {
	body := moduleBody(t, "logic [7:0] mem [256];")
	v := body[0].(*ast.Variable)
	if len(v.Type.PackedDims) != 1 || len(v.UnpackedDims) != 1 {
		t.Fatalf("unexpected variable: %+v", v)
	}
	if v.UnpackedDims[0].Left[0].Text != "256" {
		t.Fatalf("unexpected unpacked dim: %+v", v.UnpackedDims[0])
	}
}

func TestVariableMixedInitializersInOneStatement(t *testing.T) {
	body := moduleBody(t, "int a = 1, b, c = 3;")
	if len(body) != 3 {
		t.Fatalf("expected 3 decls, got %+v", body)
	}
	a, b, c := body[0].(*ast.Variable), body[1].(*ast.Variable), body[2].(*ast.Variable)
	if len(a.Initial) != 1 || a.Initial[0].Text != "1" {
		t.Fatalf("unexpected a: %+v", a)
	}
	if len(b.Initial) != 0 {
		t.Fatalf("unexpected b: %+v", b)
	}
	if len(c.Initial) != 1 || c.Initial[0].Text != "3" {
		t.Fatalf("unexpected c: %+v", c)
	}
}

func TestParameterDeclImplicitTypeSharedAcrossNames(t *testing.T) {
	body := moduleBody(t, "parameter WIDTH = 8, DEPTH = 4;")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls, got %+v", body)
	}
	w := body[0].(*ast.Parameter)
	if w.Name != "WIDTH" || w.Type.Name != "" || len(w.Default) != 1 || w.Default[0].Text != "8" {
		t.Fatalf("unexpected WIDTH: %+v", w)
	}
	d := body[1].(*ast.Parameter)
	if d.Name != "DEPTH" || len(d.Default) != 1 || d.Default[0].Text != "4" {
		t.Fatalf("unexpected DEPTH: %+v", d)
	}
}

func TestParameterDeclExplicitTypeSharedAcrossNames(t *testing.T) {
	body := moduleBody(t, "parameter int A = 1, B = 2;")
	a := body[0].(*ast.Parameter)
	b := body[1].(*ast.Parameter)
	if a.Type.Name != "int" || b.Type.Name != "int" {
		t.Fatalf("expected both A and B to share type int, got %+v and %+v", a, b)
	}
}

func TestParameterDeclNestedCommaInDefaultNotSplit(t *testing.T) {
	body := moduleBody(t, "parameter int A = foo(1,2), B = 3;")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls, got %+v", body)
	}
	a := body[0].(*ast.Parameter)
	if tokenTexts(a.Default) == nil {
		t.Fatalf("expected a default value for A")
	}
	want := []string{"foo", "(", "1", ",", "2", ")"}
	got := tokenTexts(a.Default)
	if len(got) != len(want) {
		t.Fatalf("A.Default = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("A.Default = %v, want %v", got, want)
		}
	}
	b := body[1].(*ast.Parameter)
	if b.Name != "B" || len(b.Default) != 1 || b.Default[0].Text != "3" {
		t.Fatalf("unexpected B: %+v", b)
	}
}

func TestParameterDeclMissingSemicolonRecordsErrorAndRecoversNextDecl(t *testing.T) {
	f, errs := parseSrc(t, `package pkg_config;
localparam int WIDTH_A = 8
localparam int WIDTH_B = 16;
endpackage`)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' after WIDTH_A's declaration")
	}
	pkg := f.Decls[0].(*ast.Package)
	if len(pkg.Body) != 2 {
		t.Fatalf("expected both WIDTH_A and WIDTH_B to survive as separate decls, got %+v", pkg.Body)
	}
	a, ok := pkg.Body[0].(*ast.Parameter)
	if !ok || a.Name != "WIDTH_A" {
		t.Fatalf("expected WIDTH_A first, got %+v", pkg.Body[0])
	}
	if len(a.Default) != 1 || a.Default[0].Text != "8" {
		t.Fatalf("expected WIDTH_A's default to be just \"8\" (not tokens stolen from WIDTH_B), got %+v", a.Default)
	}
	b, ok := pkg.Body[1].(*ast.Parameter)
	if !ok || b.Name != "WIDTH_B" || len(b.Default) != 1 || b.Default[0].Text != "16" {
		t.Fatalf("expected WIDTH_B to be parsed independently, got %+v", pkg.Body[1])
	}
}

func TestParameterDeclDefaultWithTypeCastIsNotMistakenForNextDecl(t *testing.T) {
	// "int'(x)" (LRM 6.24.1) starts with the same "int" keyword text
	// isDeclBoundaryKeyword otherwise treats as a new declaration's type
	// -- it must not be mistaken for one when it's really the cast
	// operator of this parameter's own default value expression.
	body := moduleBody(t, "parameter int W = int'(x);")
	w := body[0].(*ast.Parameter)
	want := []string{"int", "'", "(", "x", ")"}
	got := tokenTexts(w.Default)
	if len(got) != len(want) {
		t.Fatalf("W.Default = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("W.Default = %v, want %v", got, want)
		}
	}
}

func TestParameterDeclTypeParameterDefaultIsBareTypeKeyword(t *testing.T) {
	// "parameter type name = logic;" (LRM 6.20.4) -- typ.Name == "type"
	// here, and the default value is itself a type, so a bare builtin
	// type keyword ("logic") is legitimate, expected content, not a sign
	// the next declaration has begun. Found via the sv-tests corpus
	// baseline (chapter-6/6.23--localparam_type_decl.sv) regressing
	// during development of the decl-boundary check.
	body := moduleBody(t, "localparam type testtype = logic;\ntesttype t;")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls (testtype, t), got %+v", body)
	}
	p, ok := body[0].(*ast.Parameter)
	if !ok || p.Name != "testtype" || p.Type.Name != "type" {
		t.Fatalf("unexpected testtype param: %+v", body[0])
	}
	if len(p.Default) != 1 || p.Default[0].Text != "logic" {
		t.Fatalf("expected testtype's default to be the bare keyword \"logic\", got %+v", p.Default)
	}
	v, ok := body[1].(*ast.Variable)
	if !ok || v.Name != "t" {
		t.Fatalf("expected variable t declared with type testtype, got %+v", body[1])
	}
}

func TestLocalparamDecl(t *testing.T) {
	body := moduleBody(t, "localparam int MAX = 255;")
	p := body[0].(*ast.Parameter)
	if !p.IsLocal || p.Name != "MAX" {
		t.Fatalf("unexpected param: %+v", p)
	}
}

func TestParameterDeclWithUnpackedDim(t *testing.T) {
	body := moduleBody(t, "localparam pa_FooBar::ty_Baz CUQ[1:0] = '0;")
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	p := body[0].(*ast.Parameter)
	if !p.IsLocal || p.Name != "CUQ" {
		t.Fatalf("unexpected param: %+v", p)
	}
	if p.Type.PackageQualifier != "pa_FooBar" || p.Type.Name != "ty_Baz" {
		t.Fatalf("unexpected type: %+v", p.Type)
	}
	if len(p.UnpackedDims) != 1 || len(p.UnpackedDims[0].Left) != 1 || p.UnpackedDims[0].Left[0].Text != "1" ||
		len(p.UnpackedDims[0].Right) != 1 || p.UnpackedDims[0].Right[0].Text != "0" {
		t.Fatalf("unexpected unpacked dim: %+v", p.UnpackedDims)
	}
	if len(p.Default) != 1 || p.Default[0].Text != "'0" {
		t.Fatalf("unexpected default: %+v", p.Default)
	}
}

func TestParameterDeclWithUnpackedDimMultipleNames(t *testing.T) {
	body := moduleBody(t, "localparam int A[1:0] = '0, B = 1;")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls, got %+v", body)
	}
	a := body[0].(*ast.Parameter)
	if len(a.UnpackedDims) != 1 {
		t.Fatalf("expected A to have 1 unpacked dim, got %+v", a.UnpackedDims)
	}
	b := body[1].(*ast.Parameter)
	if len(b.UnpackedDims) != 0 {
		t.Fatalf("expected B to have no unpacked dims, got %+v", b.UnpackedDims)
	}
	if b.Name != "B" || len(b.Default) != 1 || b.Default[0].Text != "1" {
		t.Fatalf("unexpected B: %+v", b)
	}
}

// The statement form of the same [net_type] data_type shape ports use
// (LRM 6.7.1) -- see consumeNetTypeQualifier.
func TestVariableNetTypeKeywordBeforeDataType(t *testing.T) {
	body := moduleBody(t, "wire logic foo;\nwire logic [3:0] bar;\nwire cfg_t baz;\nwire pkg_cfg::cfg_t qux;")
	if len(body) != 4 {
		t.Fatalf("expected 4 decls, got %+v", body)
	}
	foo := body[0].(*ast.Variable)
	if foo.Type.Name != "logic" || foo.Name != "foo" {
		t.Fatalf("unexpected foo: %+v", foo)
	}
	bar := body[1].(*ast.Variable)
	if bar.Type.Name != "logic" || bar.Name != "bar" || len(bar.Type.PackedDims) != 1 {
		t.Fatalf("unexpected bar: %+v", bar)
	}
	baz := body[2].(*ast.Variable)
	if baz.Type.Name != "cfg_t" || baz.Name != "baz" {
		t.Fatalf("unexpected baz: %+v", baz)
	}
	qux := body[3].(*ast.Variable)
	if qux.Type.PackageQualifier != "pkg_cfg" || qux.Type.Name != "cfg_t" || qux.Name != "qux" {
		t.Fatalf("unexpected qux: %+v", qux)
	}
}

// A net-type keyword that IS the declaration's type must stay that way --
// the lookahead in consumeNetTypeQualifier is what keeps these (and
// TestVariableUwireDeclaration above) parsing as they always did.
func TestVariableBareNetTypeKeywordRemainsTheType(t *testing.T) {
	for name, tc := range map[string]struct {
		src   string
		count int
	}{
		"no data type": {"wire w;", 1},
		"packed dims":  {"wire [3:0] w;", 1},
		"signed":       {"wire signed [3:0] w;", 1},
		"comma list":   {"wire w, x;", 2},
	} {
		t.Run(name, func(t *testing.T) {
			body := moduleBody(t, tc.src)
			if len(body) != tc.count {
				t.Fatalf("expected %d decls, got %+v", tc.count, body)
			}
			v := body[0].(*ast.Variable)
			if v.Type.Name != "wire" || v.Name != "w" {
				t.Fatalf("unexpected variable: %+v", v)
			}
		})
	}
}
