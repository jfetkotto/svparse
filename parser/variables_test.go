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
