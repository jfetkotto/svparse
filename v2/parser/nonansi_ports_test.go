package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestNonANSIPortDeclAttachesDirectionAndType(t *testing.T) {
	// The exact repro shape from the non-ANSI-port issue: bare names in
	// the header, direction supplied separately in the body.
	c := moduleWithPorts(t, "module top\n(\n tdi_swj_s,\n tdo_swj_s\n);\ninput  tdi_swj_s;\noutput tdo_swj_s;\nendmodule")
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %+v", c.Ports)
	}
	if c.Ports[0].Name != "tdi_swj_s" || c.Ports[0].Direction != ast.DirInput {
		t.Fatalf("unexpected first port: %+v", c.Ports[0])
	}
	if c.Ports[1].Name != "tdo_swj_s" || c.Ports[1].Direction != ast.DirOutput {
		t.Fatalf("unexpected second port: %+v", c.Ports[1])
	}
	if len(c.Body) != 0 {
		t.Fatalf("expected the merged PortDecls to leave no trace in Body, got %+v", c.Body)
	}
}

func TestNonANSIPortDeclWithTypeAndWidth(t *testing.T) {
	c := moduleWithPorts(t, "module top(dapcaddr_ctrlap_app);\noutput [7:0] dapcaddr_ctrlap_app;\nendmodule")
	if len(c.Ports) != 1 {
		t.Fatalf("expected 1 port, got %+v", c.Ports)
	}
	p := c.Ports[0]
	if p.Direction != ast.DirOutput || len(p.Type.PackedDims) != 1 {
		t.Fatalf("unexpected port: %+v", p)
	}
}

func TestNonANSIPortDeclWithNetTypeKeyword(t *testing.T) {
	c := moduleWithPorts(t, "module top(clk);\ninput wire logic clk;\nendmodule")
	if len(c.Ports) != 1 {
		t.Fatalf("expected 1 port, got %+v", c.Ports)
	}
	p := c.Ports[0]
	if p.Direction != ast.DirInput || p.Type.Name != "logic" {
		t.Fatalf("unexpected port: %+v", p)
	}
}

func TestNonANSIPortDeclSharesTypeAcrossMultipleNames(t *testing.T) {
	// "output [7:0] a, b;" -- one statement, one shared type, two names.
	c := moduleWithPorts(t, "module top(a, b);\noutput [7:0] a, b;\nendmodule")
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %+v", c.Ports)
	}
	for _, p := range c.Ports {
		if p.Direction != ast.DirOutput || len(p.Type.PackedDims) != 1 {
			t.Fatalf("expected both ports to share direction/type, got %+v", p)
		}
	}
}

func TestNonANSIPortDeclUndeclaredNameIsAnError(t *testing.T) {
	// "input" naming something absent from the header port list has
	// nothing to attach to.
	_, errs := parseSrc(t, "module top(a);\ninput a;\ninput b;\nendmodule")
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error for the undeclared port, got %+v", errs)
	}
}

func TestNonANSIPortDeclDoesNotAffectANSIModules(t *testing.T) {
	// A plain ANSI module must be entirely unaffected by this new dispatch
	// case -- "input"/"output" never appear at body scope there.
	c := moduleWithPorts(t, "module top(input logic clk); endmodule")
	if len(c.Ports) != 1 || c.Ports[0].Direction != ast.DirInput || c.Ports[0].Type.Name != "logic" {
		t.Fatalf("unexpected port: %+v", c.Ports)
	}
	if len(c.Body) != 0 {
		t.Fatalf("expected no body decls, got %+v", c.Body)
	}
}
