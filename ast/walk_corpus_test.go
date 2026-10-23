package ast_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/parser"
	"github.com/jfetkotto/svparse/preprocessor"
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
	check := func(t *testing.T, f *ast.File) {
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
	walkCorpus(t, func(t *testing.T, rel string, f *ast.File) { check(t, f) })
	t.Run("golden-source", func(t *testing.T) {
		toks, _ := preprocessor.Preprocess("t.sv", goldenSource, nil)
		f, _ := parser.Parse("t.sv", toks)
		check(t, f)
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
	} {
		if !seenKinds[kind] {
			t.Errorf("no %s node appeared anywhere in the corpus", kind)
		}
	}
}

// TestDumpCoversEveryNodeInTheCorpus fails if any node type reaches Dump's
// "?" fallback, i.e. a node was added without a Dump case.
func TestDumpCoversEveryNodeInTheCorpus(t *testing.T) {
	walkCorpus(t, func(t *testing.T, rel string, f *ast.File) {
		out := ast.Dump(f)
		if strings.Contains(out, "(?") || strings.Contains(out, "container?") {
			t.Errorf("Dump hit its fallback:\n%s", out)
		}
		if out != ast.Dump(f) {
			t.Error("Dump is not deterministic")
		}
	})
}

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
        (enum-member RUN = "2'd1"))))
  (interface bus_if
    (parameter W <int> = "8")
    (port input clk <logic>)
    (variable data <logic [W - 1:0]>)
    (modport mp
      (modport-port input data)
      (modport-port input clk)))
  (module top
    (parameter N = "4")
    (localparam M = "N * 2")
    (port input clk <logic>)
    (port output out <pkg::s_t> [3:0])
    (import pkg::*)
    (instantiation bus_if
      (param-override .W "16")
      (instance u_bus
        (connection .clk "clk")))
    (instantiation leaf
      (param-override "8")
      (instance u0
        (connection .*))
      (instance u1
        (connection .a implicit)
        (connection .b "x + 1")))
    (function f <int>
      (arg input x <int>)
      (arg y = "3")))
  (class base extends=root implements=ifc
    (parameter T <type> = "int")
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
