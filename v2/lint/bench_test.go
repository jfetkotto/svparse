package lint

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

// syntheticDesign is a design of many parameterised modules instantiated many
// times, the shape that makes per-instance analysis expensive: nLeaves
// distinct leaf modules (assignments of mixed widths, a generate loop, a
// function, a package struct), grouped ten to a group, each group instantiated
// copies times with different parameter overrides.
func syntheticDesign(nLeaves, copies int) string {
	var b strings.Builder
	b.WriteString(`package p;
  typedef struct packed { logic [7:0] a; logic [15:0] b; } s_t;
  function automatic int widen(int v); return v + 1; endfunction
endpackage
`)
	for i := 0; i < nLeaves; i++ {
		fmt.Fprintf(&b, `module leaf%[1]d #(parameter int W = 8, parameter int D = 4) (
  input  logic clk,
  input  logic [W-1:0] a, b,
  output logic [W:0] y,
  output logic [W-1:0] z
);
  p::s_t st;
  logic [W-1:0] t;
  logic [15:0] wide;
  logic [3:0] narrow;
  assign y = a + b;
  assign wide = a;            // padded
  assign narrow = wide;       // truncated
  assign st.a = t;            // truncated when W > 8
  always_ff @(posedge clk) begin
    t <= a ^ b;
    z <= t + %[2]d;
  end
  for (genvar g = 0; g < D; g++) begin : lane
    logic [W-1:0] r;
    assign r = a >> g;
  end
endmodule
`, i, i%7)
	}
	groups := (nLeaves + 9) / 10
	for g := 0; g < groups; g++ {
		fmt.Fprintf(&b, "module group%d (input logic clk, input logic [15:0] a, b, output logic [16:0] y);\n", g)
		fmt.Fprintf(&b, "  logic [16:0] ys [10];\n")
		for k := 0; k < 10 && g*10+k < nLeaves; k++ {
			w := []int{8, 12, 16}[k%3]
			fmt.Fprintf(&b, "  leaf%d #(.W(%d), .D(%d)) u%d (.clk(clk), .a(a[%d-1:0]), .b(b[%d-1:0]), .y(ys[%d][%d:0]), .z());\n", g*10+k, w, 2+k%3, k, w, w, k, w)
		}
		b.WriteString("  assign y = ys[0];\nendmodule\n")
	}
	b.WriteString("module top (input logic clk, input logic [15:0] a, b, output logic [16:0] y);\n")
	for c := 0; c < copies; c++ {
		for g := 0; g < groups; g++ {
			fmt.Fprintf(&b, "  group%d g%d_%d (.clk(clk), .a(a), .b(b), .y());\n", g, g, c)
		}
	}
	b.WriteString("  assign y = '0;\nendmodule\n")
	return b.String()
}

func buildInput(tb testing.TB, src string) Input {
	tb.Helper()
	res := preprocessor.PreprocessDetailed("synthetic.sv", src, nil, preprocessor.Options{KeepComments: true})
	file, errs := parser.Parse("synthetic.sv", res.Tokens)
	if len(errs) != 0 {
		tb.Fatalf("parse errors: %v", errs[0])
	}
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	design, ds := elab.Elaborate(context.Background(), comp, elab.Options{Tops: []string{"top"}})
	for _, d := range ds {
		if d.Severity == diag.Error {
			tb.Fatalf("elaboration error: %v", d)
		}
	}
	return Input{Comp: comp, Design: design, Comments: res.Comments}
}

func benchRun(b *testing.B, cfg Config) {
	in := buildInput(b, syntheticDesign(200, 4))
	nodes := 0
	in.Design.Walk(func(*elab.Node) bool { nodes++; return true })
	b.ReportMetric(float64(nodes), "nodes")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Run(context.Background(), in, cfg)
	}
}

func BenchmarkRunAllRules(b *testing.B)   { benchRun(b, Config{}) }
func BenchmarkRunWidthRules(b *testing.B) { benchRun(b, only("width-truncation", "width-extension")) }
func BenchmarkRunOverlappingDrivers(b *testing.B) {
	benchRun(b, only("overlapping-drivers"))
}

// The findings on the synthetic design must not change when the analysis is
// made faster: this is the guard for the performance work.
func TestSyntheticDesignFindings(t *testing.T) {
	in := buildInput(t, syntheticDesign(40, 3))
	counts := map[string]int{}
	for _, d := range Run(context.Background(), in, Config{}) {
		counts[d.Code]++
	}
	t.Logf("findings: %v", counts)
	if counts["width-truncation"] == 0 || counts["width-extension"] == 0 {
		t.Fatalf("the synthetic design should trigger both width rules: %v", counts)
	}
}
