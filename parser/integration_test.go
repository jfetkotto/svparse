package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

// A realistic file exercising every feature this milestone added
// together: a package with struct/enum typedefs, a parameterized module
// with ANSI ports, a package import inside the module body, variables
// using both builtin and typedef'd types (including an unpacked array),
// a function with a body, an instantiation mixing named connections with
// a wildcard, a class with extends/rand/constraint/constructor/extern
// method, and that method's out-of-class qualified definition. As with
// the lexer's and preprocessor's own integration tests, the point isn't
// to check every token -- it's to prove the pieces cohere on real-shaped
// input, not just in isolation.
const integrationSnippet = `package my_pkg;
  typedef struct packed {
    logic [7:0] addr;
    logic       valid;
  } bus_t;

  typedef enum logic [1:0] {
    IDLE = 0,
    BUSY = 1,
    DONE = 2
  } state_t;
endpackage

module top #(
  parameter int WIDTH = 8
) (
  input  logic             clk,
  input  logic             rst_n,
  output logic [WIDTH-1:0] data_out
);

  import my_pkg::*;

  bus_t bus;
  state_t state;
  logic [7:0] mem [256];

  function automatic int add(int a, int b);
    return a + b;
  endfunction

  leaf u_leaf (
    .clk(clk),
    .rst_n(rst_n),
    .*
  );

endmodule

class packet extends base_packet;
  rand bit [7:0] payload;
  int id;

  constraint c_payload { payload inside {[0:255]}; }

  function new();
    id = 0;
  endfunction

  extern function void display();
endclass

function void packet::display();
  $display("id=%0d", id);
endfunction
`

func parseIntegrationSnippet(t *testing.T) (*ast.File, []Error) {
	t.Helper()
	toks, ppErrs := preprocessor.Preprocess("top.sv", integrationSnippet, nil)
	if len(ppErrs) != 0 {
		t.Fatalf("unexpected preprocessor errors: %+v", ppErrs)
	}
	return Parse("top.sv", toks)
}

func TestIntegrationSnippetNoErrors(t *testing.T) {
	_, errs := parseIntegrationSnippet(t)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestIntegrationSnippetTopLevelShape(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	if len(f.Decls) != 4 {
		t.Fatalf("expected 4 top-level decls (package, module, class, out-of-class function), got %+v", f.Decls)
	}
	if _, ok := f.Decls[0].(*ast.Package); !ok {
		t.Fatalf("expected package first, got %T", f.Decls[0])
	}
	if _, ok := f.Decls[1].(*ast.Container); !ok {
		t.Fatalf("expected module second, got %T", f.Decls[1])
	}
	if _, ok := f.Decls[2].(*ast.Class); !ok {
		t.Fatalf("expected class third, got %T", f.Decls[2])
	}
	if _, ok := f.Decls[3].(*ast.Function); !ok {
		t.Fatalf("expected the out-of-class function fourth, got %T", f.Decls[3])
	}
}

func TestIntegrationSnippetPackageTypedefs(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	pkg := f.Decls[0].(*ast.Package)
	if len(pkg.Body) != 2 {
		t.Fatalf("expected 2 typedefs, got %+v", pkg.Body)
	}
	busT := pkg.Body[0].(*ast.Typedef)
	if busT.Name != "bus_t" {
		t.Fatalf("unexpected first typedef: %+v", busT)
	}
	s, ok := busT.Underlying.(*ast.Struct)
	if !ok || !s.Packed || len(s.Members) != 2 {
		t.Fatalf("unexpected bus_t struct: %+v (%T)", busT.Underlying, busT.Underlying)
	}
	stateT := pkg.Body[1].(*ast.Typedef)
	e, ok := stateT.Underlying.(*ast.Enum)
	if !ok || len(e.Members) != 3 {
		t.Fatalf("unexpected state_t enum: %+v", stateT.Underlying)
	}
}

func TestIntegrationSnippetModuleContents(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	mod := f.Decls[1].(*ast.Container)
	if mod.Name != "top" || len(mod.Params) != 1 || len(mod.Ports) != 3 {
		t.Fatalf("unexpected module header: %+v", mod)
	}

	var kinds []string
	for _, d := range mod.Body {
		switch v := d.(type) {
		case *ast.Import:
			kinds = append(kinds, "import:"+v.Package)
		case *ast.Variable:
			kinds = append(kinds, "var:"+v.Name)
		case *ast.Function:
			kinds = append(kinds, "func:"+v.Name)
		case *ast.Instantiation:
			kinds = append(kinds, "inst:"+v.ModuleType)
		default:
			kinds = append(kinds, "other")
		}
	}
	want := []string{"import:my_pkg", "var:bus", "var:state", "var:mem", "func:add", "inst:leaf"}
	if len(kinds) != len(want) {
		t.Fatalf("module body = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("module body = %v, want %v", kinds, want)
		}
	}
}

func TestIntegrationSnippetInstantiationMixesNamedAndWildcard(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	mod := f.Decls[1].(*ast.Container)
	var inst *ast.Instantiation
	for _, d := range mod.Body {
		if i, ok := d.(*ast.Instantiation); ok {
			inst = i
		}
	}
	if inst == nil {
		t.Fatalf("expected to find the leaf instantiation")
	}
	conns := inst.Instances[0].Connections
	if len(conns) != 3 {
		t.Fatalf("expected 3 connections, got %+v", conns)
	}
	if conns[0].Name != "clk" || conns[1].Name != "rst_n" || !conns[2].Wildcard {
		t.Fatalf("unexpected connections: %+v", conns)
	}
}

func TestIntegrationSnippetClassContents(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	cls := f.Decls[2].(*ast.Class)
	if cls.Name != "packet" || cls.Extends != "base_packet" {
		t.Fatalf("unexpected class header: %+v", cls)
	}
	if len(cls.Body) != 5 {
		t.Fatalf("expected 5 members (payload, id, constraint, new, extern display), got %d: %+v", len(cls.Body), cls.Body)
	}
}

func TestIntegrationSnippetClassMemberKinds(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	cls := f.Decls[2].(*ast.Class)
	payload := cls.Body[0].(*ast.Variable)
	if !payload.IsRand || payload.Name != "payload" {
		t.Fatalf("unexpected payload member: %+v", payload)
	}
	id := cls.Body[1].(*ast.Variable)
	if id.IsRand || id.Name != "id" {
		t.Fatalf("unexpected id member: %+v", id)
	}
	constraint := cls.Body[2].(*ast.Constraint)
	if constraint.Name != "c_payload" {
		t.Fatalf("unexpected constraint: %+v", constraint)
	}
	ctor := cls.Body[3].(*ast.Function)
	if ctor.Name != "new" || ctor.Prototype {
		t.Fatalf("unexpected constructor: %+v", ctor)
	}
	extern := cls.Body[4].(*ast.Function)
	if extern.Name != "display" || !extern.Prototype {
		t.Fatalf("unexpected extern method: %+v", extern)
	}
}

func TestIntegrationSnippetOutOfClassDefinitionMatchesExternName(t *testing.T) {
	f, _ := parseIntegrationSnippet(t)
	def := f.Decls[3].(*ast.Function)
	if def.Name != "display" || def.Prototype {
		t.Fatalf("unexpected out-of-class definition: %+v", def)
	}
	cls := f.Decls[2].(*ast.Class)
	extern := cls.Body[4].(*ast.Function)
	if extern.Name != def.Name {
		t.Fatalf("expected the extern prototype and the out-of-class definition to share a name, got %q and %q", extern.Name, def.Name)
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	seeds := []string{
		"", integrationSnippet,
		"module top; endmodule",
		"class foo; endclass",
		"typedef struct { logic a; } t;",
		"function void foo(); endfunction",
		"leaf u_leaf(.clk(clk));",
		"my_type_t x;",
		"my_type_t x(",
		"module module module;",
		"class class class;",
		"function new(",
		"import a::b, c::",
		"leaf #(",
		"rand",
		"static constraint",
		"extern function",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("fuzz.sv", src, nil)
		Parse("fuzz.sv", toks)
	})
}

// BenchmarkPreprocessAndParse covers the whole pipeline on a realistic
// file, which is what sigils runs for every rescan -- i.e. on every
// keystroke in an open buffer, and once per file at startup across a
// worker pool. Nothing here had a benchmark before, so a change to the
// lexer or the skip helpers had no way to be measured rather than assumed.
func BenchmarkPreprocessAndParse(b *testing.B) {
	src := benchRTL(40)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for range b.N {
		toks, ppErrs := preprocessor.Preprocess("bench.sv", src, nil)
		if len(ppErrs) != 0 {
			b.Fatalf("unexpected preprocessor errors: %+v", ppErrs)
		}
		if _, errs := Parse("bench.sv", toks); len(errs) != 0 {
			b.Fatalf("unexpected parse errors: %+v", errs)
		}
	}
}

func benchRTL(modules int) string {
	var b strings.Builder
	for i := range modules {
		fmt.Fprintf(&b, "module m%d #(parameter int W = 8) (\n", i)
		b.WriteString("  input  wire logic [W-1:0] a,\n  input  wire logic [W-1:0] b,\n  output var  logic [W-1:0] y\n);\n")
		for j := range 20 {
			fmt.Fprintf(&b, "  logic [W-1:0] t%d;\n  assign t%d = (a[%d] & b[%d]) | (a >> 1) ^ {b[0], a[1:0]};\n", j, j, j, j)
		}
		b.WriteString("  always_comb begin\n    y = a;\n  end\n")
		fmt.Fprintf(&b, "  leaf #(.W(W)) u%d (.a(a), .b(b), .y(y));\n", i)
		b.WriteString("endmodule\n\n")
	}
	return b.String()
}
