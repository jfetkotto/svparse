package elab

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
)

func codesOf(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return strings.Join(out, ",")
}

// mustElabLenient elaborates and fails on errors, tolerating warnings (the
// width mismatches some connections here have on purpose).
func mustElabLenient(t testing.TB, src string) *Design {
	t.Helper()
	d, ds := elaborate(t, src, Options{})
	for _, x := range ds {
		if x.Severity == diag.Error {
			t.Fatalf("diagnostics: %v", ds)
		}
	}
	return d
}

func portOf(n *Node, name string) *PortBinding {
	for i := range n.Ports {
		if n.Ports[i].Name == name {
			return &n.Ports[i]
		}
	}
	return nil
}

func exprText(b *PortBinding) string {
	if b == nil || b.Expr == nil {
		return "-"
	}
	return ast.ExprString(b.Expr)
}

func TestPortConnectionForms(t *testing.T) {
	d := mustElabLenient(t, `module top;
  logic [7:0] a, b, q; logic clk;
  wire [3:0] n;
  leaf pos(clk, a, q);
  leaf named(.clk(clk), .d(b), .q(q));
  leaf sparse(.clk(clk), .d(), .q());
  leaf short(clk);
  leaf imp(.clk, .d(a), .q);
  leaf wild(.*);
  leaf mixed_wild(.*, .d(b), .q());
  leaf bits(.clk(clk), .d(a[3:0]), .q(n));
  leaf cat(.clk(clk), .d({a[3:0], b[3:0]}), .q({n, n}));
endmodule
module leaf(input clk, input [7:0] d, output [7:0] q); endmodule`)
	want := map[string]string{
		"top.pos":        "clk:pos:clk,d:pos:a,q:pos:q",
		"top.named":      "clk:named:clk,d:named:b,q:named:q",
		"top.sparse":     "clk:named:clk,d:none:-,q:none:-",
		"top.short":      "clk:pos:clk,d:none:-,q:none:-",
		"top.imp":        "clk:implicit:clk,d:named:a,q:implicit:q",
		"top.wild":       "clk:wildcard:clk,d:none:-,q:wildcard:q",
		"top.mixed_wild": "clk:wildcard:clk,d:named:b,q:none:-",
	}
	kinds := map[ConnKind]string{ConnNone: "none", ConnPositional: "pos", ConnNamed: "named", ConnImplicit: "implicit", ConnWildcard: "wildcard"}
	for path, w := range want {
		var got []string
		for _, b := range d.Find(path).Ports {
			got = append(got, b.Name+":"+kinds[b.Kind]+":"+exprText(&b))
		}
		if strings.Join(got, ",") != w {
			t.Errorf("%s:\n got %s\nwant %s", path, strings.Join(got, ","), w)
		}
	}
	// Types are resolved on both sides.
	b := portOf(d.Find("top.pos"), "d")
	if b.PortType.String() != "logic [7:0]" || b.ExprType.String() != "logic [7:0]" || b.Direction != ast.DirInput {
		t.Errorf("types: %v %v %v", b.PortType, b.ExprType, b.Direction)
	}
	if q := portOf(d.Find("top.pos"), "q"); q.Direction != ast.DirOutput {
		t.Errorf("q direction %v", q.Direction)
	}
}

func TestPortWidthsFollowTheInstance(t *testing.T) {
	// The port's width depends on the instance's parameter: each instance is
	// checked against its own.
	c := compile(t, map[string]string{"t.sv": `module top;
  logic [7:0] a8; logic [15:0] a16;
  leaf #(.W(8)) ok8(.d(a8));
  leaf #(.W(16)) ok16(.d(a16));
  leaf #(.W(8)) bad(.d(a16));
endmodule
module leaf #(parameter W = 4) (input [W-1:0] d); endmodule`})
	d, ds := Elaborate(nil, c, Options{})
	if len(ds) != 1 || ds[0].Code != diag.CodePortWidthMismatch || ds[0].Severity != diag.Warning {
		t.Fatalf("diagnostics: %v", ds)
	}
	if w := portOf(d.Find("top.ok16"), "d").PortType.String(); w != "logic [15:0]" {
		t.Errorf("ok16 port type = %s", w)
	}
	if w := portOf(d.Find("top.bad"), "d").PortType.String(); w != "logic [7:0]" {
		t.Errorf("bad port type = %s", w)
	}
}

func TestPortConnectionErrors(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"unknown port", "leaf u(.nope(a));", diag.CodeUnknownPort},
		{"too many", "leaf u(a, a, a);", diag.CodeTooManyPorts},
		{"duplicate", "leaf u(.d(a), .d(a));", diag.CodeDuplicatePortConnection},
		{"mixed", "leaf u(a, .q(a));", diag.CodeMixedPortConnections},
		{"output to an expression", "leaf u(.d(a), .q(a + 1));", diag.CodeInvalidOutputConnection},
		{"output to a literal", "leaf u(.d(a), .q(8'h0));", diag.CodeInvalidOutputConnection},
		{"inout to an expression", "wide u(.p(a + 1));", diag.CodeInvalidInoutConnection},
		{"input from a string", `leaf u(.d("str"), .q(a));`, diag.CodePortWidthMismatch},
		{"input from a real", "leaf u(.d(1.5), .q(a));", ""},
		{"undeclared expression", "leaf u(.d(nosuch + 1));", diag.CodeUndeclared},
		{"width warning", "leaf u(.d(w16), .q(a));", diag.CodePortWidthMismatch},
		{"output width warning", "leaf u(.d(a), .q(w16));", diag.CodePortWidthMismatch},
		{"fine", "leaf u(.d(a), .q(a));", ""},
		{"inout to a net", "wide u(.p(w16));", ""},
	}
	for _, c := range cases {
		src := `module top; logic [7:0] a; logic [15:0] w16;` + c.body + `
endmodule
module leaf(input [7:0] d, output [7:0] q); endmodule
module wide(inout [15:0] p); endmodule`
		_, ds := elaborate(t, src, Options{})
		if got := codesOf(ds); got != c.want {
			t.Errorf("%s: got [%s], want [%s]\n%v", c.name, got, c.want, ds)
		}
	}
}

func TestImplicitNets(t *testing.T) {
	d := mustElab(t, `module top;
  logic [3:0] known;
  leaf u(.d(known), .q(out_net));
  leaf v(.d(in_net), .q());
  assign driven = known[0];
  leaf w(.*);
endmodule
module leaf(input [3:0] d, output [3:0] q); endmodule`, Options{})
	var got []string
	for _, n := range d.Tops[0].ImplicitNets {
		got = append(got, n.Name+":"+itoa(n.Width))
	}
	// The connected nets take the width of the port they connect to.
	if strings.Join(got, ",") != "out_net:4,in_net:4,driven:1" && strings.Join(got, ",") != "driven:1,out_net:4,in_net:4" {
		t.Errorf("implicit nets = %v", got)
	}
	// With `default_nettype none they are errors.
	_, ds := elaborate(t, `module top; leaf u(.d(nope), .q()); assign x = 1; endmodule
module leaf(input d, output q); endmodule`, Options{DefaultNettype: "none"})
	if codesOf(ds) != "undeclared-identifier,undeclared-identifier" {
		t.Errorf("default_nettype none: %v", ds)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestInterfacePorts(t *testing.T) {
	d := mustElab(t, `interface bus_if(input clk); logic [7:0] data; modport master(output data); modport slave(input data); endinterface
module top;
  logic clk;
  bus_if b0(clk), b1(clk);
  producer p(.ifc(b0.master));
  consumer c(.ifc(b0));
  generic g(.ifc(b1));
  wrap w(b1);
endmodule
module producer(bus_if.master ifc); endmodule
module consumer(bus_if ifc); endmodule
module generic(interface ifc); endmodule
module wrap(bus_if ifc); consumer c(.ifc(ifc)); endmodule`, Options{})
	p := portOf(d.Find("top.p"), "ifc")
	if p.Interface == nil || p.Interface.Name != "b0" || p.Modport != "master" {
		t.Errorf("producer: %+v", p)
	}
	if portOf(d.Find("top.c"), "ifc").Interface.Name != "b0" || portOf(d.Find("top.g"), "ifc").Interface.Name != "b1" {
		t.Error("consumer/generic")
	}
	// An interface port passes through a level of hierarchy.
	if in := portOf(d.Find("top.w.c"), "ifc").Interface; in == nil || in.Path() != "top.b1" {
		t.Errorf("wrap.c: %+v", portOf(d.Find("top.w.c"), "ifc"))
	}

	cases := []struct{ name, conn, want string }{
		{"wrong interface", "other_if o(); consumer c(.ifc(o));", diag.CodeInterfacePortMismatch},
		{"not an interface", "logic v; consumer c(.ifc(v));", diag.CodeInterfacePortMismatch},
		{"no such modport", "consumer c(.ifc(b0.nosuch));", diag.CodeInterfacePortMismatch},
		{"expression", "consumer c(.ifc(1 + 2));", diag.CodeInterfacePortMismatch},
		{"fine", "consumer c(.ifc(b0));", ""},
	}
	for _, c := range cases {
		src := "interface bus_if; modport master(); endinterface interface other_if; endinterface\nmodule top; bus_if b0(); " + c.conn + " endmodule\nmodule consumer(bus_if ifc); endmodule"
		_, ds := elaborate(t, src, Options{})
		if got := codesOf(ds); got != c.want {
			t.Errorf("%s: got [%s], want [%s]\n%v", c.name, got, c.want, ds)
		}
	}
}

func TestBind(t *testing.T) {
	d := mustElab(t, `module top; cpu c0(); cpu c1(); other o(); endmodule
module cpu; logic [3:0] state; alu a(); endmodule
module alu; endmodule
module other; endmodule
module checker_m #(parameter N = 1) (input [3:0] st); endmodule
bind cpu checker_m #(.N(5)) chk (.st(state));
bind top.o checker_m on_o (.st(4'b0));
bind cpu : a checker_m in_alu (.st(4'b0));`, Options{})
	for _, path := range []string{"top.c0.chk", "top.c1.chk", "top.o.on_o", "top.c0.a.in_alu", "top.c1.a.in_alu"} {
		n := d.Find(path)
		if n == nil || n.Def.Name != "checker_m" {
			t.Errorf("%s is missing", path)
		}
	}
	if got := paramStr(d.Find("top.c1.chk"), "N"); got != "32'sh00000005" {
		t.Errorf("bound parameter N = %s", got)
	}
	// Connections are written in the target's scope.
	if b := portOf(d.Find("top.c0.chk"), "st"); b.Expr == nil || ast.ExprString(b.Expr) != "state" || b.ExprType.String() != "logic [3:0]" {
		t.Errorf("chk.st: %+v", b)
	}
	if d.Find("top.o.chk") != nil {
		t.Error("the module-name bind must not reach other modules")
	}
	// An unknown target is an error.
	_, ds := elaborate(t, "module top; endmodule module chk; endmodule bind nowhere chk c();", Options{})
	if codesOf(ds) != diag.CodeUnknownBindTarget {
		t.Errorf("unknown bind target: %v", ds)
	}
	// A bound module is not a top.
	d2 := mustElab(t, "module top; endmodule module chk; endmodule bind top chk c();", Options{})
	if len(d2.Tops) != 1 || d2.Tops[0].Name != "top" || d2.Find("top.c") == nil {
		t.Errorf("tree:\n%s", tree(d2))
	}
}

func TestResolveHier(t *testing.T) {
	d := mustElab(t, `module top;
  int top_sig;
  mid m0();
  genvar i;
  for (i = 0; i < 2; i++) begin : g
    leaf l();
    int in_blk;
  end
  leaf arr[2] ();
endmodule
module mid; int mid_sig; leaf l(); endmodule
module leaf; int lsig; endmodule`, Options{})
	l := d.Find("top.m0.l")
	cases := []struct {
		from  *Node
		path  string
		node  string
		sym   string
		rest  string
		found bool
	}{
		{l, "lsig", "top.m0.l", "lsig", "", true},
		{l, "mid_sig", "top.m0", "mid_sig", "", true}, // upward: the enclosing module's signal
		{l, "top_sig", "top", "top_sig", "", true},
		{l, "top.m0.mid_sig", "top.m0", "mid_sig", "", true},
		{l, "m0.l.lsig", "top.m0.l", "lsig", "", true},
		{d.Find("top"), "m0.l", "top.m0.l", "", "", true},
		{d.Find("top"), "g[1].l.lsig", "top.g[1].l", "lsig", "", true},
		{d.Find("top"), "g[1].in_blk", "top.g[1]", "in_blk", "", true},
		{d.Find("top"), "arr[1].lsig", "top.arr[1]", "lsig", "", true},
		{d.Find("top"), "top_sig.field.sub", "top", "top_sig", "field.sub", true},
		{l, "$root.top.top_sig", "top", "top_sig", "", true},
		{l, "nothing", "", "", "", false},
		{l, "m0.nothing", "", "", "", false},
		{l, "g.l", "", "", "", false}, // a loop block needs its index
	}
	for _, c := range cases {
		ref, ok := c.from.ResolveHier(strings.Split(c.path, "."))
		if c.path == "g[1].l.lsig" || c.path == "g[1].in_blk" || c.path == "arr[1].lsig" {
			ref, ok = c.from.ResolveHier(splitPath(c.path))
		}
		if ok != c.found {
			t.Errorf("%s: found=%v, want %v", c.path, ok, c.found)
			continue
		}
		if !ok {
			continue
		}
		sym := ""
		if ref.Sym != nil {
			sym = ref.Sym.Name
		}
		if ref.Node.Path() != c.node || sym != c.sym || strings.Join(ref.Rest, ".") != c.rest {
			t.Errorf("%s: node %s sym %q rest %q, want %s %q %q", c.path, ref.Node.Path(), sym, strings.Join(ref.Rest, "."), c.node, c.sym, c.rest)
		}
	}
	if ref, ok := d.ResolveHier([]string{"top", "m0", "mid_sig"}); !ok || ref.Node.Path() != "top.m0" {
		t.Errorf("Design.ResolveHier: %+v %v", ref, ok)
	}
	if _, ok := d.ResolveHier([]string{"nothing"}); ok {
		t.Error("Design.ResolveHier of a missing top")
	}
	if _, ok := d.ResolveHier(nil); ok {
		t.Error("empty path")
	}
	var nilNode *Node
	if _, ok := nilNode.ResolveHier([]string{"a"}); ok {
		t.Error("nil node")
	}
}

// Each instance of an array connects to a slice of a bus n times its width.
func TestInstanceArrayPortsTakeSlices(t *testing.T) {
	src := `module top;
  logic [7:0] bus8, out8; logic [3:0] bus4;
  bit1 a [7:0] (.d(bus8), .q(out8));
  bit1 b [7:0] (.d(bus4), .q(out8));
  bit1 c [7:0] (.d(1'b0), .q(out8));
endmodule
module bit1(input d, output q); endmodule`
	_, ds := elaborate(t, src, Options{})
	if len(ds) != 1 || ds[0].Code != diag.CodePortWidthMismatch || !strings.Contains(ds[0].Message, `connected to 4 bits`) {
		t.Errorf("only the 4-bit bus on 8 instances is a mismatch: %v", ds)
	}
}
