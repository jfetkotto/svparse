package sema

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
)

func checkNames(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	toks, _ := preprocessor.Preprocess("n.sv", src, nil)
	file, errs := parser.Parse("n.sv", toks)
	if len(errs) != 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	c := NewCompilation(Options{})
	c.AddFile(file)
	return c.Model().CheckNames()
}

func TestCheckNamesReportsUndeclaredNames(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"continuous rhs", "module m(output b); assign b = nosuch; endmodule", "nosuch"},
		{"procedural rhs", "module m; logic [3:0] x; always_comb x = nosuch + 1; endmodule", "nosuch"},
		{"condition", "module m; logic x; always_comb if (nosuch) x = 1; else x = 0; endmodule", "nosuch"},
		{"index", "module m; logic [3:0] x; logic y; assign y = x[nosuch]; endmodule", "nosuch"},
		{"range", "module m; logic [nosuch:0] x; endmodule", "nosuch"},
		{"initialiser", "module m; logic x = nosuch; endmodule", "nosuch"},
		{"port connection expression", "module m(input a); leaf u(.p(a & nosuch)); endmodule module leaf(input p); endmodule", "nosuch"},
		{"parameter override", "module m; leaf #(.W(nosuch)) u(); endmodule module leaf #(parameter W = 1)(); endmodule", "nosuch"},
		{"function call", "module m; int x; initial x = nosuch(1); endmodule", "nosuch"},
		{"in a function", "module m; function int f(int a); return a + nosuch; endfunction endmodule", "nosuch"},
		{"generate condition", "module m; if (nosuch) begin : g end endmodule", "nosuch"},
		{"package", "package p; localparam int X = nosuch; endpackage", "nosuch"},
		{"after a use before declaration of a different name", "module m(output b); assign b = later; logic other; endmodule", "later"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := checkNames(t, tc.src)
			if len(ds) != 1 || ds[0].Code != diag.CodeUndeclared || !strings.Contains(ds[0].Message, tc.want) {
				t.Fatalf("got %v", ds)
			}
		})
	}
}

func TestCheckNamesAcceptsDeclaredAndImplicitNames(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"ports and locals", "module m(input [3:0] a, output logic [3:0] b); logic [3:0] t; always_comb begin t = a; b = t + 1; end endmodule"},
		{"parameters in ranges", "module m #(parameter W = 4)(input [W-1:0] a); logic [W:0] x; assign x = a; endmodule"},
		{"package import", "package p; typedef logic [3:0] n_t; localparam int K = 2; endpackage module m; import p::*; n_t x; assign x = K; endmodule"},
		{"scoped name", "package p; localparam int K = 2; endpackage module m; logic [3:0] x; assign x = p::K; endmodule"},
		{"enum member", "module m; typedef enum {A, B} e_t; e_t s; always_comb s = B; endmodule"},
		{"genvar", "module m; logic [3:0] x; for (genvar i = 0; i < 4; i++) begin : g assign x[i] = 1'b0; end endmodule"},
		{"loop variable", "module m; logic [3:0] x; always_comb for (int i = 0; i < 4; i++) x[i] = 1'b0; endmodule"},
		{"foreach variable", "module m; logic [3:0] a [4]; int s; always_comb foreach (a[i]) s = i; endmodule"},
		{"function argument", "module m; function int f(int a); return a; endfunction int x; initial x = f(1); endmodule"},
		{"system functions", "module m; int x; initial x = $clog2(8) + $bits(x); endmodule"},
		{"implicit net from assignment target", "module m(input a); assign y = a; assign z = y; endmodule"},
		{"implicit net from connection, used later", "module m(input a, input b); wire d; leaf u(a, b, c); assign d = c; endmodule module leaf(input a, input b, output c); endmodule"},
		{"implicit net from named connection", "module m(input a); leaf u(.p(a), .q(w)); assign a2 = w; endmodule module leaf(input p, output q); endmodule"},
		{"gate terminal", "module m(input a); and g (o, a, a); assign y = o; endmodule"},
		{"escaped identifier", "module m; reg \\cpu3 ; wire w = cpu3; endmodule"},
		{"module name as hierarchical root", "module top; initial $display(top); endmodule"},
		{"std classes", "module m; process p; initial begin p = process::self(); if (p.status == process::FINISHED) p.kill(); end endmodule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ds := checkNames(t, tc.src); len(ds) != 0 {
				t.Fatalf("got %v", ds)
			}
		})
	}
}

func FuzzCheckNames(f *testing.F) {
	f.Add("module m(input a); logic v; assign v = a & nosuch; leaf u(.p(zz)); endmodule")
	f.Add("package p; localparam int X = p::Y; endpackage module m; import p::*; initial $display(X, top); endmodule")
	f.Add("module m; for (genvar i = 0; i < 2; i++) begin : g assign y[i] = i; end endmodule")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		file, _ := parser.Parse("f.sv", toks)
		c := NewCompilation(Options{})
		c.AddFile(file)
		for _, d := range c.Model().CheckNames() {
			if d.Code != diag.CodeUndeclared {
				t.Fatalf("CheckNames reported %q", d.Code)
			}
		}
	})
}

func TestCheckNamesInClasses(t *testing.T) {
	accept := map[string]string{
		"members and methods": "class c; int x; static int n; function new(int v = 0); x = v; endfunction function void f(); x = n + this.x; endfunction endclass",
		"inherited members":   "class b; int v; protected int p; endclass class d extends b; function void f(); v = p; super.f2(); endfunction endclass",
		"parameterised base":  "class b #(type T = int); T v; endclass class d extends b#(int); function void f(); v = 1; endfunction endclass",
		"unknown base":        "class d extends not_here; function void f(); inherited_thing = 1; endfunction endclass",
		"type parameter base": "class d #(type B = int) extends B; function void f(); from_base = 1; endfunction endclass",
		"built-in methods":    "class c; rand int r; function void f(); int ok; ok = randomize(); r.rand_mode(0); srandom(1); pre_randomize(); endfunction endclass",
		"local scope":         "class c; int s; function int f(); return local::s; endfunction endclass",
		"extern method":       "class c; int n; extern function int e(int a); endclass function int c::e(int a); return a + n; endfunction",
		"nested types":        "class c; typedef enum {X, Y} en_t; localparam int LP = 3; en_t en = Y; class in; int iv; endclass function void f(int d = LP); in i; i = new; if (en == X) i.iv = d; endfunction endclass",
		"forward class":       "typedef class later; class b; later l; function void f(); l.run(); endfunction endclass class later; function void run(); endfunction endclass",
		"package members":     "package p; int pk; endpackage class c; import p::*; function void f(); pk = 1; endfunction endclass",
		"foreach and blocks":  "class c; int q[$]; function void f(); foreach (q[i]) begin int z; z = i; end endfunction endclass",
		"constraints":         "class c; rand int r; rand int a[4]; int lim; constraint k { r inside {[0:lim]}; r dist {1 := 2}; soft r > 1; unique {a}; foreach (a[i]) a[i] < lim + i; if (r > 1) { r < 10; } else r == 0; solve r before lim; } endclass",
		"external constraint": "class c; rand int r; int lim; constraint k; endclass constraint c::k { r < lim; foreach (r[i, j]) r > i; }",
		"constraint in a class with an unknown base": "class c extends missing; constraint k { x < from_base; } endclass",
	}
	for name, src := range accept {
		t.Run(name, func(t *testing.T) {
			if ds := checkNames(t, src); len(ds) != 0 {
				t.Fatalf("got %v", ds)
			}
		})
	}
	reject := map[string]string{
		"undeclared in a method":         "class c; int x; function void f(); x = nosuch; endfunction endclass",
		"undeclared call":                "class c; function void f(); nosuch_fn(); endfunction endclass",
		"undeclared in a member initial": "class c; int x = nosuch; endclass",
		"undeclared in a task":           "class c; task t(); #1 nosuch = 1; endtask endclass",
		"a member of a later class":      "class a; int x; endclass class b; function void f(); x = 1; endfunction endclass",
		"undeclared in a constraint":     "class c; rand int r; constraint k { r < nosuch; } endclass",
		"undeclared in an external one":  "class c; rand int r; constraint k; endclass constraint c::k { r < nosuch; }",
		"loop variable does not leak":    "class c; rand int a[4]; constraint k { foreach (a[i]) a[i] > 0; i > 1; } endclass",
		"in a resolvable base":           "class b; int v; endclass class d extends b; function void f(); v = 1; zz = 2; endfunction endclass",
	}
	for name, src := range reject {
		t.Run(name, func(t *testing.T) {
			if ds := checkNames(t, src); len(ds) == 0 {
				t.Fatal("expected an undeclared name")
			}
		})
	}
}

func TestCheckNamesInAssertions(t *testing.T) {
	accept := map[string]string{
		"sequence with arguments and a local": `module m(input clk, input a, input b, input [7:0] d); logic [7:0] q;
  sequence s1(x); @(posedge clk) x ##1 b; endsequence
  sequence s2; int v; (a, v = d) ##2 (q == v); endsequence
  property p1(logic y, int n = 2); @(posedge clk) y |-> ##n b; endproperty
  property p2; @(posedge clk) s1(a) |=> $rose(b) and (a throughout b[*2]); endproperty
  property p3; @(posedge clk) first_match(a ##[1:3] b) |-> s2; endproperty
  property p4; @(posedge clk) if (a) b else a; endproperty
endmodule`,
		"asserts": `module m(input clk, input rst, input a, input b);
  property p1; @(posedge clk) disable iff (rst) a |-> b; endproperty
  assert property (p1) else $error("x");
  assert property (@(posedge clk) a |-> ##1 b);
  cover property (@(posedge clk) a ##1 b);
  c1: cover property (p1);
  assume property (@(posedge clk) a != b);
  always @(posedge clk) assert (a || b);
endmodule`,
	}
	for name, src := range accept {
		t.Run(name, func(t *testing.T) {
			if ds := checkNames(t, src); len(ds) != 0 {
				t.Fatalf("got %v", ds)
			}
		})
	}
	reject := map[string]string{
		"in a property":          "module m(input clk, input a); property p; @(posedge clk) if (a) a else nosuch; endproperty endmodule",
		"in a property argument": "module m(input clk, input a); property p(z); @(posedge clk) z |-> nosuch; endproperty endmodule",
		"in an assertion":        "module m(input clk, input a); assert property (@(posedge clk) a |-> nosuch); endmodule",
		"in disable iff":         "module m(input clk, input a); assert property (@(posedge clk) disable iff (nosuch) a); endmodule",
		"formal does not leak":   "module m(input clk, input a); property p(z); @(posedge clk) z |-> a; endproperty assert property (@(posedge clk) z); endmodule",
	}
	for name, src := range reject {
		t.Run(name, func(t *testing.T) {
			if ds := checkNames(t, src); len(ds) == 0 {
				t.Fatal("expected an undeclared name")
			}
		})
	}
}

func TestCheckNamesInCoverageAndClocking(t *testing.T) {
	accept := map[string]string{
		"covergroup": `module m(input clk, input [3:0] a, input [3:0] b, input en); logic [3:0] data; int lim;
  covergroup cg (int w, ref logic [3:0] v) @(posedge clk);
    option.per_instance = 1;
    cp_a: coverpoint a iff (en) { bins lo = {[0:3]}; bins hi[] = {[4:w]}; bins few = a with (item < lim); ignore_bins ig = {15}; }
    cp_b: coverpoint b { bins t = (0 => 1 => 2); bins d = default; }
    cp_v: coverpoint v;
    x_ab: cross cp_a, cp_b { bins both = binsof(cp_a.lo) && binsof(cp_b.t); ignore_bins g = binsof(cp_a) intersect {1}; }
    cross a, data;
  endgroup
  covergroup sg with function sample(int s); coverpoint s; endgroup
endmodule`,
		"covergroup in a class": "class c; int x; bit clk; covergroup cg @(posedge clk); coverpoint x; endgroup endclass",
		"clocking block":        "module m(input clk, input a, output b); clocking cb @(posedge clk); default input #1 output #2; input a; output b; endclocking endmodule",
	}
	for name, src := range accept {
		t.Run(name, func(t *testing.T) {
			if ds := checkNames(t, src); len(ds) != 0 {
				t.Fatalf("got %v", ds)
			}
		})
	}
	reject := map[string]string{
		"coverpoint":      "module m(input clk, input a); covergroup cg @(posedge clk); coverpoint nosuch; endgroup endmodule",
		"bin range":       "module m(input clk, input [3:0] a); covergroup cg @(posedge clk); coverpoint a { bins b = {[0:nosuch]}; } endgroup endmodule",
		"cross":           "module m(input clk, input a); covergroup cg @(posedge clk); coverpoint a; cross a, nosuch; endgroup endmodule",
		"label not leaks": "module m(input clk, input a); covergroup cg @(posedge clk); lbl: coverpoint a; endgroup wire w = lbl; endmodule",
		"clocking signal": "module m(input clk); clocking cb @(posedge clk); input nosuch; endclocking endmodule",
		"clocking event":  "module m; clocking cb @(posedge nosuch); endclocking endmodule",
	}
	for name, src := range reject {
		t.Run(name, func(t *testing.T) {
			if ds := checkNames(t, src); len(ds) == 0 {
				t.Fatal("expected an undeclared name")
			}
		})
	}
}
