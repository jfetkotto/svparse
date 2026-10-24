package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestTypedefPlainAlias(t *testing.T) {
	body := moduleBody(t, "typedef logic [7:0] byte_t;")
	td := body[0].(*ast.Typedef)
	if td.Name != "byte_t" {
		t.Fatalf("unexpected name: %+v", td)
	}
	alias, ok := td.Underlying.(*ast.TypeAlias)
	if !ok {
		t.Fatalf("expected *ast.TypeAlias, got %T", td.Underlying)
	}
	if alias.Type.Name != "logic" || len(alias.Type.PackedDims) != 1 {
		t.Fatalf("unexpected alias type: %+v", alias.Type)
	}
}

func TestTypedefMissingSemicolonRecordsErrorAndRecoversNextDecl(t *testing.T) {
	f, errs := parseSrc(t, `package pkg_config;
typedef enum int { OPT_A, OPT_B } mode_t
localparam int WIDTH_B = 16;
endpackage`)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' after mode_t's typedef")
	}
	pkg := f.Decls[0].(*ast.Package)
	if len(pkg.Body) != 2 {
		t.Fatalf("expected both the typedef and WIDTH_B to survive as separate decls, got %+v", pkg.Body)
	}
	td, ok := pkg.Body[0].(*ast.Typedef)
	if !ok || td.Name != "mode_t" {
		t.Fatalf("expected typedef mode_t first, got %+v", pkg.Body[0])
	}
	param, ok := pkg.Body[1].(*ast.Parameter)
	if !ok || param.Name != "WIDTH_B" {
		t.Fatalf("expected WIDTH_B to survive as its own declaration, got %+v", pkg.Body[1])
	}
}

func TestTypedefPlainAliasMissingSemicolonRecordsErrorAndRecoversNextDecl(t *testing.T) {
	f, errs := parseSrc(t, "module top;\ntypedef logic [7:0] byte_t\nlogic done;\nendmodule")
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' after byte_t's typedef")
	}
	c := f.Decls[0].(*ast.Container)
	if len(c.Body) != 2 {
		t.Fatalf("expected both the typedef and done to survive, got %+v", c.Body)
	}
	if _, ok := c.Body[0].(*ast.Typedef); !ok {
		t.Fatalf("expected a typedef first, got %+v", c.Body[0])
	}
	if v, ok := c.Body[1].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done second, got %+v", c.Body[1])
	}
}

func TestTypedefUnterminatedAtEOFRecordsError(t *testing.T) {
	f, errs := parseSrc(t, "module top;\ntypedef logic [7:0] byte_t")
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' with no more input")
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected the module itself still recorded, got %+v", f.Decls)
	}
}

func TestTypedefForwardDeclaration(t *testing.T) {
	body := moduleBody(t, "typedef class my_future_class;")
	td := body[0].(*ast.Typedef)
	if td.Name != "my_future_class" || td.Underlying != nil {
		t.Fatalf("expected a forward declaration with nil Underlying, got %+v", td)
	}
}

func TestTypedefBareForwardDeclaration(t *testing.T) {
	body := moduleBody(t, "typedef my_type_t;")
	td := body[0].(*ast.Typedef)
	if td.Name != "my_type_t" || td.Underlying != nil {
		t.Fatalf("expected a forward declaration, got %+v", td)
	}
}

func TestTypedefStruct(t *testing.T) {
	body := moduleBody(t, `typedef struct packed {
		logic [7:0] addr;
		logic valid;
	} bus_t;`)
	td := body[0].(*ast.Typedef)
	if td.Name != "bus_t" {
		t.Fatalf("unexpected name: %+v", td)
	}
	s, ok := td.Underlying.(*ast.Struct)
	if !ok {
		t.Fatalf("expected *ast.Struct, got %T", td.Underlying)
	}
	if !s.Packed {
		t.Fatalf("expected Packed, got %+v", s)
	}
	if len(s.Members) != 2 {
		t.Fatalf("expected 2 members, got %+v", s.Members)
	}
	if s.Members[0].Name != "addr" || len(s.Members[0].Type.PackedDims) != 1 {
		t.Fatalf("unexpected addr member: %+v", s.Members[0])
	}
	if s.Members[1].Name != "valid" {
		t.Fatalf("unexpected valid member: %+v", s.Members[1])
	}
}

func TestTypedefStructMultiNameMember(t *testing.T) {
	body := moduleBody(t, "typedef struct { logic a, b; } pair_t;")
	s := body[0].(*ast.Typedef).Underlying.(*ast.Struct)
	if len(s.Members) != 2 || s.Members[0].Name != "a" || s.Members[1].Name != "b" {
		t.Fatalf("unexpected members: %+v", s.Members)
	}
}

func TestTypedefUnion(t *testing.T) {
	body := moduleBody(t, "typedef union packed { logic [31:0] w; logic [3:0][7:0] b; } word_t;")
	td := body[0].(*ast.Typedef)
	u, ok := td.Underlying.(*ast.Union)
	if !ok || !u.Packed || len(u.Members) != 2 {
		t.Fatalf("unexpected union: %+v (Underlying %T)", u, td.Underlying)
	}
}

func TestTypedefEnumNoBaseType(t *testing.T) {
	body := moduleBody(t, "typedef enum { RED, GREEN, BLUE } color_t;")
	td := body[0].(*ast.Typedef)
	e, ok := td.Underlying.(*ast.Enum)
	if !ok {
		t.Fatalf("expected *ast.Enum, got %T", td.Underlying)
	}
	if e.BaseType.Name != "" {
		t.Fatalf("expected no base type, got %+v", e.BaseType)
	}
	if len(e.Members) != 3 {
		t.Fatalf("expected 3 members, got %+v", e.Members)
	}
	names := []string{"RED", "GREEN", "BLUE"}
	for i, want := range names {
		if e.Members[i].Name != want {
			t.Fatalf("member %d = %q, want %q", i, e.Members[i].Name, want)
		}
	}
}

func TestTypedefEnumWithBaseTypeAndValues(t *testing.T) {
	body := moduleBody(t, "typedef enum logic [1:0] { IDLE = 0, BUSY = 1, DONE = 2 } state_t;")
	td := body[0].(*ast.Typedef)
	e := td.Underlying.(*ast.Enum)
	if e.BaseType.Name != "logic" || len(e.BaseType.PackedDims) != 1 {
		t.Fatalf("unexpected base type: %+v", e.BaseType)
	}
	if len(e.Members) != 3 {
		t.Fatalf("expected 3 members, got %+v", e.Members)
	}
	if e.Members[1].Name != "BUSY" || len(e.Members[1].Value) != 1 || e.Members[1].Value[0].Text != "1" {
		t.Fatalf("unexpected BUSY member: %+v", e.Members[1])
	}
}

func TestTypedefEnumMemberWithoutValue(t *testing.T) {
	body := moduleBody(t, "typedef enum { A, B = 5, C } e_t;")
	e := body[0].(*ast.Typedef).Underlying.(*ast.Enum)
	if len(e.Members[0].Value) != 0 {
		t.Fatalf("expected A to have no explicit value, got %+v", e.Members[0])
	}
	if len(e.Members[1].Value) != 1 || e.Members[1].Value[0].Text != "5" {
		t.Fatalf("unexpected B: %+v", e.Members[1])
	}
	if len(e.Members[2].Value) != 0 {
		t.Fatalf("expected C to have no explicit value, got %+v", e.Members[2])
	}
}
