package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

const netSource = `module m;
  nettype real real_net;
  nettype real resolved_net with real_sum;
  nettype pkg::net_t alias_net with pkg::resolve;
  interconnect bus;
  interconnect [3:0] vec;
  specparam delay = 50, other = 2;
  tri1 scalared [15:0] a = 0;
  tri1 vectored [7:0] b;
  wire #10 w;
  wire (strong0, pull1) [3:0] s;
  wire logic [7:0] #(1, 2:3:4, 5) d1, d2;
  trireg (small) cap;
  var type(a + b) c;
  var type(a) e, f;
endmodule`

// TestNettypeInterconnectSpecparamAndNetDecorations pins the tree for the
// declaration forms this item adds: user-defined net types (alias and
// resolution-function forms), interconnect nets, specparams, the
// vectored/scalared qualifiers, drive and charge strengths, delays (single,
// list, and min:typ:max), and the type reference operator as a data type.
func TestNettypeInterconnectSpecparamAndNetDecorations(t *testing.T) {
	f, errs := parseDecls(t, netSource)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	const want = `(file "t.sv"
  (module m
    (nettype real_net <real>)
    (nettype resolved_net <real> with=real_sum)
    (nettype alias_net <pkg::net_t> with=pkg::resolve)
    (variable bus <interconnect>)
    (variable vec <interconnect [3:0]>)
    (specparam delay = 50)
    (specparam other = 2)
    (variable a <tri1 [15:0]> scalared = 0)
    (variable b <tri1 [7:0]> vectored)
    (variable w <wire> delay=[10])
    (variable s <wire [3:0]> strength=(strong0,pull1))
    (variable d1 <logic [7:0]> delay=[1 (mintypmax 2 3 4) 5])
    (variable d2 <logic [7:0]> delay=[1 (mintypmax 2 3 4) 5])
    (variable cap <trireg> strength=(small))
    (variable c <(typeof (+ a b))>)
    (variable e <(typeof (type-or-expr a))>)
    (variable f <(typeof (type-or-expr a))>)))
`
	if got := ast.Dump(f); got != want {
		t.Errorf("dump mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func variablesByName(f *ast.File) map[string]*ast.Variable {
	out := map[string]*ast.Variable{}
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok {
			out[v.Name] = v
		}
		return true
	})
	return out
}

func TestNetDecorationFields(t *testing.T) {
	f, _ := parseDecls(t, netSource)
	v := variablesByName(f)
	if v["a"].NetQualifier != "scalared" || v["b"].NetQualifier != "vectored" || v["w"].NetQualifier != "" {
		t.Errorf("qualifiers: a=%q b=%q w=%q", v["a"].NetQualifier, v["b"].NetQualifier, v["w"].NetQualifier)
	}
	if got := strings.Join(v["s"].DriveStrength, ","); got != "strong0,pull1" {
		t.Errorf("s strength = %q", got)
	}
	if got := strings.Join(v["cap"].DriveStrength, ","); got != "small" {
		t.Errorf("charge strength = %q", got)
	}
	if len(v["a"].Type.PackedDims) != 1 {
		t.Errorf("dimensions after the qualifier were lost: %+v", v["a"].Type.PackedDims)
	}
	if len(v["d1"].Delay) != 3 || len(v["w"].Delay) != 1 || len(v["a"].Delay) != 0 {
		t.Errorf("delays: d1=%d w=%d a=%d", len(v["d1"].Delay), len(v["w"].Delay), len(v["a"].Delay))
	}
	if v["c"].Type.TypeRef == nil || v["c"].Type.Name != "type" {
		t.Errorf("type(a + b) = %+v", v["c"].Type)
	}
}

func TestDelayValueForms(t *testing.T) {
	// Each is a legal delay3 (LRM 28.16). A plain "#10 w" must stop before the
	// name: the delay is a primary, not a full expression.
	cases := map[string]string{
		"wire #10 w;":              "10",
		"wire #1.5 w;":             "1.5",
		"wire #10ns w;":            "10ns",
		"wire #d w;":               "d",
		"wire #(7) w;":             "7",
		"wire #(1, 2) w;":          "1 2",
		"wire #(1, 2, 3) w;":       "1 2 3",
		"wire #(a + 1, b) w;":      "(+ a 1) b",
		"wire #(1:2:3) w;":         "(mintypmax 1 2 3)",
		"wire [3:0] #5 w;":         "5",
		"wire logic [3:0] #(5) w;": "5",
	}
	for src, want := range cases {
		f, errs := parseDecls(t, "module m; "+src+" endmodule")
		if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %v", src, errs)
			continue
		}
		w := variablesByName(f)["w"]
		if w == nil {
			t.Errorf("%q: variable w not found", src)
			continue
		}
		got := strings.Join(exprTexts(w.Delay), " ")
		if got != want {
			t.Errorf("%q: delay = %q, want %q", src, got, want)
		}
	}
}

func exprTexts(es []ast.Expr) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = strings.TrimSpace(ast.Dump(e))
	}
	return out
}

func TestNetSuffixOrders(t *testing.T) {
	// Strength, qualifier and dimensions in the orders LRM 6.7 allows.
	for _, src := range []string{
		"tri1 scalared [15:0] a;",
		"tri1 (strong0, weak1) scalared [15:0] a;",
		"tri1 (strong0, weak1) [15:0] a;",
		"tri1 [15:0] a;",
		"tri1 vectored a;",
		"wire (weak0, weak1) #3 a;",
	} {
		_, errs := parseDecls(t, "module m; "+src+" endmodule")
		if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %v", src, errs)
		}
	}
}

func TestStrengthAndDelayAreOnlyForNets(t *testing.T) {
	// A delay or strength after a non-net type is not legal SystemVerilog, so
	// it must stay an error rather than be silently accepted.
	for _, src := range []string{
		"logic #5 x;",
		"int (strong0, weak1) x;",
		"my_type #(5) x, y;", // this one is a parameterized class type, which is fine
	} {
		_, errs := parseDecls(t, "module m; "+src+" endmodule")
		isClassType := strings.HasPrefix(src, "my_type")
		if isClassType && len(errs) != 0 {
			t.Errorf("%q: a parameterized class type should parse: %v", src, errs)
		}
		if !isClassType && len(errs) == 0 {
			t.Errorf("%q: accepted a net-only decoration on a non-net type", src)
		}
	}
}

func TestSpecparamForms(t *testing.T) {
	f, errs := parseDecls(t, "module m; specparam a = 1; specparam [7:0] b = 2, c = 3; parameter p = 4; endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	got := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if p, ok := n.(*ast.Parameter); ok {
			got[p.Name] = p.IsSpecparam
		}
		return true
	})
	if !got["a"] || !got["b"] || !got["c"] || got["p"] {
		t.Errorf("IsSpecparam = %v, want a b c true and p false", got)
	}
}

func TestNettypeDeclaration(t *testing.T) {
	f, errs := parseDecls(t, "module m; nettype pkg::real_t real_net with pkg::sum; endmodule")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	nt, ok := find[*ast.Nettype](f)
	if !ok {
		t.Fatal("no nettype")
	}
	if nt.Name != "real_net" || nt.Type.PackageQualifier != "pkg" || nt.Type.Name != "real_t" || nt.ResolveFunc != "pkg::sum" {
		t.Errorf("nettype = %+v", nt)
	}
}

func TestDeclaratorsOwnTheirDelayAndTypeReference(t *testing.T) {
	// "wire #5 a, b;" and "var type(x) a, b;" give two Variables one delay or
	// one type reference; each must own its copy so Walk visits every node once.
	f, _ := parseDecls(t, "module m; wire #(1, 2) a, b; var type(x) c, d; endmodule")
	v := variablesByName(f)
	if len(v["a"].Delay) != 2 || len(v["b"].Delay) != 2 || v["a"].Delay[0] == v["b"].Delay[0] {
		t.Error("the declarators share delay expression nodes")
	}
	if v["c"].Type.TypeRef == v["d"].Type.TypeRef {
		t.Error("the declarators share one type reference node")
	}
	if ast.Dump(v["c"].Type.TypeRef) != ast.Dump(v["d"].Type.TypeRef) {
		t.Error("but the copies must be equal in content")
	}
	counts := map[ast.Node]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		if n != nil {
			counts[n]++
		}
		return true
	})
	for n, c := range counts {
		if c > 1 {
			t.Errorf("%T visited %d times", n, c)
		}
	}
}

func TestMalformedNetDeclarationsRecover(t *testing.T) {
	const next = "\nmodule n; logic fine; endmodule"
	for _, src := range []string{
		"module m; wire #( ; endmodule" + next,
		"module m; wire #(1, ; endmodule" + next,
		"module m; wire (strong0 ; endmodule" + next,
		"module m; wire (strong0, ) w; endmodule" + next,
		"module m; var type( ; endmodule" + next,
		"module m; var type(a ; endmodule" + next,
		"module m; nettype ; endmodule" + next,
		"module m; nettype real ; endmodule" + next,
		"module m; nettype real n with ; endmodule" + next,
		"module m; nettype real n with pkg:: ; endmodule" + next,
		"module m; specparam ; endmodule" + next,
		"module m; interconnect ; endmodule" + next,
	} {
		f, errs := parseDecls(t, src) // must not panic or hang
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
			t.Errorf("%q: the module after the malformed declaration was lost", src)
		}
	}
}
