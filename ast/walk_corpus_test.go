package ast_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/lexer"
	"github.com/jfetkotto/svparse/parser"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

const corpusDir = "../testdata/sv-tests"

var (
	nodeType  = reflect.TypeOf((*ast.Node)(nil)).Elem()
	tokenType = reflect.TypeOf(preprocessor.Token{})
	posType   = reflect.TypeOf(ast.Position{})
)

// reachableNodes finds every Node in the tree rooted at v by reflection,
// sharing no code with ast.Walk, so a field Walk forgets to descend into
// shows up as a node reflection finds and Walk does not.
//
// A zero-valued ast.Type is left out: Walk deliberately skips it (it records
// the absence of a type). Raw expression spans ([]preprocessor.Token) and the
// embedded Position are not nodes and not descended into.
func reachableNodes(v reflect.Value, out map[ast.Node]bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		reachableNodes(v.Elem(), out)
	case reflect.Interface:
		if !v.IsNil() {
			reachableNodes(v.Elem(), out)
		}
	case reflect.Slice:
		if v.Type().Elem() == tokenType {
			return
		}
		for i := 0; i < v.Len(); i++ {
			reachableNodes(v.Index(i), out)
		}
	case reflect.Struct:
		t := v.Type()
		if t == posType || t == tokenType {
			return
		}
		if v.CanAddr() && reflect.PointerTo(t).Implements(nodeType) {
			if !(t == reflect.TypeOf(ast.Type{}) && v.IsZero()) {
				out[v.Addr().Interface().(ast.Node)] = true
			}
		}
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() {
				reachableNodes(v.Field(i), out)
			}
		}
	}
}

func walkCorpus(t *testing.T, fn func(t *testing.T, rel string, f *ast.File)) {
	t.Helper()
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".sv" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(corpusDir, path)
		t.Run(rel, func(t *testing.T) {
			toks, _ := preprocessor.Preprocess(rel, string(data), nil)
			f, _ := parser.Parse(rel, toks)
			fn(t, rel, f)
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusDir, err)
	}
}

// TestWalkReachesEveryNodeOnce runs over the whole vendored corpus plus
// goldenSource: Walk must visit exactly the nodes reflection finds, each
// exactly once.
func TestWalkReachesEveryNodeOnce(t *testing.T) {
	seenKinds := make(map[string]bool)
	walkCorpus(t, func(t *testing.T, rel string, f *ast.File) { checkWalkComplete(t, f, seenKinds) })
	t.Run("golden-source", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("t.sv", goldenSource, nil)
		f, _ := parser.Parse("t.sv", toks)
		checkWalkComplete(t, f, seenKinds)
	})
	t.Run("inline-aggregates", func(t *testing.T) {
		// Declarators of one statement share a Type value; every node must
		// still be visited exactly once.
		toks, _ := preprocessor.Preprocess("a.sv", aggregateSource, nil)
		f, _ := parser.Parse("a.sv", toks)
		checkWalkComplete(t, f, seenKinds)
		if out := ast.Dump(f); strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
	})
	t.Run("parameterized-classes", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("c.sv", classSource, nil)
		f, _ := parser.Parse("c.sv", toks)
		checkWalkComplete(t, f, seenKinds)
		if out := ast.Dump(f); strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
	})
	t.Run("nets", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("n.sv", netSource, nil)
		f, _ := parser.Parse("n.sv", toks)
		checkWalkComplete(t, f, seenKinds)
		if out := ast.Dump(f); strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
	})
	t.Run("statements", func(t *testing.T) {
		// Block-local declarations of one statement share a Type, and a for loop
		// reads its declarators one by one; every node must still be visited once.
		stmt, errs := parser.ParseStmt(lexStmt(stmtSource))
		if len(errs) != 0 {
			t.Fatalf("stmtSource has errors: %v", errs)
		}
		f := &ast.File{Path: "s.sv", Decls: []ast.Decl{&ast.Initial{Body: stmt}}}
		checkWalkComplete(t, f, seenKinds)
		if out := ast.Dump(f); strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
	})
	t.Run("subroutine-bodies", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("b.sv", subroutineSource, nil)
		f, _ := parser.Parse("b.sv", toks)
		checkWalkComplete(t, f, seenKinds)
		if out := ast.Dump(f); strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
	})
	t.Run("generate", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("g.sv", generateSource, nil)
		f, errs := parser.Parse("g.sv", toks)
		if len(errs) != 0 {
			t.Fatalf("generateSource has errors: %v", errs)
		}
		checkWalkComplete(t, f, seenKinds)
		out := ast.Dump(f)
		if strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
		for _, kind := range []string{"GenerateRegion", "GenerateBlock", "GenerateIf", "GenerateFor", "GenerateCase", "GenerateCaseItem"} {
			if !seenKinds[kind] {
				t.Errorf("no %s node in generateSource", kind)
			}
		}
	})
	t.Run("primitives", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("p.sv", primitivesSource, nil)
		f, errs := parser.Parse("p.sv", toks)
		if len(errs) != 0 {
			t.Fatalf("primitivesSource has errors: %v", errs)
		}
		checkWalkComplete(t, f, seenKinds)
		if out := ast.Dump(f); strings.Contains(out, "UNHANDLED") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
		for _, kind := range []string{"GateInstantiation", "GateInstance", "UDP", "UDPRow"} {
			if !seenKinds[kind] {
				t.Errorf("no %s node in primitivesSource", kind)
			}
		}
	})
	t.Run("all-expressions", func(t *testing.T) {
		checkWalkComplete(t, fileHoldingAllExprs(), seenKinds)
	})

	// Guard against the corpus test going vacuous: these are the node
	// types real source is expected to produce. (PortDecl is absent on
	// purpose: non-ANSI port declarations are merged into Ports and never
	// survive into a Body.)
	for _, kind := range []string{
		"File", "Container", "Class", "Package", "Constraint", "Import", "Variable",
		"Parameter", "Typedef", "TypeAlias", "Struct", "Union", "Enum", "EnumMember",
		"Port", "Modport", "ModportPort", "Arg", "Function", "Task", "Type",
		"Instantiation", "ParamOverride", "Instance", "PortConnection",
		"Nettype", "Block", "Assign", "ConstraintBlock",
	} {
		if !seenKinds[kind] {
			t.Errorf("no %s node appeared anywhere in the corpus", kind)
		}
	}
}

// checkWalkComplete fails unless Walk visits exactly the nodes reflection
// finds in f, each once, and records the node kinds it saw in seenKinds.
func checkWalkComplete(t *testing.T, f *ast.File, seenKinds map[string]bool) {
	t.Helper()
	want := make(map[ast.Node]bool)
	reachableNodes(reflect.ValueOf(f), want)
	want[f] = true

	got := make(map[ast.Node]int)
	ast.Inspect(f, func(n ast.Node) bool {
		if n != nil {
			got[n]++
			seenKinds[reflect.TypeOf(n).Elem().Name()] = true
		}
		return true
	})
	for n := range want {
		if got[n] == 0 {
			t.Errorf("Walk missed %T at %+v", n, n.Pos())
		}
	}
	for n, c := range got {
		if !want[n] {
			t.Errorf("Walk visited %T at %+v that reflection did not find", n, n.Pos())
		}
		if c != 1 {
			t.Errorf("Walk visited %T at %+v %d times", n, n.Pos(), c)
		}
	}
}

// TestDumpCoversEveryNodeInTheCorpus fails if any node type reaches Dump's
// "UNHANDLED" fallback, i.e. a node was added without a Dump case.
func TestDumpCoversEveryNodeInTheCorpus(t *testing.T) {
	walkCorpus(t, func(t *testing.T, rel string, f *ast.File) {
		out := ast.Dump(f)
		if strings.Contains(out, "UNHANDLED") || strings.Contains(out, "container?") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
		if out != ast.Dump(f) {
			t.Error("Dump is not deterministic")
		}
	})
}

// aggregateSource uses anonymous struct, union and enum types in every type
// position, with two declarators sharing one aggregate and one class type.
const aggregateSource = `module m (input struct packed { logic a; } p);
  struct { int x; struct { int y; } inner; } a, b;
  enum logic [1:0] { P, Q = 2'd1 } e1, e2;
  union tagged packed { bit [3:0] v; void none; } u;
  cls #(8) c, d;
  function struct packed { logic ok; } f(input enum { LO, HI } lvl); endfunction
endmodule`

// classSource uses parameterized class references in extends and implements
// clauses and as types, including as the scope of a nested type, with two
// declarators sharing one scoped type.
const classSource = `class ext #(int e = 1) extends pkg::base #(5, .W(2)) (1, x) implements pkg::I, J #(3); endclass
interface class ic extends ia #(T), ib #(T); endclass
module m (input cls #(8) p);
  cls #(8)::inner_t a, b;
  function cls #(4) f(cls #(9)::U u); endfunction
endmodule`

// netSource uses every net-declaration decoration (delay, strength, qualifier),
// user-defined net types, specparams and the type reference operator, with
// two declarators sharing a delay and a type reference.
const netSource = `module m;
  nettype real real_net with real_sum;
  interconnect [3:0] bus;
  specparam delay = 50;
  tri1 scalared [15:0] a = 0;
  wire (strong0, pull1) logic [7:0] #(1, 2:3:4) d1, d2;
  var type(a + 1) c, d;
endmodule`

// stmtSource uses nearly every statement form, with block-local declarations
// that share a type and a for loop with several declarators.
const stmtSource = `begin : main
  int a, b = 2;
  cls #(4) h, k;
  for (int i = 0, j = 9; i < j; i++, j--) a += i;
  foreach (arr[i, j]) a = arr[i][j];
  unique case (a) inside 1, [2:3]: b = 1; default: b = 0; endcase
  if (a) b <= #1 c; else if (b) @(posedge clk) b = 1; else begin end
  fork wait (r) ->> ev; join_any
  assert (a) else $error("x");
  randcase 1: a = 1; endcase
  do a++; while (a < 3);
  repeat (2) @* disable fork;
  force a = 1; release a;
  forever #5 clk = ~clk;
  if (v matches .x) y = 1;
end : main`

func lexStmt(src string) []preprocessor.Token {
	raw, _ := lexer.Lex(src)
	var toks []preprocessor.Token
	for _, t := range raw {
		if t.Kind != token.KindEOF {
			toks = append(toks, preprocessor.Token{Token: t, File: "s.sv"})
		}
	}
	return toks
}

// subroutineSource has function and task bodies with locals that share a type,
// arguments declared in the body, a constraint block, and a multi-type for loop.
const subroutineSource = `module m;
  function int f(input int x);
    cls #(4) a, b;
    for (int i = 0, bit c = 1; i < 3; i++) x += i;
    if (!obj.randomize() with (v) { v > 0; }) return 0;
    return x;
  endfunction
  function int old_style;
    input int p, q;
    old_style = p + q;
  endfunction
  task t(input int n);
    repeat (n) @(posedge clk);
    q.find_first with (item > 1);
  endtask
endmodule`

// generateSource has every generate construct, nested, with bare branches.
const generateSource = `module m #(int W = 8);
  generate
    for (genvar i = 0; i < W; i++) begin : g
      if (i == 0) assign a = 1; else if (i > 3) begin : hi
        leaf u();
      end else wire w;
    end
    case (W)
      8, 16: begin end
      default: ;
    endcase
  endgenerate
endmodule`

// primitivesSource has gates with strength, delay, names and arrays, and both
// a combinational and a sequential user-defined primitive.
const primitivesSource = `primitive mux (out, sel, a, b);
  output out;
  input sel, a, b;
  table
    0 0 ? : 0;
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
  pullup (p);
endmodule`

// goldenSource touches most declaration kinds, including ones the vendored
// corpus never produces (modports).
const goldenSource = `package pkg;
  typedef struct packed { logic [7:0] a; bit b; } s_t;
  typedef enum logic [1:0] { IDLE, RUN = 2'd1 } st_t;
endpackage
interface bus_if #(parameter int W = 8) (input logic clk);
  logic [W-1:0] data;
  modport mp (input data, input clk);
endinterface
module top #(parameter N = 4, localparam M = N * 2) (input logic clk, output pkg::s_t out [3:0]);
  import pkg::*;
  bus_if #(.W(16)) u_bus (.clk(clk));
  leaf #(8) u0 (.*), u1 (.a, .b(x + 1));
  function automatic int f(input int x, y = 3);
    return x;
  endfunction
endmodule
class base #(type T = int) extends root implements ifc;
  rand int unsigned v;
  static string name;
  extern constraint c;
endclass
`

// TestDumpGolden pins the dump format end to end on source touching most
// declaration kinds.
func TestDumpGolden(t *testing.T) {
	const want = `(file "t.sv"
  (package pkg
    (typedef s_t
      (struct packed
        (variable a <logic [7:0]>)
        (variable b <bit>)))
    (typedef st_t
      (enum <logic [1:0]>
        (enum-member IDLE)
        (enum-member RUN = 2'd1))))
  (interface bus_if
    (parameter W <int> = 8)
    (port input clk <logic>)
    (variable data <logic [(- W 1):0]>)
    (modport mp
      (modport-port input data)
      (modport-port input clk)))
  (module top
    (parameter N = 4)
    (localparam M = (* N 2))
    (port input clk <logic>)
    (port output out <pkg::s_t> [3:0])
    (import pkg::*)
    (instantiation bus_if
      (param-override .W 16)
      (instance u_bus
        (connection .clk clk)))
    (instantiation leaf
      (param-override 8)
      (instance u0
        (connection .*))
      (instance u1
        (connection .a implicit)
        (connection .b (+ x 1))))
    (function f <int>
      (arg input x <int>)
      (arg y = 3)
      (return x)))
  (class base extends=root implements=ifc
    (parameter T <type> = <int>)
    (variable v <int unsigned> rand)
    (variable name <string> static)
    (constraint c prototype)))
`
	toks, ppErrs := preprocessor.Preprocess("t.sv", goldenSource, nil)
	f, parseErrs := parser.Parse("t.sv", toks)
	if len(ppErrs)+len(parseErrs) != 0 {
		t.Fatalf("sample should parse cleanly: pp %v, parse %v", ppErrs, parseErrs)
	}
	if got := ast.Dump(f); got != want {
		t.Errorf("Dump mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
