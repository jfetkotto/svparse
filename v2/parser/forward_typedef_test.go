package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
)

func TestForwardTypedefsRecordTheirKind(t *testing.T) {
	// LRM 6.18: "typedef class|struct|union|enum Name;" forward-declares a type
	// defined later; a bare "typedef Name;" does too, with no keyword.
	f, errs := parseDecls(t, `module m;
  typedef class c_t;
  typedef struct s_t;
  typedef union u_t;
  typedef enum e_t;
  typedef bare_t;
  typedef struct { int a; } s_t;
endmodule`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	var got []string
	ast.Inspect(f, func(n ast.Node) bool {
		if td, ok := n.(*ast.Typedef); ok {
			kind := "forward=" + td.ForwardKind
			if td.Underlying != nil {
				kind = "defined"
			}
			got = append(got, td.Name+":"+kind)
		}
		return true
	})
	want := "c_t:forward=class s_t:forward=struct u_t:forward=union e_t:forward=enum bare_t:forward= s_t:defined"
	if g := joinSpace(got); g != want {
		t.Errorf("got  %s\nwant %s", g, want)
	}
}

func joinSpace(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}

func TestForwardTypedefDump(t *testing.T) {
	f, _ := parseDecls(t, "module m; typedef struct s_t; typedef bare_t; endmodule")
	const want = "(file \"t.sv\"\n  (module m\n    (typedef s_t forward=struct)\n    (typedef bare_t forward)))\n"
	if got := ast.Dump(f); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestTypedefAggregateWithBodyIsNotAForwardDeclaration(t *testing.T) {
	// "typedef struct packed {...} s_t;" and "typedef enum logic [1:0] {...} e_t;"
	// have a body; only keyword + name + ';' is forward.
	f, errs := parseDecls(t, "module m; typedef struct packed { logic a; } s_t; typedef enum logic [1:0] { A } e_t; endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if td, ok := n.(*ast.Typedef); ok && (td.Underlying == nil || td.ForwardKind != "") {
			t.Errorf("%s was taken for a forward declaration", td.Name)
		}
		return true
	})
}

func TestMalformedForwardTypedefsRecover(t *testing.T) {
	const next = "\nmodule n; logic fine; endmodule"
	for _, src := range []string{
		"module m; typedef struct; endmodule" + next,
		"module m; typedef enum 5; endmodule" + next,
		"module m; typedef union u endmodule" + next,
	} {
		f, errs := parseDecls(t, src)
		if len(errs) == 0 {
			t.Errorf("%q: expected an error", src)
		}
		found := false
		for _, d := range f.Decls {
			if c, ok := d.(*ast.Container); ok && c.Name == "n" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: the module after the malformed typedef was lost", src)
		}
	}
}

func TestStructMembersMayBeRandQualified(t *testing.T) {
	// LRM 7.2: struct_union_member ::= [random_qualifier] data_type ...
	f, errs := parseDecls(t, "module m; typedef struct { rand bit i; randc integer b[3:0]; int plain; } s_t; endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	got := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			q := "none"
			if v.IsRand {
				q = "rand"
			}
			if v.IsRandC {
				q = "randc"
			}
			got[v.Name] = q
		}
		return true
	})
	if got["i"] != "rand" || got["b"] != "randc" || got["plain"] != "none" {
		t.Errorf("qualifiers = %v, want i=rand b=randc plain=none", got)
	}
}

func TestRandQualifierAppliesToEveryNameInTheMemberStatement(t *testing.T) {
	f, _ := parseDecls(t, "module m; typedef struct { rand int a, b; int c; } s_t; endmodule")
	got := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			got[v.Name] = v.IsRand
		}
		return true
	})
	if !got["a"] || !got["b"] || got["c"] {
		t.Errorf("IsRand = %v, want a and b only", got)
	}
}
