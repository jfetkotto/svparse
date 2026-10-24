package lint

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

// usageOf analyses src and returns, for each named symbol of module top, how it
// is used: "r" read, "w" written, "rw" both, "-" neither.
func usageOf(t *testing.T, src string, withDesign bool) map[string]string {
	t.Helper()
	res := preprocessor.PreprocessDetailed("t.sv", src, nil, preprocessor.Options{})
	file, errs := parser.Parse("t.sv", res.Tokens)
	if len(errs) != 0 {
		t.Fatalf("parse errors: %+v", errs)
	}
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	var design *elab.Design
	if withDesign {
		design, _ = elab.Elaborate(context.Background(), comp, elab.Options{})
	}
	model := comp.Model()
	u := buildUsage(comp, model, design, nil)
	def, ok := comp.Lookup("top")
	if !ok {
		t.Fatal("no top")
	}
	scope, _ := model.DefinitionScope(def)
	out := map[string]string{}
	var visit func(s *sema.Scope)
	visit = func(s *sema.Scope) {
		for _, sym := range s.Symbols() {
			v := ""
			if u.Read(sym) {
				v += "r"
			}
			if u.Written(sym) {
				v += "w"
			}
			if v == "" {
				v = "-"
			}
			out[sym.Name] = v
			if sym.Child != nil && sym.Child.Kind == sema.ScopeGenerate {
				visit(sym.Child)
			}
		}
	}
	visit(scope)
	return out
}

func check(t *testing.T, got map[string]string, want string) {
	t.Helper()
	var bad []string
	for _, kv := range strings.Fields(want) {
		name, use, _ := strings.Cut(kv, "=")
		if got[name] != use {
			bad = append(bad, name+": got "+got[name]+", want "+use)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("usage:\n  %s", strings.Join(bad, "\n  "))
	}
}

func TestUsageOfAssignmentsAndExpressions(t *testing.T) {
	got := usageOf(t, `module top(input clk, input [3:0] din);
  logic [3:0] a, b, c, d, e, f, g, h, i, j, k, idx, sel, untouched;
  logic [3:0] arr [4];
  struct packed { logic [1:0] hi; logic [1:0] lo; } st;
  initial begin
    a = din;            // a written, din read
    b += 1;             // compound: read and written
    c++;                // read and written
    arr[idx] = a;       // arr written, idx read
    st.hi = din[1:0];   // a field write is a write of st
    {d, e} = {din, din};
  end
  assign f = din ^ g;
  always @(posedge clk) h <= f;
  always_comb if (sel[0]) i = 1; else i = 0;
  initial force j = 1;
  logic [3:0] w = din;
endmodule`, true)
	check(t, got, "a=rw b=rw c=rw arr=w idx=r st=w d=w e=w f=rw g=r h=w sel=r i=w j=w w=w din=r clk=r untouched=-")
}

func TestUsageOfCallsAndConnections(t *testing.T) {
	src := `module top;
  logic [7:0] in1, out1, both, plain, into_task, ret, sink, unconnected_use;
  logic ok, wild, w2, imp;
  function automatic int f(input int x, output int y, inout int z); y = x; z = x; return 0; endfunction
  task t(output logic o); o = 1; endtask
  initial begin
    ret = f(in1, out1, both);
    t(into_task);
    void'($value$plusargs("x=%d", plain));
  end
  leaf u(.d(in1), .q(sink), .x(unconnected_use));
  leaf2 v(.*);
  leaf3 z(.imp);
endmodule
module leaf(input [7:0] d, output [7:0] q, inout [7:0] x); endmodule
module leaf2(input wild, output w2); endmodule
module leaf3(input imp); endmodule`
	got := usageOf(t, src, true)
	check(t, got, "in1=r out1=w both=rw into_task=w plain=rw ret=w sink=w unconnected_use=rw wild=rw w2=rw imp=rw")
	// Without a design the directions of connections are unknown, so names in
	// connections count as both.
	got = usageOf(t, src, false)
	check(t, got, "in1=rw sink=rw unconnected_use=rw out1=w both=rw")
}

func TestUsageOfAGenerateBranchNotTaken(t *testing.T) {
	// The branch is not elaborated, but its connections still count as uses.
	got := usageOf(t, `module top #(parameter ON = 0);
  logic only_in_dead_branch, driven;
  if (ON) begin : g
    leaf u(.d(only_in_dead_branch), .q(driven));
  end
endmodule
module leaf(input d, output q); endmodule`, true)
	check(t, got, "only_in_dead_branch=rw driven=rw")
}

func TestUsageOfHierarchicalAndPackageReferences(t *testing.T) {
	got := usageOf(t, `package p; int pv; endpackage
module top;
  sub u();
  logic local_use;
  initial begin
    u.sig = 1;
    local_use = u.other;
    p::pv = 2;
  end
endmodule
module sub; logic sig, other; endmodule`, true)
	check(t, got, "local_use=w u=-")
	// Writes through a hierarchical name reach the other module's signal.
	res := preprocessor.PreprocessDetailed("t.sv", "module top; sub u(); initial u.sig = 1; initial $display(u.other); endmodule\nmodule sub; logic sig, other; endmodule\n", nil, preprocessor.Options{})
	file, _ := parser.Parse("t.sv", res.Tokens)
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	model := comp.Model()
	u := buildUsage(comp, model, nil, nil)
	def, _ := comp.Lookup("sub")
	scope, _ := model.DefinitionScope(def)
	sig, _ := scope.Own("sig")
	other, _ := scope.Own("other")
	if !u.Written(sig) || u.Read(sig) || !u.Read(other) || u.Written(other) {
		t.Errorf("sig: r=%v w=%v; other: r=%v w=%v", u.Read(sig), u.Written(sig), u.Read(other), u.Written(other))
	}
}

func TestUsageOfParametersAndTypes(t *testing.T) {
	got := usageOf(t, `module top #(parameter W = 4, parameter D = W, parameter X = 1, parameter type T = int);
  localparam L = 2;
  localparam M = L + 1;
  logic [D-1:0] v;
  T t;
  initial v = M;
endmodule`, true)
	check(t, got, "W=r D=r X=- L=r M=r")
}

func TestUsageToleratesBrokenSource(t *testing.T) {
	// Half-typed code: the analysis must not panic.
	for _, src := range []string{
		"module top; assign = ; leaf u(.; initial begin a[ = b; end endmodule",
		"module top; always @(posedge) x <= ; foreach (a[) ; endmodule",
		"class C; endclass module top; C::; u.; $; endmodule",
	} {
		res := preprocessor.PreprocessDetailed("t.sv", src, nil, preprocessor.Options{})
		file, _ := parser.Parse("t.sv", res.Tokens)
		comp := sema.NewCompilation(sema.Options{})
		comp.AddFile(file)
		design, _ := elab.Elaborate(context.Background(), comp, elab.Options{})
		buildUsage(comp, comp.Model(), design, nil)
	}
}
