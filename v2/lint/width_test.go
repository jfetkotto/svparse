package lint

import (
	"strings"
	"testing"
)

func TestWidthTable(t *testing.T) {
	cases := []struct {
		name, assign string
		trunc, ext   bool
	}{
		{"same width", "q8 = a8;", false, false},
		{"wider source", "q8 = a16;", true, false},
		{"narrower source", "q16 = a8;", false, true},
		{"literal that fits", "q8 = 200;", false, false},
		{"literal that does not", "q8 = 300;", true, false},
		{"sized literal that fits", "q8 = 32'd255;", false, false},
		{"unbased fill", "q8 = '1;", false, false},
		{"negative literal", "q8 = -1;", false, false},
		{"counter", "q8 = q8 + 1;", false, false},
		{"sum with a wide variable", "q8 = q8 + i32;", true, false},
		{"mask bounds the result", "q8 = a16 & 16'h00FF;", false, false}, // 16 bits declared, 8 effective
		{"narrow mask", "q8 = a16 & 8'hFF;", false, false},
		{"right shift by a constant", "q8 = a16 >> 8;", false, false},
		{"right shift too small", "q8 = a16 >> 4;", true, false},
		{"left shift is not bounded", "q8 = a8 << 4;", false, false},
		{"concat is declared width", "q8 = {a8, 1'b0};", true, false},
		{"concat fits", "q16 = {a8, a8};", false, false},
		{"part select", "q8 = a16[7:0];", false, false},
		{"conditional takes the wider", "q8 = sel ? a8 : a16;", true, false},
		{"parameter value fits", "q8 = P;", false, false},
		{"parameter value too big", "q4 = BIG;", true, false},
		{"constant function", "q4 = $clog2(256);", false, false},
		{"reduction is one bit", "q8 = &a16;", false, true},
		{"comparison is one bit", "q8 = (a16 == 0);", false, true},
		{"carry idiom", "{c, q8} = a8 + a8;", false, false},
		{"product widening", "q16 = a8 * a8;", false, false},
		{"compound assignment is skipped", "q8 += a16;", false, false},
		{"real is skipped", "r = a16;", false, false},
		{"cast to the target width", "q8 = 8'(a16);", false, false},
		{"signed cast", "q8 = $signed(a16);", true, false},
	}
	for _, c := range cases {
		src := `module top #(parameter P = 100, parameter BIG = 40) (input sel, input [7:0] a8, input [15:0] a16, input int i32,
  output logic [7:0] q8, output logic [15:0] q16, output logic [3:0] q4, output logic c, output real r);
  initial begin ` + c.assign + ` end
endmodule`
		gotT := len(findings(t, "width-truncation", src)) > 0
		gotE := len(findings(t, "width-extension", src)) > 0
		if gotT != c.trunc || gotE != c.ext {
			t.Errorf("%s (%s): truncation=%v extension=%v, want %v %v", c.name, c.assign, gotT, gotE, c.trunc, c.ext)
		}
	}
}

func TestWidthFollowsTheInstancesParameters(t *testing.T) {
	// The same assignment truncates in one instance and not the other, so it is
	// reported once, with the instance that shows it.
	src := `module top;
  logic [15:0] big; logic [7:0] lo;
  leaf #(.W(8)) a(.d(big), .q(lo));
  leaf #(.W(16)) b(.d(big), .q(lo));
endmodule
module leaf #(parameter W = 8) (input [15:0] d, output logic [7:0] q);
  logic [W-1:0] tmp;
  always_comb begin tmp = d; q = tmp[7:0]; end
endmodule`
	got := findings(t, "width-truncation", src)
	if len(got) != 1 || !strings.Contains(got[0], "16-bit value assigned to a 8-bit target") {
		t.Errorf("got %v", got)
	}
}

func TestWidthInGenerateBlocksAndInitializers(t *testing.T) {
	src := `module top #(parameter N = 2) (input [15:0] wide, output logic [7:0] o);
  logic [3:0] init = wide;
  for (genvar i = 0; i < N; i++) begin : g
    logic [7:0] v;
    assign v = wide;
    assign o = v;
  end
endmodule`
	got := findings(t, "width-truncation", src)
	if len(got) != 2 {
		t.Errorf("want the initializer and the generate assign, got %v", got)
	}
}

func TestWidthNeedsAnElaboratedDesign(t *testing.T) {
	ctx := analyzeAST(t, "module top(input [15:0] a, output [7:0] q); assign q = a; endmodule")
	checkWidths(ctx, true) // no Design: nothing to do, and no panic
}
