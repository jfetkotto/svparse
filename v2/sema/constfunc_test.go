package sema

import (
	"testing"

	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/svint"
)

const constFuncs = `
module top #(parameter W = 5);
  function automatic int factorial(int n);
    if (n <= 1) return 1;
    return n * factorial(n - 1);
  endfunction
  function automatic int fib(int n);
    int a, b, t;
    a = 0; b = 1;
    for (int i = 0; i < n; i++) begin t = a + b; a = b; b = t; end
    return a;
  endfunction
  function int gcd(int a, int b);
    while (b != 0) begin int t; t = b; b = a % b; a = t; end
    return a;
  endfunction
  function automatic int clog(int v);
    int r;
    for (r = 0; (1 << r) < v; r = r + 1) ;
    clog = r;
  endfunction
  function automatic logic [7:0] reverse(logic [7:0] v);
    logic [7:0] r;
    for (int i = 0; i < 8; i++) r[7 - i] = v[i];
    return r;
  endfunction
  function automatic int popcount(logic [15:0] v);
    int n = 0;
    foreach_unavailable: for (int i = 0; i < 16; i++) if (v[i]) n++;
    return n;
  endfunction
  function int classify(int x);
    case (x)
      0: return 100;
      1, 2: return 200;
      default: return 300;
    endcase
  endfunction
  function int firstmul(int k);
    for (int i = 1; ; i++) begin
      if (i % k != 0) continue;
      return i;
    end
  endfunction
  function int sumto(int n);
    int s = 0, i = 0;
    do begin i++; if (i > 100) break; s += i; end while (i < n);
    return s;
  endfunction
  function int rep(int n);
    int s = 0;
    repeat (n) s += 3;
    return s;
  endfunction
  function int forever_break();
    int c = 0;
    forever begin c++; if (c == 7) break; end
    return c;
  endfunction
  function int defaults(int a, int b = 10, int c = 100);
    return a + b + c;
  endfunction
  function automatic int twice(int x); return x * 2; endfunction
  function automatic int nested(int x); return twice(twice(x)) + factorial(3); endfunction
  function real half(real x); return x / 2.0; endfunction
  function logic [3:0] setbits(logic [3:0] v);
    v[1:0] = 2'b11; v[3] = 1'b1;
    return v;
  endfunction
  function logic [7:0] partplus();
    logic [7:0] r = 8'h00;
    r[2 +: 3] = 3'b111;
    r[7 -: 2] = 2'b10;
    return r;
  endfunction
  function logic [7:0] oob();
    logic [7:0] r = 8'h00;
    r[9] = 1'b1; r[-1] = 1'b1; r[10:9] = 2'b11;
    return r;
  endfunction
  function int uninit(); int x; return x; endfunction
  function logic [3:0] uninit4(); logic [3:0] x; return x; endfunction
  function void noop(); endfunction
  function int ops();
    int x = 10;
    x += 5; x -= 3; x *= 2; x /= 4; x <<= 2; x |= 1; x ^= 3;
    return x;
  endfunction
  function byte narrow(int v); return v; endfunction
  function int usesparam(); return W * 2; endfunction

  localparam F5 = factorial(5);
  localparam [clog(33)-1:0] VEC = '1;
  logic [reverse(8'h01)-1:0] sized;
endmodule`

func TestConstantFunctions(t *testing.T) {
	m, s := ctxScope(t, "")
	_ = s
	m2, c := model(t, constFuncs)
	top := defScope(t, m2, c, "top")
	_ = m
	cases := []struct{ expr, want string }{
		{"factorial(5)", "32'sh00000078"},
		{"factorial(0)", "32'sh00000001"},
		{"fib(10)", "32'sh00000037"},
		{"gcd(48, 18)", "32'sh00000006"},
		{"clog(33)", "32'sh00000006"},
		{"clog(1)", "32'sh00000000"},
		{"reverse(8'h01)", "8'h80"},
		{"reverse(8'hA5)", "8'ha5"},
		{"reverse(8'h0F)", "8'hf0"},
		{"popcount(16'hF0F0)", "32'sh00000008"},
		{"classify(0)", "32'sh00000064"},
		{"classify(2)", "32'sh000000c8"},
		{"classify(9)", "32'sh0000012c"},
		{"firstmul(7)", "32'sh00000007"},
		{"sumto(10)", "32'sh00000037"},
		{"sumto(1000)", "32'sh000013ba"},
		{"rep(4)", "32'sh0000000c"},
		{"forever_break()", "32'sh00000007"},
		{"defaults(1)", "32'sh0000006f"},
		{"defaults(1, 2)", "32'sh00000067"},
		{"defaults(1, .c(5))", "32'sh00000010"},
		{"defaults(.a(1), .b(2), .c(3))", "32'sh00000006"},
		{"nested(3)", "32'sh00000012"},
		{"setbits(4'b0000)", "4'hb"},
		{"partplus()", "8'h9c"},
		{"oob()", "8'h00"},
		{"uninit()", "32'sh00000000"},
		{"uninit4()", "4'bxxxx"},
		{"ops()", "32'sh0000001a"},
		{"narrow(300)", "8'sh2c"},
		{"usesparam()", "32'sh0000000a"},
		{"F5", "32'sh00000078"},
		{"$bits(VEC)", "32'sh00000006"},
		{"$bits(sized)", "32'sh00000080"},
		{"factorial(3) + fib(6)", "32'sh0000000e"},
	}
	for _, tc := range cases {
		got, ds := m2.Eval(top, parseExprText(t, tc.expr), nil)
		if len(ds) != 0 {
			t.Errorf("%s: diagnostics %v", tc.expr, ds)
			continue
		}
		if got.Kind != ConstInt || got.Int.String() != tc.want {
			t.Errorf("%s = %v, want %s", tc.expr, got, tc.want)
		}
	}
	if got, ds := m2.Eval(top, parseExprText(t, "half(5.0)"), nil); len(ds) != 0 || got.Kind != ConstReal || got.Real != 2.5 {
		t.Errorf("half(5.0) = %v %v", got, ds)
	}
	if got, ds := m2.Eval(top, parseExprText(t, "noop()"), nil); len(ds) != 0 {
		t.Errorf("noop() = %v %v", got, ds)
	}
	// A parameter override flows into a function that reads the parameter.
	w, _ := top.Own("W")
	env := &Env{Params: map[*Symbol]Const{w: intConst(mustLit(t, "32'sd8"))}}
	if got, _ := m2.Eval(top, parseExprText(t, "usesparam()"), env); got.Int.String() != "32'sh00000010" {
		t.Errorf("usesparam() with W=8 = %v", got)
	}
}

func mustLit(t *testing.T, s string) svint.Value {
	t.Helper()
	v, err := svint.ParseLiteral(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConstantFunctionErrors(t *testing.T) {
	m, c := model(t, `module top;
  int g;
  function int reads_global(); return g; endfunction
  function int writes_global(); g = 1; return 1; endfunction
  function int with_output(output int o); o = 1; return 1; endfunction
  function int with_delay(); #5; return 1; endfunction
  function int nonblocking(); int x; x <= 1; return x; endfunction
  function int forever_loop(); forever ; return 0; endfunction
  function int local_array(); int a[4]; a[0] = 1; return a[0]; endfunction
  function int recurse(int n); return recurse(n + 1); endfunction
  function int too_many(int a); return a; endfunction
  function int fork_join(); fork join return 1; endfunction
  function int calls_task(); return 1; endfunction
  function int missing(int a, int b); return a + b; endfunction
  extern function int external(int a);
endmodule`)
	top := defScope(t, m, c, "top")
	cases := []struct{ expr, code string }{
		{"reads_global()", diag.CodeNotConstant},
		{"writes_global()", diag.CodeNotConstant},
		{"with_output(1)", diag.CodeNotConstant},
		{"with_delay()", diag.CodeNotConstant},
		{"nonblocking()", diag.CodeNotConstant},
		{"local_array()", diag.CodeNotConstant},
		{"recurse(0)", diag.CodeConstantLimit},
		{"too_many(1, 2)", diag.CodeNotConstant},
		{"missing(1)", diag.CodeNotConstant},
		{"external(1)", diag.CodeNotConstant},
	}
	for _, tc := range cases {
		_, ds := m.Eval(top, parseExprText(t, tc.expr), nil)
		found := false
		for _, d := range ds {
			found = found || d.Code == tc.code
		}
		if !found {
			t.Errorf("%s: want %s, got %v", tc.expr, tc.code, ds)
		}
	}
	// An endless loop stops at the step budget.
	_, ds := m.Eval(top, parseExprText(t, "forever_loop()"), &Env{MaxSteps: 5000})
	if len(ds) == 0 || ds[0].Code != diag.CodeConstantLimit {
		t.Errorf("forever loop: %v", ds)
	}
	// An exhausted budget stays exhausted: nothing else runs afterwards.
	env := &Env{MaxSteps: 50}
	if _, ds := m.Eval(top, parseExprText(t, "forever_loop()"), env); len(ds) == 0 {
		t.Error("expected the limit")
	}
}

func TestConstantFunctionsInDimensions(t *testing.T) {
	m, c := model(t, `module top;
  function automatic int width_for(int n); return n > 8 ? 16 : 8; endfunction
  localparam N = 12;
  logic [width_for(N)-1:0] v;
  logic [width_for(2)-1:0] w;
  int arr [width_for(N)];
endmodule`)
	top := defScope(t, m, c, "top")
	for name, want := range map[string]string{"v": "logic [15:0]", "w": "logic [7:0]", "arr": "int [0:15]"} {
		ty, ds := m.TypeOf(top.syms[name])
		if len(ds) != 0 || ty.String() != want {
			t.Errorf("%s: %v %v, want %s", name, ty, ds, want)
		}
	}
}
