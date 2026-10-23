package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

const miscSource = `package p; endpackage
module m;
  timeunit 1ns / 1ps;
  timeprecision 10ps;
  defparam u1.W = 8, u2.sub[1].W = 4;
  bind top.u1 : a.b, c checker_t chk (.clk(clk));
  bind m other_t o (.x(x));
  let max(a, b = 1) = a > b ? a : b;
  alias w1 = w2 = w3;
  export p::f;
  export *::*;
  export "DPI-C" c_f = function f;
  export "DPI-C" task t;
  default disable iff rst;
  default clocking cb;
  default clocking cb2 @(posedge clk);
    default input #1step output negedge #2;
    input a, b;
    output #3 c = top.c;
    input output d;
    inout e;
    property pp; a; endproperty
  endclocking
  global clocking g @(posedge clk); endclocking
  clocking @(negedge clk); endclocking
endmodule
config cfg;
  localparam P = 1;
  design lib.top other.cell;
  default liblist lib1 lib2;
  instance top.u1 use lib.cell2;
  cell lib.c use x;
endconfig
`

const miscDump = `(file "test.sv"
  (package p)
  (module m
    (timeunit 1ns / 1ps)
    (timeprecision 10ps)
    (defparam
      (assign = (. u1 W) 8)
      (assign = (. (index (. u2 sub) 1) W) 4))
    (bind top.u1 : a.b,c
      (instantiation checker_t
        (instance chk
          (connection .clk clk))))
    (bind m
      (instantiation other_t
        (instance o
          (connection .x x))))
    (let max = (?: (> a b) a b)
      (port a)
      (port b = 1))
    (alias [w1 w2 w3])
    (export p::f)
    (export *::*)
    (dpi-export "DPI-C" c_f = function f)
    (dpi-export "DPI-C" task t)
    (default-disable-iff rst)
    (default-clocking cb)
    (clocking cb2 default
      (timing @(posedge clk))
      (clocking-default input #1step output negedge #2)
      (clocking-item input
        (clocking-signal a)
        (clocking-signal b))
      (clocking-item output out-skew=#3
        (clocking-signal c = (. top c)))
      (clocking-item input output
        (clocking-signal d))
      (clocking-item inout
        (clocking-signal e)))
    (clocking g global
      (timing @(posedge clk)))
    (clocking 
      (timing @(negedge clk))))
  (config cfg design=[lib.top other.cell]
    (localparam P = 1)
    (rule default "liblist lib1 lib2")
    (rule instance "top.u1 use lib.cell2")
    (rule cell "lib.c use x")))
`

func TestMiscItemsGolden(t *testing.T) {
	f, errs := parseSrc(t, miscSource)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	if got := ast.Dump(f); strings.TrimSpace(got) != strings.TrimSpace(miscDump) {
		t.Fatalf("dump mismatch:\n%s", got)
	}
}

func TestDefaultClockingReferenceVersusDeclaration(t *testing.T) {
	body := moduleBody(t, "default clocking cb;\ndefault clocking cb2 @(posedge clk); endclocking")
	if _, ok := body[0].(*ast.DefaultClocking); !ok {
		t.Fatalf("got %T", body[0])
	}
	if c, ok := body[1].(*ast.Clocking); !ok || !c.Default || c.Name != "cb2" {
		t.Fatalf("got %s", ast.Dump(body[1]))
	}
}

func TestMiscErrorsAreReportedOnlyForStructure(t *testing.T) {
	// Malformed values inside defparam, let and clocking items are suppressed
	// like any expression error.
	f, errs := parseSrc(t, "module m; defparam u.W = ; let f(a) = a +; clocking c @(posedge clk); input a = ; endclocking endmodule")
	if len(errs) != 0 {
		t.Fatalf("reported: %+v", errs)
	}
	if len(f.Decls[0].(*ast.Container).Body) != 3 {
		t.Fatalf("items: %s", ast.Dump(f))
	}
}

func TestUnterminatedClockingStopsAtEndmodule(t *testing.T) {
	f, errs := parseSrc(t, "module a; clocking cb @(posedge clk); input x;\nendmodule\nmodule b; endmodule")
	if len(errs) == 0 || len(f.Decls) != 2 {
		t.Fatalf("errs=%v decls=%d", errs, len(f.Decls))
	}
}

func TestUnterminatedConfigStopsAtModule(t *testing.T) {
	f, errs := parseSrc(t, "config c; design top;\nmodule b; endmodule")
	if len(errs) == 0 || len(f.Decls) != 2 {
		t.Fatalf("errs=%v decls=%d", errs, len(f.Decls))
	}
}

func TestMissingSemicolonOnMiscItemDoesNotEatNextDecl(t *testing.T) {
	for _, item := range []string{"timeunit 1ns", "defparam u.W = 1", "alias a = b", "let f = 1", "export p::f"} {
		body, errs := moduleBodyErrs(t, item+"\nlogic next;")
		if len(errs) == 0 {
			t.Errorf("%q: expected an error", item)
		}
		found := false
		for _, d := range body {
			if v, ok := d.(*ast.Variable); ok && v.Name == "next" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: next declaration lost", item)
		}
	}
}
