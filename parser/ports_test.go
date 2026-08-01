package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

func tokenTexts(toks []preprocessor.Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}

func moduleWithPorts(t *testing.T, src string) *ast.Container {
	t.Helper()
	f, errs := parseSrc(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %+v", f.Decls)
	}
	c, ok := f.Decls[0].(*ast.Container)
	if !ok {
		t.Fatalf("expected *ast.Container, got %T", f.Decls[0])
	}
	return c
}

func TestPortsExplicitTypeAndDirection(t *testing.T) {
	c := moduleWithPorts(t, "module top(input logic clk, output logic [7:0] data); endmodule")
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %+v", c.Ports)
	}
	clk := c.Ports[0]
	if clk.Direction != ast.DirInput || clk.Type.Name != "logic" || clk.Name != "clk" {
		t.Fatalf("unexpected clk port: %+v", clk)
	}
	data := c.Ports[1]
	if data.Direction != ast.DirOutput || data.Type.Name != "logic" || data.Name != "data" {
		t.Fatalf("unexpected data port: %+v", data)
	}
	if len(data.Type.PackedDims) != 1 {
		t.Fatalf("expected 1 packed dim, got %+v", data.Type.PackedDims)
	}
	if len(data.Type.PackedDims[0].Left) != 1 || data.Type.PackedDims[0].Left[0].Text != "7" {
		t.Fatalf("unexpected packed dim: %+v", data.Type.PackedDims[0])
	}
	if len(data.Type.PackedDims[0].Right) != 1 || data.Type.PackedDims[0].Right[0].Text != "0" {
		t.Fatalf("unexpected packed dim: %+v", data.Type.PackedDims[0])
	}
}

func TestPortsImplicitType(t *testing.T) {
	c := moduleWithPorts(t, "module top(input clk, output rst_n); endmodule")
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %+v", c.Ports)
	}
	for _, port := range c.Ports {
		if port.Type.Name != "" {
			t.Errorf("expected implicit (empty) type for %s, got %+v", port.Name, port.Type)
		}
	}
	if c.Ports[0].Name != "clk" || c.Ports[1].Name != "rst_n" {
		t.Fatalf("unexpected port names: %+v", c.Ports)
	}
}

func TestPortsImplicitTypeWithPackedDim(t *testing.T) {
	c := moduleWithPorts(t, "module top(input [7:0] data); endmodule")
	if len(c.Ports) != 1 {
		t.Fatalf("expected 1 port, got %+v", c.Ports)
	}
	port := c.Ports[0]
	if port.Name != "data" || port.Type.Name != "" || len(port.Type.PackedDims) != 1 {
		t.Fatalf("unexpected port: %+v", port)
	}
}

func TestPortsNoDirectionIsUnspecified(t *testing.T) {
	c := moduleWithPorts(t, "module top(logic clk); endmodule")
	if c.Ports[0].Direction != ast.DirUnspecified {
		t.Fatalf("expected DirUnspecified, got %+v", c.Ports[0])
	}
}

func TestPortsRefVarDirection(t *testing.T) {
	c := moduleWithPorts(t, "module top(ref var int x); endmodule")
	if c.Ports[0].Direction != ast.DirRef || c.Ports[0].Name != "x" {
		t.Fatalf("unexpected port: %+v", c.Ports[0])
	}
}

func TestPortsUnpackedDimension(t *testing.T) {
	c := moduleWithPorts(t, "module top(input logic [7:0] data [4]); endmodule")
	port := c.Ports[0]
	if len(port.Type.PackedDims) != 1 {
		t.Fatalf("expected 1 packed dim, got %+v", port.Type.PackedDims)
	}
	if len(port.UnpackedDims) != 1 || len(port.UnpackedDims[0].Left) != 1 || port.UnpackedDims[0].Left[0].Text != "4" {
		t.Fatalf("expected 1 unpacked dim [4], got %+v", port.UnpackedDims)
	}
}

func TestPortsDefaultValue(t *testing.T) {
	c := moduleWithPorts(t, "module top(input logic en = 1); endmodule")
	port := c.Ports[0]
	if len(port.Default) != 1 || port.Default[0].Text != "1" {
		t.Fatalf("unexpected default: %+v", port.Default)
	}
}

func TestPortsEmptyParensIsNoPorts(t *testing.T) {
	c := moduleWithPorts(t, "module top(); endmodule")
	if len(c.Ports) != 0 {
		t.Fatalf("expected no ports, got %+v", c.Ports)
	}
}

func TestPortsNoParensAtAllIsNoPorts(t *testing.T) {
	c := moduleWithPorts(t, "module top; endmodule")
	if len(c.Ports) != 0 {
		t.Fatalf("expected no ports, got %+v", c.Ports)
	}
}

func TestPortsQualifiedType(t *testing.T) {
	c := moduleWithPorts(t, "module top(input pkg::my_t x); endmodule")
	port := c.Ports[0]
	if port.Type.PackageQualifier != "pkg" || port.Type.Name != "my_t" || port.Name != "x" {
		t.Fatalf("unexpected port: %+v", port)
	}
}

func TestPortsSignedUnsigned(t *testing.T) {
	c := moduleWithPorts(t, "module top(input logic signed [7:0] a, input unsigned [3:0] b); endmodule")
	if !c.Ports[0].Type.Signed {
		t.Fatalf("expected a to be signed: %+v", c.Ports[0])
	}
	if !c.Ports[1].Type.Unsigned {
		t.Fatalf("expected b to be unsigned: %+v", c.Ports[1])
	}
}

func TestPortsDimensionWithFunctionCallExpression(t *testing.T) {
	// $clog2(WIDTH) inside a dimension bound must not confuse the ':'
	// stop-condition (its own '(' ')' must be tracked at a deeper depth).
	c := moduleWithPorts(t, "module top(input logic [$clog2(WIDTH)-1:0] addr); endmodule")
	port := c.Ports[0]
	dim := port.Type.PackedDims[0]
	leftTexts := tokenTexts(dim.Left)
	want := []string{"$clog2", "(", "WIDTH", ")", "-", "1"}
	if len(leftTexts) != len(want) {
		t.Fatalf("Left = %v, want %v", leftTexts, want)
	}
	for i := range want {
		if leftTexts[i] != want[i] {
			t.Fatalf("Left = %v, want %v", leftTexts, want)
		}
	}
}

func TestParamPortList(t *testing.T) {
	c := moduleWithPorts(t, "module top #(parameter int WIDTH = 8, HEIGHT = 4) (); endmodule")
	if len(c.Params) != 2 {
		t.Fatalf("expected 2 params, got %+v", c.Params)
	}
	w := c.Params[0]
	if w.Name != "WIDTH" || w.Type.Name != "int" || len(w.Default) != 1 || w.Default[0].Text != "8" {
		t.Fatalf("unexpected WIDTH param: %+v", w)
	}
	h := c.Params[1]
	// HEIGHT has no explicit "parameter"/"int" repeated -- still parses,
	// implicit type, inheriting "parameter" kind from context (not
	// "localparam"), matching real SV shorthand.
	if h.Name != "HEIGHT" || h.IsLocal || h.Type.Name != "" {
		t.Fatalf("unexpected HEIGHT param: %+v", h)
	}
}

func TestParamPortListLocalparam(t *testing.T) {
	c := moduleWithPorts(t, "module top #(localparam int MAX = 255) (); endmodule")
	if !c.Params[0].IsLocal {
		t.Fatalf("expected IsLocal, got %+v", c.Params[0])
	}
}

func TestParamPortListWithUnpackedDim(t *testing.T) {
	c := moduleWithPorts(t, "module top #(parameter int W[1:0] = '0) (); endmodule")
	w := c.Params[0]
	if w.Name != "W" || len(w.UnpackedDims) != 1 || len(w.UnpackedDims[0].Left) != 1 ||
		w.UnpackedDims[0].Left[0].Text != "1" || len(w.UnpackedDims[0].Right) != 1 || w.UnpackedDims[0].Right[0].Text != "0" {
		t.Fatalf("unexpected W param: %+v", w)
	}
	if len(w.Default) != 1 || w.Default[0].Text != "'0" {
		t.Fatalf("unexpected default: %+v", w.Default)
	}
}

func TestParamPortListNoParams(t *testing.T) {
	c := moduleWithPorts(t, "module top(input logic clk); endmodule")
	if len(c.Params) != 0 {
		t.Fatalf("expected no params, got %+v", c.Params)
	}
}

func TestPortsInterfaceWithModport(t *testing.T) {
	// "simple_bus.slave sb" (LRM 25.3's interface_port_header, specific
	// interface type + modport) -- the '.' must not send this through
	// parseTypeAndName's implicit-type rewind, which would otherwise take
	// "simple_bus" itself as the port name and silently drop ".slave sb".
	c := moduleWithPorts(t, "module top(simple_bus.slave sb, input logic clk); endmodule")
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %+v", c.Ports)
	}
	sb := c.Ports[0]
	if sb.Name != "sb" || sb.Type.Name != "simple_bus" {
		t.Fatalf("unexpected interface port: %+v", sb)
	}
	if c.Ports[1].Name != "clk" {
		t.Fatalf("unexpected second port: %+v", c.Ports[1])
	}
}

func TestPortsInterfaceWithoutModport(t *testing.T) {
	// "simple_bus sb" -- a specific interface type with no modport
	// restriction, lexically identical to any other "[type] name" port
	// entry, so it's expected to already work via parseTypeAndName with no
	// special-casing needed.
	c := moduleWithPorts(t, "module top(simple_bus sb); endmodule")
	if len(c.Ports) != 1 || c.Ports[0].Name != "sb" || c.Ports[0].Type.Name != "simple_bus" {
		t.Fatalf("unexpected port: %+v", c.Ports)
	}
}

func TestPortsGenericInterfaceKeyword(t *testing.T) {
	// "interface b" -- the generic interface_port_header form (any
	// interface type, LRM 25.3), distinct from a specific interface_
	// identifier. "interface" itself is a reserved keyword, so this can't
	// reach parseTypeAndName's ordinary "[type] name" path at all.
	c := moduleWithPorts(t, "module top(interface b); endmodule")
	if len(c.Ports) != 1 || c.Ports[0].Name != "b" || c.Ports[0].Type.Name != "interface" {
		t.Fatalf("unexpected port: %+v", c.Ports)
	}
}

func TestPortsGenericInterfaceKeywordWithModport(t *testing.T) {
	c := moduleWithPorts(t, "module top(interface.mb b); endmodule")
	if len(c.Ports) != 1 || c.Ports[0].Name != "b" || c.Ports[0].Type.Name != "interface" {
		t.Fatalf("unexpected port: %+v", c.Ports)
	}
}

func TestPortsInterfaceWithModportAndUnpackedDim(t *testing.T) {
	c := moduleWithPorts(t, "module top(simple_bus.slave sb [2]); endmodule")
	if len(c.Ports) != 1 || c.Ports[0].Name != "sb" || c.Ports[0].Type.Name != "simple_bus" {
		t.Fatalf("unexpected port: %+v", c.Ports)
	}
	if len(c.Ports[0].UnpackedDims) != 1 {
		t.Fatalf("expected 1 unpacked dim, got %+v", c.Ports[0].UnpackedDims)
	}
}

func TestPortsTrailingGarbageIsRecordedAsAnError(t *testing.T) {
	// Two names with no separating comma is malformed -- previously
	// silently discarded after the first name; now recorded.
	_, errs := parseSrc(t, "module top(logic clk extra); endmodule")
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error for the trailing garbage, got %+v", errs)
	}
}

func TestPortsMalformedEntryDoesNotBlockOthers(t *testing.T) {
	// A malformed entry (bare "42", not a valid name) doesn't prevent the
	// other, valid entries in the same list from parsing -- each group is
	// isolated by SplitBalancedArgs already, so one bad group can't even
	// desync the token stream for its neighbors. This deliberately
	// malformed input is expected to record an error, unlike
	// moduleWithPorts's other callers, so it doesn't use that helper.
	f, errs := parseSrc(t, "module top(input logic clk, 42, output logic rst); endmodule")
	if len(errs) != 1 {
		t.Fatalf("expected exactly one error for the malformed \"42\" entry, got %+v", errs)
	}
	c := f.Decls[0].(*ast.Container)
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 valid ports despite the malformed middle entry, got %+v", c.Ports)
	}
	if c.Ports[0].Name != "clk" || c.Ports[1].Name != "rst" {
		t.Fatalf("unexpected ports: %+v", c.Ports)
	}
}
