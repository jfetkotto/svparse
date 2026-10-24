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
