package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
)

const classParamSource = `package pkg; endpackage
class base_cls #(int b = 20); endclass
class ext_cls #(int e = 25) extends base_cls #(5); endclass
class Foo extends pkg::Bar #(x, .y(2)) (1, a + b) implements pkg::I, J #(3); endclass
interface class ic3 #(type TYPE = logic) extends ic1#(TYPE), ic2#(TYPE); endclass
module m (input cls #(8) p, output cls #(8)::inner_t q);
  cls #(8)::inner_t v;
  pkg::cls #(2)::T w;
  struct { cls #(8) member; } s;
  function cls #(4) f(cls #(8) a, cls #(9)::U b); endfunction
endmodule`

// TestParameterizedClassReferences pins the tree for a parameterized class in
// an extends clause (with a package scope, named and positional values, and
// constructor arguments), an implements list, an interface class extending
// several interfaces, and a parameterized class used as a type in every
// position, including as the scope of a nested type.
func TestParameterizedClassReferences(t *testing.T) {
	f, errs := parseDecls(t, classParamSource)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	const want = `(file "t.sv"
  (package pkg)
  (class base_cls
    (parameter b <int> = 20))
  (class ext_cls extends=base_cls #(5)
    (parameter e <int> = 25))
  (class Foo extends=pkg::Bar #((type-or-expr x), .y 2) extends-args=[1 (+ a b)] implements=pkg::I, J #(3))
  (class ic3 interface extends=ic1 #((type-or-expr TYPE)), ic2 #((type-or-expr TYPE))
    (parameter TYPE <type> = <logic>))
  (module m
    (port input p <cls #(8)>)
    (port output q <cls#(8)::inner_t>)
    (variable v <cls#(8)::inner_t>)
    (variable w <pkg::cls#(2)::T>)
    (variable s <struct>
      (struct
        (variable member <cls #(8)>)))
    (function f <cls #(4)>
      (arg a <cls #(8)>)
      (arg b <cls#(9)::U>))))
`
	if got := ast.Dump(f); got != want {
		t.Errorf("dump mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestClassLegacyNameFieldsStayPopulated: sigils reads Extends and Implements
// as bare names, so they must keep meaning that however rich the new fields get.
func TestClassLegacyNameFieldsStayPopulated(t *testing.T) {
	f, _ := parseDecls(t, classParamSource)
	classes := map[string]*ast.Class{}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.Class); ok {
			classes[c.Name] = c
		}
		return true
	})
	if got := classes["ext_cls"].Extends; got != "base_cls" {
		t.Errorf("ext_cls.Extends = %q, want the bare name base_cls", got)
	}
	foo := classes["Foo"]
	if foo.Extends != "Bar" {
		t.Errorf("Foo.Extends = %q, want Bar (qualifier and parameters dropped)", foo.Extends)
	}
	if strings.Join(foo.Implements, ",") != "I,J" {
		t.Errorf("Foo.Implements = %v, want [I J]", foo.Implements)
	}
	if got := foo.ExtendsTypes[0].PackageQualifier; got != "pkg" {
		t.Errorf("the full reference keeps its qualifier, got %q", got)
	}
	ic3 := classes["ic3"]
	if ic3.Extends != "ic1" || len(ic3.ExtendsTypes) != 2 || ic3.ExtendsTypes[1].Name != "ic2" {
		t.Errorf("ic3 extends %q with %d bases, want ic1 first of 2", ic3.Extends, len(ic3.ExtendsTypes))
	}
}

func TestParameterizedClassValuesAreParsed(t *testing.T) {
	// The values inside "#( ... )" are expressions, parsed by the fill pass
	// like every other override, with the raw tokens kept beside them.
	f, _ := parseDecls(t, "class c extends base #(5 + 1, .W(8)); endclass")
	c, _ := find[*ast.Class](f)
	ov := c.ExtendsTypes[0].ParamOverrides
	if len(ov) != 2 {
		t.Fatalf("got %d overrides", len(ov))
	}
	if got := strings.TrimSpace(ast.Dump(ov[0].ValueExpr)); got != "(+ 5 1)" {
		t.Errorf("first value = %s, want (+ 5 1)", got)
	}
	if ov[1].Name != "W" || len(ov[1].Value) != 1 {
		t.Errorf("second override = %+v: raw value should be kept", ov[1])
	}
}

func TestClassScopedTypeSeparatesQualifierFromOwnOverrides(t *testing.T) {
	f, errs := parseDecls(t, "module m; cls #(8)::inner_t v; cls #(3) w; endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var v, w *ast.Variable
	ast.Inspect(f, func(n ast.Node) bool {
		if x, ok := n.(*ast.Variable); ok {
			switch x.Name {
			case "v":
				v = x
			case "w":
				w = x
			}
		}
		return true
	})
	if v.Type.PackageQualifier != "cls" || v.Type.Name != "inner_t" ||
		len(v.Type.QualifierOverrides) != 1 || len(v.Type.ParamOverrides) != 0 {
		t.Errorf("cls#(8)::inner_t = %+v: want qualifier cls with the overrides on the qualifier only", v.Type)
	}
	if w.Type.Name != "cls" || len(w.Type.ParamOverrides) != 1 || len(w.Type.QualifierOverrides) != 0 {
		t.Errorf("cls #(3) = %+v: want the overrides on the type itself", w.Type)
	}
}

func TestScopedTypeDeclaratorsOwnTheirOverrides(t *testing.T) {
	f, _ := parseDecls(t, "module m; cls #(8)::T a, b; endmodule")
	var vars []*ast.Variable
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			vars = append(vars, v)
		}
		return true
	})
	a, b := vars[0].Type.QualifierOverrides, vars[1].Type.QualifierOverrides
	if len(a) != 1 || len(b) != 1 || &a[0] == &b[0] || a[0].ValueExpr == b[0].ValueExpr {
		t.Errorf("the declarators share qualifier overrides")
	}
}

func TestNetDelayIsNotMistakenForClassParameters(t *testing.T) {
	// "wire #(5) w;" is a net with a delay (not supported yet), not a
	// parameterized class: a keyword type never takes a parameter list, so the
	// delay must not be recorded as ParamOverrides.
	f, _ := parseDecls(t, "module m; wire #(5) w; endmodule")
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok && len(v.Type.ParamOverrides) != 0 {
			t.Errorf("net delay recorded as class parameters: %+v", v.Type.ParamOverrides)
		}
		return true
	})
}

func TestUnclosedParameterListLeavesTheSemicolon(t *testing.T) {
	// The ';' that ends the header is not part of the broken list: it must be
	// left for the header, or the error would be reported twice (once for the
	// list, once for a "missing" semicolon that was in fact swallowed).
	_, errs := parseDecls(t, "class c extends base #(5; endclass")
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "unterminated parameter override list") {
		t.Errorf("errors %q: want the unterminated list reported", joined)
	}
	if strings.Contains(joined, "expected ';' after class header") {
		t.Errorf("errors %q: the header's ';' was swallowed by the list", joined)
	}
	if len(errs) != 1 {
		t.Errorf("want exactly one error, got %d: %v", len(errs), errs)
	}
}

func TestMalformedClassHeadersRecover(t *testing.T) {
	const next = "\nclass after; endclass"
	for _, src := range []string{
		"class c extends ; endclass" + next,
		"class c extends base #( ; endclass" + next,
		"class c extends pkg:: ; endclass" + next,
		"class c extends a, ; endclass" + next,
		"class c implements ; endclass" + next,
		"class c implements #(3); endclass" + next,
		"class c extends base #(5) (1, ; endclass" + next,
	} {
		f, errs := parseDecls(t, src) // must not panic or hang
		if len(errs) == 0 {
			t.Errorf("%q: expected an error", src)
		}
		found := false
		for _, d := range f.Decls {
			if c, ok := d.(*ast.Class); ok && c.Name == "after" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: the class after the broken header was lost", src)
		}
	}
}
