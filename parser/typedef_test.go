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
