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
		{"class bodies are not looked into", "class c; int x; function void f(); x = nosuch; endfunction endclass"},
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
