package sema

import "testing"

func TestEnumRules(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"valid", `module m; typedef enum {A, B, C} e_t; e_t v; e_t w;
  enum logic [2:0] {X = 3'h2, Y = 3'h3} k;
  enum integer {P = 0, Q = 32'bx, R = 5} z;
  initial begin v = A; w = v; v = e_t'(1); v = (w == A) ? B : C; k = X; end endmodule`, ""},
		{"sized value of the wrong width", "module m; enum logic [2:0] {G = 4'h2} k; endmodule", "invalid-enum-value"},
		{"x in a 2-state enum", "module m; enum bit [1:0] {a = 0, b = 2'bxx} v; endmodule", "invalid-enum-value"},
		{"x in the default int enum", "module m; enum {a = 'x} v; endmodule", "invalid-enum-value"},
		{"value after x", "module m; enum integer {a = 0, b = {32{1'bx}}, c} v; endmodule", "invalid-enum-value"},
		{"integer into an enum", "module m; typedef enum {a, b} e; e v; initial v = 1; endmodule", "cast-required"},
		{"nonblocking too", "module m; typedef enum {a, b} e; e v; initial v <= 1; endmodule", "cast-required"},
		{"another enum into an enum", "module m; typedef enum {a, b} e; typedef enum {c, d} f; e v; initial v = c; endmodule", "cast-required"},
		{"arithmetic assignment", "module m; typedef enum {a, b} e; e v; initial v += 1; endmodule", "cast-required"},
		{"enum into an integer", "module m; typedef enum {a, b} e; e v; int i; initial i = v * 4; endmodule", ""},
	}
	for _, c := range cases {
		if got := codes(checkSrc(t, c.src)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
