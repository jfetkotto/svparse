package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

const inlineAggregateSource = `module m (input struct packed { logic a; logic [1:0] b; } p, output enum logic [1:0] {X, Y} q);
  struct packed { logic [7:0] lo; logic [7:0] hi; } word, other;
  enum integer { A = 0, B = 2 } e1;
  union tagged packed { bit [3:0] v1; void none; } u;
  struct { struct { int i; } inner; enum { P, Q } kind; } nested;
  struct packed { logic a; } [3:0] arr;
  function struct packed { logic ok; } status(input enum { LOW, HIGH } level); endfunction
  parameter struct packed { int w; } DEFAULTS = '{w: 1};
endmodule`

// TestInlineAggregatesInEveryTypePosition pins the tree for an anonymous
// struct, union, or enum used as a port type, a variable type (sharing one
// declaration between two names), a union and enum with qualifiers, a nested
// member type, a type with packed dimensions after the body, a function
// return and argument type, and a parameter type.
func TestInlineAggregatesInEveryTypePosition(t *testing.T) {
	f, errs := parseDecls(t, inlineAggregateSource)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	const want = `(file "t.sv"
  (module m
    (port input p <struct packed>
      (struct packed
        (variable a <logic>)
        (variable b <logic [1:0]>)))
    (port output q <enum>
      (enum <logic [1:0]>
        (enum-member X)
        (enum-member Y)))
    (variable word <struct packed>
      (struct packed
        (variable lo <logic [7:0]>)
        (variable hi <logic [7:0]>)))
    (variable other <struct packed>
      (struct packed
        (variable lo <logic [7:0]>)
        (variable hi <logic [7:0]>)))
    (variable e1 <enum>
      (enum <integer>
        (enum-member A = 0)
        (enum-member B = 2)))
    (variable u <union tagged packed>
      (union tagged packed
        (variable v1 <bit [3:0]>)
        (variable none <void>)))
    (variable nested <struct>
      (struct
        (variable inner <struct>
          (struct
            (variable i <int>)))
        (variable kind <enum>
          (enum
            (enum-member P)
            (enum-member Q)))))
    (variable arr <struct packed [3:0]>
      (struct packed
        (variable a <logic>)))
    (function status <struct packed>
      (struct packed
        (variable ok <logic>))
      (arg input level <enum>
        (enum
          (enum-member LOW)
          (enum-member HIGH))))
    (parameter DEFAULTS <struct packed> = (pattern (item w 1))
      (struct packed
        (variable w <int>)))))
`
	if got := ast.Dump(f); got != want {
		t.Errorf("dump mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestInlineAggregateRecordsQualifiers(t *testing.T) {
	f, _ := parseDecls(t, `module m;
  struct packed signed { logic [3:0] a; } s;
  struct packed unsigned { logic [3:0] a; } u;
  union tagged packed { int a; } tp;
  union { int a; } plain;
endmodule`)
	byName := map[string]*ast.Variable{}
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok && v.Type.Inline != nil {
			byName[v.Name] = v
		}
		return true
	})
	if v := byName["s"]; !v.Type.Signed || v.Type.Unsigned || !v.Type.Inline.(*ast.Struct).Packed {
		t.Errorf("s: signed=%v unsigned=%v, want signed packed struct", v.Type.Signed, v.Type.Unsigned)
	}
	if v := byName["u"]; v.Type.Signed || !v.Type.Unsigned {
		t.Errorf("u: signed=%v unsigned=%v, want unsigned", v.Type.Signed, v.Type.Unsigned)
	}
	if u := byName["tp"].Type.Inline.(*ast.Union); !u.Tagged || !u.Packed {
		t.Errorf("tp: tagged=%v packed=%v, want both", u.Tagged, u.Packed)
	}
	if u := byName["plain"].Type.Inline.(*ast.Union); u.Tagged || u.Packed {
		t.Errorf("plain: tagged=%v packed=%v, want neither", u.Tagged, u.Packed)
	}
}

func TestInlineAggregateSpans(t *testing.T) {
	// The aggregate spans from its keyword through the closing brace; the
	// Type's own Position is the keyword token (a Type is positioned on its name).
	f, _ := parseDecls(t, "module m;\n  struct packed {\n    logic a;\n  } s;\nendmodule")
	v, _ := find[*ast.Variable](f)
	s := v.Type.Inline.(*ast.Struct)
	if p := s.Pos(); p.Line != 1 || p.Character != 2 || p.EndLine != 3 || p.EndCharacter != 3 {
		t.Errorf("struct span = %+v, want 1:2 through 3:3", p)
	}
	if p := v.Type.Pos(); p.Line != 1 || p.Character != 2 || p.EndCharacter != 8 {
		t.Errorf("type position = %+v, want the keyword at 1:2-1:8", p)
	}
}

func TestInlineAggregateDeclaratorsOwnTheirCopy(t *testing.T) {
	// "struct {...} a, b;" is one type for two variables, but the tree must
	// not share nodes: each variable gets its own aggregate, at the same position.
	f, _ := parseDecls(t, "module m; struct { int x; } a, b; endmodule")
	var aggs []*ast.Struct
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.Struct); ok {
			aggs = append(aggs, s)
		}
		return true
	})
	if len(aggs) != 2 {
		t.Fatalf("found %d aggregates, want one per variable", len(aggs))
	}
	if aggs[0] == aggs[1] || &aggs[0].Members[0] == &aggs[1].Members[0] {
		t.Error("the two variables share an aggregate or its members")
	}
	if aggs[0].Pos() != aggs[1].Pos() {
		t.Errorf("copies must keep the same position (that is how one-type is recoverable): %+v vs %+v", aggs[0].Pos(), aggs[1].Pos())
	}
}

func TestParameterizedTypeDeclaratorsOwnTheirOverrides(t *testing.T) {
	// "cls #(8) c, d;" gave both variables one ParamOverride array, so Walk
	// visited it twice and a fill hung one expression under both.
	f, _ := parseDecls(t, "module m; cls #(8) c, d; endmodule")
	var vars []*ast.Variable
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			vars = append(vars, v)
		}
		return true
	})
	a, b := vars[0].Type.ParamOverrides, vars[1].Type.ParamOverrides
	if len(a) != 1 || len(b) != 1 || &a[0] == &b[0] || a[0].ValueExpr == b[0].ValueExpr {
		t.Errorf("overrides are shared: %p %p, exprs %p %p", &a[0], &b[0], a[0].ValueExpr, b[0].ValueExpr)
	}
}

// TestInlineAggregateMalformedInputRecovers pins the blast radius of a
// broken aggregate: whatever it is, the NEXT module must still be found as its
// own top-level module, never read into the broken one. Both the inline and
// the typedef form are covered, since they share the body parser.
func TestInlineAggregateMalformedInputRecovers(t *testing.T) {
	const next = "\nmodule n; logic fine; endmodule"
	cases := []struct {
		name, src string
		wantErr   string
	}{
		{"missing body", "module m; struct x; logic ok; endmodule" + next, "expected '{' to start struct/union body"},
		{"missing member semicolon", "module m; struct { int a } s; logic ok; endmodule" + next, "unterminated struct/union"},
		{"missing closing brace", "module m; struct { int a; logic ok; endmodule" + next, "unterminated struct/union body"},
		{"missing closing brace, union", "module m; union { int a; endmodule" + next, "unterminated struct/union body"},
		{"enum missing closing brace", "module m; enum { A, B ; logic ok; endmodule" + next, "unterminated enum body"},
		{"enum without body", "module m; enum logic [1:0] e; logic ok; endmodule" + next, "expected '{' to start enum body"},
		{"typedef struct missing member semicolon", "module m; typedef struct { int a } t; endmodule" + next, "unterminated struct/union"},
		{"typedef enum missing closing brace", "module m; typedef enum { A, B t; endmodule" + next, "unterminated enum body"},
		{"stray closing brace", "module m; struct { int a; } } logic ok; endmodule" + next, ""},
		{"truncated file", "module m; struct { int a;", "unterminated struct/union body"},
		{"truncated enum", "module m; enum { A, B", "unterminated enum body"},
	}
	for _, c := range cases {
		f, errs := parseDecls(t, c.src) // must not panic or hang
		if len(errs) == 0 {
			t.Errorf("%s: expected an error, got none", c.name)
		}
		if c.wantErr != "" && !strings.Contains(strings.Join(errs, "\n"), c.wantErr) {
			t.Errorf("%s: errors %v, want one containing %q", c.name, errs, c.wantErr)
		}
		if strings.Contains(c.src, "module n") {
			var mods []string
			for _, d := range f.Decls {
				if m, ok := d.(*ast.Container); ok {
					mods = append(mods, m.Name)
				}
			}
			if strings.Join(mods, ",") != "m,n" {
				t.Errorf("%s: top-level modules = %v, want [m n]: the broken aggregate swallowed what followed", c.name, mods)
			}
		}
	}
}

func TestRecoveryStopsAtAnAggregateKeyword(t *testing.T) {
	// A broken declaration must not eat the aggregate declaration after it.
	f, _ := parseDecls(t, "module m;\n  ) garbage here\n  struct { int a; } s;\nendmodule")
	if v, ok := find[*ast.Variable](f); !ok {
		t.Error("the struct declaration after the garbage was swallowed")
	} else if v.Name != "a" && v.Name != "s" {
		t.Errorf("unexpected first variable %q", v.Name)
	}
	found := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			found[v.Name] = true
		}
		return true
	})
	if !found["s"] {
		t.Errorf("variable s not found: %v", found)
	}
}

func TestTypedefAggregatesAreUnchanged(t *testing.T) {
	// The typedef path now shares its body parser with the inline form; it
	// must still produce a Typedef whose Underlying is positioned on the name
	// and which has no Inline type anywhere.
	f, errs := parseDecls(t, "module m; typedef struct packed { logic a; } s_t; s_t v; endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	td, _ := find[*ast.Typedef](f)
	st, ok := td.Underlying.(*ast.Struct)
	if !ok || !st.Packed {
		t.Fatalf("underlying = %T, want a packed struct", td.Underlying)
	}
	if st.Pos() != td.Pos() {
		t.Errorf("struct position %+v should equal the typedef name position %+v", st.Pos(), td.Pos())
	}
	inline := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if ty, ok := n.(*ast.Type); ok && ty.Inline != nil {
			inline++
		}
		return true
	})
	if inline != 0 {
		t.Errorf("a typedef'd aggregate must not appear as an inline type, found %d", inline)
	}
}
