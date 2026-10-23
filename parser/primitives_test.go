package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

const primitivesSource = `primitive mux (out, sel, a, b);
  output out;
  input sel, a, b;
  table
    0 0 ? : 0;
    0 1 ? : 1;
    1 ? 0 : 0;
    (01) ? ? : - ;
  endtable
endprimitive
primitive dff (output reg q, input d, clk);
  initial q = 0;
  table
    r 0 : ? : 0;
  endtable
endprimitive
module m;
  and #(1,2) (strong0, weak1) g1 (o, a, b), g2[3:0] (o2, a2, b2);
  nand (o, a, b);
  pullup (p);
  bufif0 #5 (y, a, en);
endmodule
`

const primitivesDump = `(file "test.sv"
  (udp mux
    (port output out)
    (port input sel)
    (port input a)
    (port input b)
    (row 0 0 ? : 0)
    (row 0 1 ? : 1)
    (row 1 ? 0 : 0)
    (row (01) ? ? : -))
  (udp dff sequential
    (port output q <reg>)
    (port input d)
    (port clk)
    (initial
      (assign = q 0))
    (row r 0 : ? : 0))
  (module m
    (gate and strength=(strong0,weak1) delay=[1 2]
      (gate-instance g1 (o a b))
      (gate-instance g2 [3:0] (o2 a2 b2)))
    (gate nand
      (gate-instance _ (o a b)))
    (gate pullup
      (gate-instance _ (p)))
    (gate bufif0 delay=[5]
      (gate-instance _ (y a en)))))
`

func TestPrimitivesGolden(t *testing.T) {
	f, errs := parseSrc(t, primitivesSource)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	if got := ast.Dump(f); strings.TrimSpace(got) != strings.TrimSpace(primitivesDump) {
		t.Fatalf("dump mismatch:\n%s", got)
	}
}

func TestGateStrengthAfterDelayAndUnnamedInstance(t *testing.T) {
	body := moduleBody(t, "or #3 (weak0, weak1) (y, a, b);")
	g, ok := body[0].(*ast.GateInstantiation)
	if !ok || len(g.DriveStrength) != 2 || len(g.Delay) != 1 || len(g.Instances) != 1 || g.Instances[0].Name != "" {
		t.Fatalf("got %s", ast.Dump(body[0]))
	}
	if n := len(g.Instances[0].Terminals); n != 3 {
		t.Fatalf("terminals = %d", n)
	}
}

func TestUDPSequentialFlagAndEdgeSymbols(t *testing.T) {
	f, errs := parseSrc(t, "primitive p (q, d, c); output q; reg q; input d, c; table (01) 0 : ? : 1; (0?) 1 : 1 : -; endtable endprimitive")
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	u := f.Decls[0].(*ast.UDP)
	if !u.Sequential || len(u.Rows) != 2 || u.Rows[1].Inputs[0] != "(0?)" || u.Rows[0].Current != "?" {
		t.Fatalf("got %s", ast.Dump(u))
	}
	if len(u.Ports) != 3 || u.Ports[0].Direction == "" {
		t.Fatalf("ports not merged: %+v", u.Ports)
	}
}

func TestUDPMissingEndprimitiveStopsAtEndmodule(t *testing.T) {
	f, errs := parseSrc(t, "primitive p (o, i); output o; input i; table 0 : 1; endtable\nmodule m; endmodule")
	if len(errs) == 0 {
		t.Fatal("expected an error")
	}
	if len(f.Decls) != 2 {
		t.Fatalf("module lost: %d decls", len(f.Decls))
	}
}

func TestUDPBadRowIsReportedAndSkipped(t *testing.T) {
	f, errs := parseSrc(t, "primitive p (o, i); output o; input i; table garbage; 0 : 1; endtable endprimitive")
	if len(errs) != 1 {
		t.Fatalf("errors: %+v", errs)
	}
	if u := f.Decls[0].(*ast.UDP); len(u.Rows) != 1 {
		t.Fatalf("rows: %d", len(u.Rows))
	}
}

func TestGateMissingSemicolonDoesNotEatNextDecl(t *testing.T) {
	body, errs := moduleBodyErrs(t, "and g (o, a, b)\nlogic next;")
	if len(errs) == 0 {
		t.Fatal("expected an error")
	}
	found := false
	for _, d := range body {
		if v, ok := d.(*ast.Variable); ok && v.Name == "next" {
			found = true
		}
	}
	if !found {
		t.Fatalf("next lost: %v", body)
	}
}

func moduleBodyErrs(t *testing.T, src string) ([]ast.Decl, []Error) {
	t.Helper()
	f, errs := parseSrc(t, "module top;\n"+src+"\nendmodule")
	return f.Decls[0].(*ast.Container).Body, errs
}
