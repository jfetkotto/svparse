package lint

import (
	"sort"
	"strings"
	"testing"
)

// findings returns the messages of one rule over src, sorted.
func findings(t *testing.T, rule, src string) []string {
	t.Helper()
	var out []string
	for _, d := range analyze(t, "t.sv", src, only(rule)) {
		out = append(out, d.Message)
	}
	sort.Strings(out)
	return out
}

func TestLatchAnalysis(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string // signals reported
	}{
		{"complete if/else", "if (e) y = 1; else y = 0;", nil},
		{"default first", "y = 0; if (e) y = 1;", nil},
		{"missing else", "if (e) y = 1;", []string{"y"}},
		{"else if chain without else", "if (e) y = 1; else if (f) y = 2;", []string{"y"}},
		{"else if chain with else", "if (e) y = 1; else if (f) y = 2; else y = 3;", nil},
		{"case without default", "case (s) 0: y = 1; 1: y = 2; endcase", []string{"y"}},
		{"case with default", "case (s) 0: y = 1; default: y = 2; endcase", nil},
		{"unique case", "unique case (s) 0: y = 1; 1: y = 2; endcase", nil},
		{"one branch misses a signal", "case (s) 0: begin y = 1; z = 1; end default: y = 2; endcase", []string{"z"}},
		{"nested incomplete", "if (e) begin if (f) y = 1; else y = 2; end", []string{"y"}},
		{"nested complete", "if (e) begin if (f) y = 1; else y = 2; end else y = 3;", nil},
		{"assigned after", "if (e) y = 1; y = 0;", nil},
		{"for loop is assumed to run", "for (int i = 0; i < 4; i++) y[i] = 1;", nil},
		{"while loop is not", "while (c) y = 1;", []string{"y"}},
		{"local temporary is ignored", "logic t; if (e) t = 1;", nil},
		{"part select counts as the signal", "y[0] = 1;", nil},
		{"two signals", "if (e) begin y = 1; z = 1; end", []string{"y", "z"}},
		{"concatenation target", "if (e) {y, z} = 2'b11;", []string{"y", "z"}},
	}
	for _, c := range cases {
		src := "module top(input e, f, c, input [1:0] s, output logic [3:0] y, output logic z);\n always_comb begin " + c.body + " end\nendmodule"
		var got []string
		for _, m := range findings(t, "latch-inferred", src) {
			// "\"y\" is not assigned ..."
			got = append(got, strings.Split(m, "\"")[1])
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: reported %v, want %v", c.name, got, c.want)
		}
	}
	// always_latch is meant to latch; a sequential block is not combinational.
	for _, src := range []string{
		"module top(input e, output logic y); always_latch if (e) y = 1; endmodule",
		"module top(input e, clk, output logic y); always_ff @(posedge clk) if (e) y <= 1; endmodule",
		"module top(input e, output logic y); always #5 if (e) y = 1; endmodule",
	} {
		if got := findings(t, "latch-inferred", src); len(got) != 0 {
			t.Errorf("%s: %v", src, got)
		}
	}
}

func TestProcessClassification(t *testing.T) {
	cases := []struct {
		always string
		kind   procKind
		list   bool
	}{
		{"always_ff @(posedge c) x <= 1;", procSequential, false},
		{"always_comb x = 1;", procCombinational, false},
		{"always_latch if (c) x = 1;", procLatch, false},
		{"always @(posedge c) x <= 1;", procSequential, false},
		{"always @(posedge c or negedge r) x <= 1;", procSequential, false},
		{"always @(a or b) x = 1;", procCombinational, true},
		{"always @(a, b) x = 1;", procCombinational, true},
		{"always @* x = 1;", procCombinational, false},
		{"always @(*) x = 1;", procCombinational, false},
		{"always #5 x = ~x;", procOther, false},
		{"always x = 1;", procOther, false},
	}
	for _, c := range cases {
		src := "module top; logic x, a, b, c, r; " + c.always + " endmodule"
		var got *process
		res := analyzeAST(t, src)
		eachProcess(res, func(p *process) { got = p })
		if got == nil || got.kind != c.kind || got.explicitList != c.list {
			t.Errorf("%s: %+v, want kind %v list %v", c.always, got, c.kind, c.list)
		}
	}
}

func TestBlockingAndNonblockingDetails(t *testing.T) {
	// A task or function called from a clocked block is not part of the block,
	// and initial blocks are free to use either.
	src := `module top(input clk, input d, output logic q);
  function void f(); q = 1; endfunction
  always_ff @(posedge clk) begin q <= d; f(); end
  initial begin q = 0; q <= 1; end
  always_comb begin : blk q <= d; end
endmodule`
	if got := findings(t, "blocking-in-sequential", src); len(got) != 0 {
		t.Errorf("blocking: %v", got)
	}
	if got := findings(t, "nonblocking-in-combinational", src); len(got) != 1 {
		t.Errorf("nonblocking: %v", got)
	}
}

func TestSensitivityDetails(t *testing.T) {
	// Signals the block writes, locals and constants are not required in the
	// list; a signal read only through an index or a field still is.
	src := `module top(input [3:0] a, b, idx, output logic [3:0] x);
  typedef struct packed { logic [3:0] f; } s_t;
  s_t st;
  localparam K = 1;
  always @(a) begin x = a; x = x + 1; end
  always @(a) x = a[idx[0]];
  always @(a) x = st.f;
  always @(a) x = a + K;
  always @(a or idx) begin logic [3:0] t; t = a; x = t + b; end
endmodule`
	got := findings(t, "incomplete-sensitivity", src)
	want := []string{`"b" is read but is not in the sensitivity list`, `"idx" is read but is not in the sensitivity list`, `"st" is read but is not in the sensitivity list`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
