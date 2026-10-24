package sema

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
)

func checkSrc(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	c := NewCompilation(Options{})
	c.AddFile(parseFile(t, "t.sv", src))
	return c.Model().CheckStatements()
}

func codes(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return strings.Join(out, ",")
}

func TestStatementRules(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"clean", `module m (input clk, input [3:0] d, output logic [3:0] q, output wire w);
  logic [3:0] r; wire [3:0] n;
  assign n = d; assign w = r[0];
  always_ff @(posedge clk) q <= d;
  always_comb r = d + 1;
  initial begin r = 0; #5 r = 1; end
  function automatic int f(int a); int t; t = a * 2; f = t; return t; endfunction
  task t1(output int o); o = 1; endtask
endmodule`, ""},
		{"assign to input", "module m (input a, input logic b); initial begin a = 1; b = 0; end endmodule", "assign-to-input,assign-to-input"},
		{"assign to input from assign", "module m (input a); wire x; assign a = 1; endmodule", "assign-to-input"},
		{"procedural net", "module m; wire w; reg r; initial begin w = 1; r = 1; w <= 0; end endmodule", "procedural-assign-to-net,procedural-assign-to-net"},
		{"procedural to implicit-net output", "module m (output q); initial q = 1; endmodule", "procedural-assign-to-net"},
		{"variable output ok", "module m (output reg q, output logic r); initial begin q = 1; r = 1; end endmodule", ""},
		{"non-ansi output reg ok", "module m (q); output q; reg q; initial q = 1; endmodule", ""},
		{"inout is a net", "module m (inout p); initial p = 1; endmodule", "procedural-assign-to-net"},
		{"constants", "module m #(parameter P = 1); localparam L = 2; typedef enum {A, B} e_t; genvar g; initial begin P = 1; L = 2; A = B; g = 0; end endmodule",
			"assign-to-constant,assign-to-constant,assign-to-constant,assign-to-constant"},
		{"not an lvalue", "module m; int a, b; initial begin (a + b) = 1; 3 = a; a++; $time = 1; end endmodule", "invalid-lvalue,invalid-lvalue,invalid-lvalue"},
		{"increment of a constant", "module m; localparam L = 1; initial L++; endmodule", "assign-to-constant"},
		{"assignment expression", "module m; int a, b; localparam L = 1; initial a = (b = 2) + (L += 1); endmodule", "assign-to-constant"},
		{"concat targets", "module m (input i); logic a; wire w; initial {a, i} = 2'b11; endmodule", "assign-to-input"},
		{"selects keep the root", "module m (input [3:0] i); logic [3:0] a; initial begin a[1] = 0; i[2] = 0; end endmodule", "assign-to-input"},
		{"hierarchical targets are not checked here", "module m; sub u(); initial u.x = 1; endmodule module sub; int x; endmodule", ""},
		{"force and release reach nets", "module m; wire w; reg r; initial begin force w = 1; release w; assign r = 1; deassign r; end endmodule", ""},
		{"function return value", `module m;
  function int f(); return 1; endfunction
  function int g(); return; endfunction
  function void v(); return 1; endfunction
  function void ok(); return; endfunction
  task t(); return 1; endtask
  task t2(); return; endtask
endmodule`, "missing-return-value,return-value-in-void,return-value-in-void"},
		{"constructor returns nothing", "class C; function new(); return 1; endfunction function new(int a); return; endfunction endclass", "return-value-in-void"},
		{"return outside a subroutine", "module m; initial return; endmodule", "return-outside-subroutine"},
		{"break and continue", `module m;
  initial begin
    for (int i = 0; i < 3; i++) begin if (i) break; else continue; end
    foreach (a[i]) break;
    while (1) begin break; end
    do continue; while (0);
    repeat (2) break;
    forever break;
    break;
    continue;
    fork break; join
  end
  int a[2];
  function void f(); break; endfunction
endmodule`, "break-outside-loop,continue-outside-loop,break-outside-loop,break-outside-loop"},
		{"timing in a function", `module m;
  function void f(); #5; @(posedge clk); wait (a); x = #1 y; endfunction
  task t(); #5; @(posedge clk); endtask
  int x, y, a; logic clk;
endmodule`, "timing-in-function,timing-in-function,timing-in-function,timing-in-function"},
		{"process names", "module m; function void f(); endfunction int v; initial f = 1; endmodule", "invalid-lvalue"},
		{"function result variable", "module m; function int f(); f = 1; endfunction endmodule", ""},
		{"generate blocks", `module m;
  genvar i; wire [3:0] w; logic [3:0] v;
  generate for (i = 0; i < 4; i++) begin : g
    assign w[i] = 1;
    always_comb begin v[i] = 0; i = 1; end
  end endgenerate
endmodule`, "assign-to-constant"},
		{"class methods", "class C; int x; function void f(); x = 1; return 1; endfunction endclass", "return-value-in-void"},
		{"out-of-block method", "class C; extern function int get(); extern function void put(); endclass function int C::get(); return; endfunction function void C::put(); return 1; endfunction", "missing-return-value,return-value-in-void"},
		{"assertion action", "module m; wire w; logic r; assert property (@(posedge clk) a) else r = 0; endmodule", ""},
	}
	for _, c := range cases {
		got := codes(checkSrc(t, c.src))
		if got != c.want {
			t.Errorf("%s: got [%s], want [%s]\n%v", c.name, got, c.want, checkSrc(t, c.src))
		}
	}
}

func TestDriverRules(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"two continuous drivers of a variable", "module m; logic v; assign v = 1; assign v = 0; endmodule", "multiple-drivers"},
		{"nets may have many", "module m; wire w; assign w = 1; assign w = 0; endmodule", ""},
		{"continuous and procedural", "module m; logic v; assign v = 1; initial v = 0; endmodule", "multiple-drivers"},
		{"continuous and always", "module m; logic v, c; assign v = 1; always @(c) v = 0; endmodule", "multiple-drivers"},
		{"one assign driving two vars", "module m; logic a, b; assign a = 1, b = 0; endmodule", ""},
		{"always_ff twice", "module m; logic q, c; always_ff @(posedge c) q <= 1; always_ff @(negedge c) q <= 0; endmodule", "multiple-drivers"},
		{"always_ff and always", "module m; logic q, c; always_ff @(posedge c) q <= 1; always @(c) q = 0; endmodule", "multiple-drivers"},
		{"always_comb and always_comb", "module m; logic q; always_comb q = 1; always_comb q = 0; endmodule", "multiple-drivers"},
		{"same process writes freely", "module m; logic q, c; always_ff @(posedge c) begin q <= 1; q <= 0; end endmodule", ""},
		{"initial may set an always_ff variable", "module m; logic q, c; initial q = 0; always_ff @(posedge c) q <= 1; endmodule", ""},
		{"initial may not set a continuous one", "module m; logic q; initial q = 0; assign q = 1; endmodule", "multiple-drivers"},
		{"plain always blocks may share", "module m; logic q, c, d; always @(c) q = 1; always @(d) q = 0; endmodule", ""},
		{"elements are separate", "module m; logic [3:0] v; assign v[0] = 1; assign v[1] = 0; always_comb v[2] = 0; endmodule", ""},
		{"whole against a part", "module m; logic [3:0] v; assign v = 0; assign v[1] = 1; endmodule", "multiple-drivers"},
		{"locals are private", "module m; initial begin int t; t = 1; end initial begin int t; t = 2; end endmodule", ""},
		{"class members are not tracked", "class C; int x; function void a(); x = 1; endfunction function void b(); x = 2; endfunction endclass", ""},
		{"functions are not attributed", "module m; logic q; always_comb q = 1; function void f(); q = 0; endfunction endmodule", ""},
		{"generate loop drivers", "module m; genvar i; logic [3:0] v; generate for (i = 0; i < 4; i++) begin : g assign v[i] = 1; end endgenerate endmodule", ""},
		{"output port driven twice", "module m (output logic q); assign q = 1; always_comb q = 0; endmodule", "multiple-drivers"},
	}
	for _, c := range cases {
		ds := checkSrc(t, c.src)
		if got := codes(ds); got != c.want {
			t.Errorf("%s: got [%s], want [%s]\n%v", c.name, got, c.want, ds)
		}
		for _, d := range ds {
			if d.Code == diag.CodeMultipleDrivers && len(d.Related) != 1 {
				t.Errorf("%s: a driver conflict should point at the other driver: %+v", c.name, d)
			}
		}
	}
	// The conflict is reported at the later of the two drivers.
	ds := checkSrc(t, "module m;\n  logic v;\n  assign v = 1;\n  assign v = 0;\nendmodule")
	if len(ds) != 1 || ds[0].Range.Line != 3 || ds[0].Related[0].Range.Line != 2 {
		t.Errorf("report position: %v", ds)
	}
}

func TestStatementChecksAreDeterministic(t *testing.T) {
	src := "module m; logic a, b, c; assign a = 1; assign a = 0; assign b = 1; assign b = 0; always_comb c = 1; always_comb c = 0; endmodule"
	first := codes(checkSrc(t, src))
	for i := 0; i < 20; i++ {
		if got := codes(checkSrc(t, src)); got != first {
			t.Fatalf("run %d: %s vs %s", i, got, first)
		}
	}
	if strings.Count(first, "multiple-drivers") != 3 {
		t.Errorf("got %s", first)
	}
}

// Over the vendored files the checks run to completion; the diagnostics they
// raise there belong to deliberately illegal tests or to code the model does
// not cover, so each kind is bounded.
func TestCorpusStatementChecks(t *testing.T) {
	counts := map[string]int{}
	var examples = map[string]string{}
	err := filepath.WalkDir("../testdata/sv-tests", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		toks, _ := preprocessor.Preprocess(path, string(data), nil)
		f, _ := parser.Parse(path, toks)
		c := NewCompilation(Options{})
		c.AddFile(f)
		for _, d := range c.Model().CheckStatements() {
			counts[d.Code]++
			if examples[d.Code] == "" {
				examples[d.Code] = strings.TrimPrefix(d.String(), "../testdata/sv-tests/")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%-28s %4d  e.g. %s", k, counts[k], examples[k])
	}
}

func FuzzCheckStatements(f *testing.F) {
	f.Add("module m (input a); logic v; wire w; assign v = a; always_comb v = 0; initial begin w = 1; a = 2; break; end function int f(); #1; return; endfunction endmodule")
	f.Add("class C; function void f(); return 1; endfunction endclass module m; genvar g; initial g = 1; endmodule")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		file, _ := parser.Parse("f.sv", toks)
		c := NewCompilation(Options{})
		c.AddFile(file)
		c.Model().CheckStatements()
	})
}
