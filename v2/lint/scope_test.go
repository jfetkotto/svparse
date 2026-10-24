package lint

import (
	"context"
	"sort"
	"testing"

	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

// A run limited to some files must find exactly what a full run finds in
// them: that is what lets an editor re-lint only the file just changed.
func TestScopedRunMatchesTheFullRunFileByFile(t *testing.T) {
	files := map[string]string{
		"leaf.sv": `module leaf #(parameter int W = 8) (input logic clk, input logic [W-1:0] a, input logic spare_in, output logic [W-1:0] q, output logic dbg);
  logic [3:0] narrow;
  logic unused_sig;
  logic floating;
  assign narrow = a;
  assign dbg = floating;
  always_ff @(posedge clk) q = a;
endmodule
`,
		"mid.sv": `module mid (input logic clk, input logic [15:0] a, output logic [15:0] q);
  logic [15:0] t;
  leaf #(.W(16)) u0 (.clk(clk), .a(a), .q(t));
  leaf #(.W(4)) u1 (.clk(clk), .a(a[3:0]), .spare_in(1'b0), .q(), .dbg());
  assign q = t;
  assign t = a;
endmodule
`,
		"top.sv": `module top (input logic clk, input logic [15:0] a, output logic [15:0] q);
  mid m0 (.clk(clk), .a(a), .q(q));
  mid m1 (.clk(clk), .a(a));
  for (genvar i = 0; i < 2; i++) begin : g
    logic [7:0] x;
    assign x = a;
  end
endmodule
`,
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	comp := sema.NewCompilation(sema.Options{})
	var comments []preprocessor.Comment
	for _, p := range paths {
		res := preprocessor.PreprocessDetailed(p, files[p], nil, preprocessor.Options{KeepComments: true})
		f, errs := parser.Parse(p, res.Tokens)
		if len(errs) != 0 {
			t.Fatalf("%s: %v", p, errs)
		}
		comp.AddFile(f)
		comments = append(comments, res.Comments...)
	}
	design, _ := elab.Elaborate(context.Background(), comp, elab.Options{Tops: []string{"top"}})
	in := Input{Comp: comp, Design: design, Comments: comments}

	key := func(d diag.Diagnostic) string { return d.String() }
	full := Run(context.Background(), in, Config{})
	byFile := map[string][]string{}
	for _, d := range full {
		byFile[d.Range.File] = append(byFile[d.Range.File], key(d))
	}
	if len(full) < 8 {
		t.Fatalf("the design should have plenty of findings, got %d: %v", len(full), full)
	}
	for _, p := range paths {
		scoped := in
		scoped.Files = map[string]bool{p: true}
		var got []string
		for _, d := range Run(context.Background(), scoped, Config{}) {
			got = append(got, key(d))
		}
		want := byFile[p]
		sort.Strings(got)
		sort.Strings(want)
		if len(got) != len(want) {
			t.Errorf("%s: scoped run found %d, full run %d in the file\n got %v\nwant %v", p, len(got), len(want), got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: scoped %q, full %q", p, got[i], want[i])
			}
		}
	}
}

func TestSemanticChecksCanBeLimitedToFiles(t *testing.T) {
	comp := sema.NewCompilation(sema.Options{})
	for p, src := range map[string]string{
		"a.sv": "module a(output logic y); assign y = missing_a; endmodule\n",
		"b.sv": "module b(output logic y); assign y = missing_b; endmodule\n",
	} {
		res := preprocessor.PreprocessDetailed(p, src, nil, preprocessor.Options{})
		f, _ := parser.Parse(p, res.Tokens)
		comp.AddFile(f)
	}
	m := comp.Model()
	if got := len(m.CheckNames()); got != 2 {
		t.Fatalf("all files: %d", got)
	}
	ds := m.CheckNamesIn(map[string]bool{"a.sv": true})
	if len(ds) != 1 || ds[0].Range.File != "a.sv" {
		t.Fatalf("limited to a.sv: %v", ds)
	}
	if len(m.CheckStatementsIn(map[string]bool{"b.sv": true})) != 0 {
		t.Fatal("statement checks of a clean file")
	}
}
