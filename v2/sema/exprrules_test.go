package sema

import (
	"testing"

	"github.com/jfetkotto/svparse/v2/diag"
)

func checkAll(t *testing.T, src string) string {
	t.Helper()
	c := NewCompilation(Options{})
	c.AddFile(parseFile(t, "t.sv", src))
	m := c.Model()
	return codes(append(m.CheckNames(), m.CheckStatements()...))
}

func TestExpressionAndTypeRules(t *testing.T) {
	const ie, is, un, it = diag.CodeInvalidExpression, diag.CodeInvalidStatement, diag.CodeUndeclared, diag.CodeInvalidType
	cases := []struct {
		name, src, want string
	}{
		{"valid", `module m; real r; wire [3:0] w; logic [7:0] v; wire x; bit [31:0] d;
  specparam sp = 1; parameter p = 2;
  typedef int myint; typedef myint other; typedef later_t; typedef struct packed {bit a; bit b;} later_t;
  assign x = w[1] & v[2]; logic y;
  initial begin r = 1.5; d = {<<{v}}; @(posedge x); end
  function void f(); fork $display(); join_none endfunction
  task t(); fork #1; join_none return; endtask
endmodule`, ""},
		{"real select", "module m; real a; wire b; assign b = a[2]; endmodule", ie},
		{"real part select", "module m; real a; wire [1:0] b; assign b = a[1:0]; endmodule", ie},
		{"real index", "module m; real a; wire [3:0] b; wire c; assign c = b[a]; endmodule", ie},
		{"real edge", "module m; real a; always @(posedge a) $display(); endmodule", ie},
		{"vectored", "module m; logic vectored [15:0] a = 0; assign a[1] = 1; endmodule", ie},
		{"specparam in parameter", "module m; specparam d = 50; parameter p = d + 2; endmodule", ie},
		{"undefined alias type", "typedef some_other myalias; module m; endmodule", un},
		{"undefined variable type", "module m; nope_t x; endmodule", un},
		{"forward typedef never defined", "typedef missing; module m; endmodule", un},
		{"packed struct default", "module m; struct packed { bit [3:0] lo = 5; bit [3:0] hi; } p; endmodule", it},
		{"fork join in function", "module m; function int f(int v); fork $display(); $display(); join_any return v; endfunction endmodule", is},
		{"return from fork", "module m; task t; fork #20; return; join_none endtask endmodule", is},
		{"stream too wide", "module m; int a, b; initial begin int d = {<<{a, b}}; end endmodule", ie},
		{"stream fits", "module m; int a, b; initial begin longint d = {<<{a, b}}; end endmodule", ""},
	}
	for _, c := range cases {
		if got := checkAll(t, c.src); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRandSequenceRules(t *testing.T) {
	const un = diag.CodeUndeclared
	cases := []struct {
		name, src, want string
	}{
		{"valid", `function int F(); int x; static int on = 1;
  randsequence (main)
    main : first second | third := 2 { x = 1; } | rand join first third;
    first : { int y; y = 2; x = y; if (on) break; return; } ;
    void second(int a) : if (x > a) first else third | repeat (3) first | case (x) 0, 1 : first; default : third; endcase;
    int third : { x = x + first; return 5; };
  endsequence
  return x; endfunction`, ""},
		{"undeclared switch variable", "function int F(); int x; randsequence (m) m : a; a : { if (nope) x = 1; }; endsequence return x; endfunction", un},
		{"undeclared in a production condition", "function int F(); randsequence (m) m : if (nope) a else b; a : { }; b : { }; endsequence return 1; endfunction", un},
		{"misspelled production", "function int F(); int x; randsequence (m) m : a bb; a : { x = 1; }; b : { x = 2; }; endsequence return x; endfunction", un},
		{"unknown start production", "function int F(); randsequence (zz) m : { }; endsequence return 1; endfunction", un},
		{"a local does not leak", "function int F(); randsequence (m) m : { int y; y = 1; }; endsequence return y; endfunction", un},
	}
	for _, c := range cases {
		if got := checkAll(t, c.src); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPatternInitialisersAndClassScope(t *testing.T) {
	const ie, it = diag.CodeInvalidExpression, diag.CodeIncompatibleTypes
	cases := []struct {
		name, src, want string
	}{
		{"valid patterns", `module m; typedef struct {int a; int b;} s_t; typedef int triple [1:3];
  s_t one = '{1, 2}; s_t two = '{a: 1, b: 2}; s_t d = '{default: 0};
  s_t arr [1:0] = '{'{0, 0}, '{1, 1}}; triple k = '{1: 1, default: 0};
  string words [int] = '{default: "hello"}; int q [$] = '{1, 2, 3}; int rep [4] = '{4{0}};
  initial begin s_t loc = '{3, 4}; end endmodule`, ""},
		{"flat values for a struct array", "module m; typedef struct {int a; int b;} s_t; s_t ms [1:0] = '{0, 0, 1, 1}; endmodule", it},
		{"too many for an array", "module m; int a [2] = '{1, 2, 3}; endmodule", it},
		{"unknown member", "module m; typedef struct {int a;} s_t; s_t v = '{zz: 1}; endmodule", it},
		{"local too", "module m; initial begin int a [2] = '{1, 2, 3}; end endmodule", it},
		{"unspecialised class scope", "module m; class c #(int a = 1); parameter int b = 2; endclass initial $display(c::b); endmodule", ie},
		{"specialised class scope", "module m; class c #(int a = 1); parameter int b = 2; endclass initial $display(c#()::b); endmodule", ""},
		{"plain class scope", "module m; class c; parameter int b = 2; endclass initial $display(c::b); endmodule", ""},
		{"inside the class", "module m; class c #(int a = 1); parameter int b = 2; function int f(); return c::b; endfunction endclass class d extends c; function int g(); return c::b; endfunction endclass endmodule", ""},
	}
	for _, c := range cases {
		if got := checkAll(t, c.src); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
