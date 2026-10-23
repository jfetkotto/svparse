package sema

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/parser"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/svint"
)

func parseExprText(t *testing.T, src string) ast.Expr {
	t.Helper()
	toks, _ := preprocessor.Preprocess("e.sv", src, nil)
	e, errs := parser.ParseExpr(toks)
	if len(errs) != 0 {
		t.Fatalf("parsing %q: %+v", src, errs)
	}
	return e
}

// ctxScope returns the scope of module "top" in src, or the unit scope if src
// is empty.
func ctxScope(t *testing.T, src string) (*Model, *Scope) {
	t.Helper()
	m, c := model(t, src+"\nmodule top; endmodule")
	return m, defScope(t, m, c, "top")
}

func evalStr(t *testing.T, m *Model, s *Scope, expr string, env *Env) (Const, []diag.Diagnostic) {
	t.Helper()
	return m.Eval(s, parseExprText(t, expr), env)
}

func mustEval(t *testing.T, m *Model, s *Scope, expr string) Const {
	t.Helper()
	c, ds := evalStr(t, m, s, expr, nil)
	if len(ds) != 0 {
		t.Fatalf("%s: diagnostics %v", expr, ds)
	}
	return c
}

func TestSelfDeterminedEvaluation(t *testing.T) {
	m, s := ctxScope(t, "")
	cases := []struct{ expr, want string }{
		{"8'hFF", "8'hff"},
		{"4'b1x0z", "4'b1x0z"},
		{"8'hFF + 8'h01", "8'h00"}, // wraps at its own width
		{"8'hFF + 9'h001", "9'b100000000"},
		{"4'sb1111 + 4'sb0001", "4'sh0"},
		{"(8'hFF + 8'h01) + 16'h0000", "16'h0100"},           // the inner sum is widened by the outer operator
		{"((8'hFF + 8'h01) & 8'hFF) + 16'h0000", "16'h0000"}, // ... through a nested context operator
		{"((8'hFF + 8'h01) & 9'h1FF) + 16'h0000", "16'h0100"},
		{"~(8'h0F | 8'h00) + 16'h0000", "16'hfff0"},            // ... through a unary operator
		{"(1 ? 8'hFF + 8'h01 : 8'h00) + 16'h0000", "16'h0100"}, // ... through ?:
		{"(8'hFF + 8'h01) >> 0", "8'h00"},                      // a shifted value is not widened by the shift amount
		{"((8'hFF + 8'h01) << 0) + 16'h0000", "16'h0100"},
		{"3 + 4", "32'sh00000007"},
		{"-5", "32'shfffffffb"},
		{"7 / 2", "32'sh00000003"},
		{"-7 / 2", "32'shfffffffd"},
		{"-7 % 3", "32'shffffffff"},
		{"2 ** 10", "32'sh00000400"},
		{"{4'hF, 4'h1}", "8'hf1"},
		{"{3{2'b10}}", "6'b101010"},
		{"{2{4'ha, 4'hb}}", "16'habab"},
		{"~4'b0101", "4'ha"},
		{"&4'b1111", "1'b1"},
		{"^4'b1011", "1'b1"},
		{"~|4'b0000", "1'b1"},
		{"!4'b0100", "1'b0"},
		{"4'b1010 & 4'b0110", "4'h2"},
		{"4'b1010 | 4'b0101", "4'hf"},
		{"4'b1010 ^ 4'b0110", "4'hc"},
		{"8'h80 >> 7", "8'h01"},
		{"-8'sd128 >>> 7", "8'shff"},
		{"8'sh80 >>> 7", "8'shff"},
		{"8'h80 >>> 7", "8'h01"},
		{"4'b0001 << 2", "4'h4"},
		{"4'b0001 << 4", "4'h0"},
		{"1 ? 8'hFF : 16'h0", "16'h00ff"},
		{"0 ? 8'hFF : 16'h0", "16'h0000"},
		{"1'bx ? 4'b1010 : 4'b1001", "4'b10xx"},
		{"4'sb1111 < 4'sb0001", "1'b1"},
		{"4'sb1111 < 4'b0001", "1'b0"}, // mixed signedness compares unsigned
		{"4'b1x00 == 4'b1000", "1'bx"},
		{"4'b1x00 === 4'b1x00", "1'b1"},
		{"4'b1x00 ==? 4'b1?00", "1'b1"},
		{"3 > 2 && 2 > 1", "1'b1"},
		{"0 || 4'b0x00", "1'bx"},
		{"1 -> 0", "1'b0"},
		{"5 inside {1, [3:6]}", "1'b1"},
		{"9 inside {1, [3:6]}", "1'b0"},
		{"8'(300)", "8'sh2c"},
		{"signed'(4'hF)", "4'shf"},
		{"unsigned'(-4'sd1)", "4'hf"},
		{"$signed(4'hF)", "4'shf"},
		{"$unsigned(-1)", "32'hffffffff"},
		{"$clog2(1000)", "32'sh0000000a"},
		{"$clog2(1)", "32'sh00000000"},
		{"$bits(logic [7:0])", "32'sh00000008"},
		{"$bits(int)", "32'sh00000020"},
		{"$bits(real)", "32'sh00000040"},
		{"$countones(8'hF3)", "32'sh00000006"},
		{"$onehot(4'b0100)", "1'b1"},
		{"$onehot0(4'b0000)", "1'b1"},
		{"$isunknown(4'b01x0)", "1'b1"},
		{"4'hF[2]", "1'b1"},
		{"8'hA5[7:4]", "4'ha"},
		{"8'hA5[3+:4]", "4'h4"},
		{"8'hA5[7-:2]", "2'b10"},
		{"8'hA5[9]", "1'bx"},
	}
	for _, c := range cases {
		got, ds := evalStr(t, m, s, c.expr, nil)
		if len(ds) != 0 {
			t.Errorf("%s: diagnostics %v", c.expr, ds)
			continue
		}
		if got.Kind != ConstInt || got.Int.String() != c.want {
			t.Errorf("%s = %v, want %s", c.expr, got, c.want)
		}
	}
}

func TestRealStringAndMixedEvaluation(t *testing.T) {
	m, s := ctxScope(t, "")
	reals := map[string]float64{
		"1.5 * 2": 3, "1 + 0.5": 1.5, "7.0 / 2": 3.5, "2.0 ** 3": 8, "$sqrt(16.0)": 4, "$itor(3)": 3,
		"$pow(2.0, 10.0)": 1024, "-(1.5)": -1.5, "$floor(2.7)": 2, "$ceil(2.1)": 3, "1e3": 1000, "1_0.5": 10.5,
		"$bitstoreal($realtobits(2.5))": 2.5,
	}
	for expr, want := range reals {
		got, ds := evalStr(t, m, s, expr, nil)
		if len(ds) != 0 || got.Kind != ConstReal || got.Real != want {
			t.Errorf("%s = %v (%v), want %v", expr, got, ds, want)
		}
	}
	ints := map[string]string{
		"$rtoi(3.9)": "32'sh00000003", "$rtoi(-3.9)": "32'shfffffffd", "1.5 > 1": "1'b1", "2.0 == 2": "1'b1",
		"1.5 < 1.25": "1'b0", "int'(3.5)": "32'sh00000004", "int'(2.5)": "32'sh00000003",
	}
	for expr, want := range ints {
		got, ds := evalStr(t, m, s, expr, nil)
		if len(ds) != 0 || got.Kind != ConstInt || got.Int.String() != want {
			t.Errorf("%s = %v (%v), want %s", expr, got, ds, want)
		}
	}
	if got := mustEval(t, m, s, `"ab" == "ab"`); got.Int.String() != "1'b1" {
		t.Errorf("string equality: %v", got)
	}
	// A string literal in an expression is an integral value; it becomes a
	// string when assigned to one.
	if got := mustEval(t, m, s, `"hi\n"`); got.Kind != ConstInt || got.Int.String() != "24'h68690a" {
		t.Errorf("string literal: %v", got)
	}
	if got, _ := m.EvalAssign(s, String, parseExprText(t, `"hi\n"`), nil); got.Kind != ConstString || got.Str != "hi\n" {
		t.Errorf("string literal assigned to a string: %v", got)
	}
	if got := mustEval(t, m, s, `$typename(int)`); got.Str != "int" {
		t.Errorf("$typename: %v", got)
	}
}

func TestAssignmentContextWidensOperands(t *testing.T) {
	m, s := ctxScope(t, "")
	eval := func(target Type, expr string) string {
		t.Helper()
		be, ds := m.BindAssign(s, target, parseExprText(t, expr))
		if len(ds) != 0 {
			t.Fatalf("%s: %v", expr, ds)
		}
		c, ds := m.EvalBound(be, nil)
		if len(ds) != 0 {
			t.Fatalf("%s: %v", expr, ds)
		}
		return c.Int.String()
	}
	bit16 := Vector(BaseBit, false, Range{15, 0})
	bit64 := Vector(BaseBit, false, Range{63, 0})
	// An 8-bit sum carried into a 16-bit target keeps its carry.
	if got := eval(bit16, "8'hFF + 8'h01"); got != "16'h0100" {
		t.Errorf("sum in a 16-bit context = %s", got)
	}
	// Context does not reach through a concatenation or a comparison.
	if got := eval(bit16, "{8'hFF + 8'h01}"); got != "8'h00" {
		t.Errorf("concat operand stays self-determined: %s", got)
	}
	if got := eval(bit16, "(8'hFF + 8'h01) == 8'h00"); got != "1'b1" {
		t.Errorf("comparison operands are sized to each other only: %s", got)
	}
	if got := eval(bit64, "1 << 40"); got != "64'sh0000010000000000" {
		t.Errorf("shift widened by context: %s", got)
	}
	if got := eval(bit16, "~8'h0F"); got != "16'hfff0" {
		t.Errorf("~ widened by context: %s", got)
	}
	if got := eval(bit16, "1 ? 8'hFF : 8'h00"); got != "16'h00ff" {
		t.Errorf("?: widened by context: %s", got)
	}
	// An unbased unsized literal fills the context.
	if got := eval(bit16, "'1"); got != "16'hffff" {
		t.Errorf("'1 = %s", got)
	}
	if got := eval(bit16, "'x"); got != "16'bxxxxxxxxxxxxxxxx" {
		t.Errorf("'x = %s", got)
	}
	// The sign of the context does not change how the operands extend: a
	// negative signed operand sign-extends only if the whole expression is signed.
	sbit16 := Vector(BaseBit, true, Range{15, 0})
	if got := eval(sbit16, "4'sb1111 + 4'sb0000"); got != "16'shffff" {
		t.Errorf("signed extension: %s", got)
	}
	if got := eval(sbit16, "4'sb1111 + 4'b0000"); got != "16'h000f" {
		t.Errorf("one unsigned operand makes the sum unsigned: %s", got)
	}
	// Real targets leave integer operands alone.
	be, _ := m.BindAssign(s, typeReal, parseExprText(t, "1 + 2"))
	if c, _ := m.EvalBound(be, nil); c.Kind != ConstInt {
		t.Errorf("the integer sum is computed as an integer, then converted: %v", c)
	}
}

func TestBoundTypes(t *testing.T) {
	m, s := ctxScope(t, "typedef enum {A, B} e_t; typedef struct packed {logic [3:0] hi; bit [3:0] lo;} s_t;")
	_ = s
	m2, c := model(t, `typedef enum {A, B} e_t;
typedef struct packed {logic [3:0] hi; bit [3:0] lo;} s_t;
typedef struct {int x; string name;} u_t;
class C; int f; function int get(); return f; endfunction endclass
module top;
  logic [7:0] v; bit signed [3:0] sb; int i; real r; string str; e_t e; s_t st; u_t us; C obj;
  int arr[4]; logic [3:0][7:0] pk; int q[$]; int aa[string]; int dyn[];
  function logic [15:0] f(int x); return 0; endfunction
endmodule`)
	_ = m
	top := defScope(t, m2, c, "top")
	cases := map[string]string{
		"v": "logic [7:0]", "sb": "bit signed [3:0]", "i": "int", "r": "real", "str": "string", "e": "e_t", "st": "s_t",
		"v + i": "logic [31:0]", "v + sb": "logic [7:0]", "sb + sb": "bit signed [3:0]", "v & 4'hF": "logic [7:0]",
		"v == 8'h0": "logic", "v === 8'h0": "bit", "v && i": "logic", "!i": "bit", "&v": "logic", "~v": "logic [7:0]",
		"{v, v}": "logic [15:0]", "{v, sb}": "logic [11:0]", "{3{sb}}": "bit [11:0]", "v[0]": "logic", "v[3:0]": "logic [3:0]",
		"v[2+:3]": "logic [2:0]", "arr[1]": "int", "pk[1]": "logic [7:0]", "pk[1][2]": "logic", "q[0]": "int", "aa[\"k\"]": "int",
		"dyn[2]": "int", "st.hi": "logic [3:0]", "st.lo": "bit [3:0]", "us.name": "string", "us.x": "int", "obj.f": "int",
		"obj.get()": "int", "f(1)": "logic [15:0]", "arr.size()": "int", "q.size()": "int", "str.len()": "int", "q.sum()": "int",
		"e.name()": "string", "r * 2": "real", "r > 1": "logic", "i ? v : sb": "logic [7:0]", "$signed(v)": "logic signed [7:0]",
		"$unsigned(sb)": "bit [3:0]", "$bits(v)": "int", "$clog2(i)": "int", "8'(i)": "bit signed [7:0]", "int'(r)": "int", "e_t'(1)": "e_t",
		"str + str": "<invalid>",
	}
	for expr, want := range cases {
		be, ds := m2.Bind(top, parseExprText(t, expr))
		got := be.Type().String()
		if got != want {
			t.Errorf("type of %s = %s, want %s (diags %v)", expr, got, want, ds)
		}
	}
}

func TestBindDiagnostics(t *testing.T) {
	m, c := model(t, `typedef enum {A, B} e_t;
class C; endclass
class D; endclass
module top; logic [7:0] v; e_t e; int i; string s; C co; D dd; real r; endmodule`)
	top := defScope(t, m, c, "top")
	cases := []struct{ expr, code string }{
		{"nosuch + 1", diag.CodeUndeclared},
		{"v + nosuch", diag.CodeUndeclared},
		{"nosuch(1)", diag.CodeUndeclared},
		{"co.missing", diag.CodeUndeclared},
		{"co == dd", diag.CodeIncompatibleTypes},
		{"s + 1", diag.CodeIncompatibleTypes},
		{"r % 2", ""}, // real modulus: tolerated at this layer
		{"v ? co : i", diag.CodeIncompatibleTypes},
		{"C'(1)", diag.CodeIncompatibleTypes},
		{"pkg::x", diag.CodeUndeclared},
	}
	for _, tc := range cases {
		_, ds := m.Bind(top, parseExprText(t, tc.expr))
		if tc.code == "" {
			continue
		}
		found := false
		for _, d := range ds {
			found = found || d.Code == tc.code
		}
		if !found {
			t.Errorf("%s: want a %s diagnostic, got %v", tc.expr, tc.code, ds)
		}
	}
	// Invalid operands cause one diagnostic, not a cascade.
	_, ds := m.Bind(top, parseExprText(t, "(nosuch + 1) * (nosuch2 - 3) == v"))
	if len(ds) != 2 {
		t.Errorf("a cascade of diagnostics: %v", ds)
	}
	// Assignment checks.
	check := func(target Type, expr, code string) {
		t.Helper()
		_, ds := m.BindAssign(top, target, parseExprText(t, expr))
		var got string
		if len(ds) > 0 {
			got = ds[0].Code
		}
		if got != code {
			t.Errorf("assign %s <- %s: code %q, want %q (%v)", target, expr, got, code, ds)
		}
	}
	var enum, class, str Type
	enum, _ = m.TypeOf(top.syms["e"])
	class, _ = m.TypeOf(top.syms["co"])
	str = String
	check(enum, "e", "")
	check(enum, "0", diag.CodeCastRequired)
	check(typeInt, "e", "")
	check(typeInt, "r", "")
	check(typeReal, "1", "")
	check(str, `"text"`, "")
	check(typeInt, "s", diag.CodeCastRequired)
	check(class, "null", "")
	check(class, "dd", diag.CodeIncompatibleTypes)
	check(Invalid, "v", "") // an unresolved target is not blamed again
}

func TestParameters(t *testing.T) {
	m, c := model(t, `typedef enum {RED, GREEN = 5, BLUE} color_t;
module top #(parameter W = 8, parameter real R = 1, parameter byte B = 300, parameter string S = "hi");
  localparam D = W * 2;
  localparam [D-1:0] ALL = '1;
  localparam int E = $clog2(D) + 1;
  localparam color_t C0 = BLUE;
  localparam A = B_LATE;
  localparam B_LATE = 3;
  localparam CYC1 = CYC2 + 1;
  localparam CYC2 = CYC1 + 1;
  localparam SELF = SELF;
  localparam NOVAL;
  logic [W-1:0] v;
  logic [D-1:0] [3:0] pk;
endmodule`)
	top := defScope(t, m, c, "top")
	val := func(name string, env *Env) (Const, []diag.Diagnostic) {
		t.Helper()
		sym, ok := top.Own(name)
		if !ok {
			t.Fatalf("no parameter %s", name)
		}
		return m.ParamValue(sym, env)
	}
	str := func(name string, env *Env) string {
		c, ds := val(name, env)
		if len(ds) != 0 {
			t.Fatalf("%s: %v", name, ds)
		}
		return c.String()
	}
	for name, want := range map[string]string{
		"W": "32'sh00000008", "D": "32'sh00000010", "ALL": "16'hffff", "E": "32'sh00000005", "A": "32'sh00000003",
		"B": "8'sh2c", "S": `"hi"`, "R": "1",
	} {
		if got := str(name, nil); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	if got := str("C0", nil); got != "32'sh00000006" {
		t.Errorf("enum parameter = %s", got)
	}
	// Overrides flow through dependent parameters.
	w, _ := top.Own("W")
	env := &Env{Params: map[*Symbol]Const{w: intConst(svint.FromInt64(32, 4))}}
	if got := str("D", env); got != "32'sh00000008" {
		t.Errorf("D with W=4 is %s", got)
	}

	// Problems.
	for _, name := range []string{"CYC1", "SELF"} {
		_, ds := val(name, nil)
		if len(ds) == 0 || ds[0].Code != diag.CodeCircularParameter {
			t.Errorf("%s: want a circular-parameter error, got %v", name, ds)
		}
	}
	if _, ds := val("NOVAL", nil); len(ds) == 0 || ds[0].Code != diag.CodeNotConstant {
		t.Errorf("NOVAL: %v", ds)
	}
	// Dimensions are evaluated: logic [W-1:0] is 8 bits wide.
	v, _ := m.TypeOf(top.syms["v"])
	if v.String() != "logic [7:0]" {
		t.Errorf("v has type %s", v)
	}
	pk, _ := m.TypeOf(top.syms["pk"])
	if pk.String() != "logic [3:0] [15:0]" {
		t.Errorf("pk has type %s", pk)
	}
}

func TestEnumValues(t *testing.T) {
	m, c := model(t, `typedef enum {RED, GREEN = 5, BLUE} color_t;
typedef enum logic [3:0] {X0 = 4'hA, X1, X2} wide_t;
typedef enum bit [1:0] {Z0, Z1, Z2, Z3} narrow_t;
module top; color_t c; wide_t w; endmodule`)
	top := defScope(t, m, c, "top")
	for expr, want := range map[string]string{
		"RED": "32'sh00000000", "GREEN": "32'sh00000005", "BLUE": "32'sh00000006",
		"X0": "4'ha", "X1": "4'hb", "X2": "4'hc", "Z3": "2'b11",
		"$bits(color_t)": "32'sh00000020", "$bits(wide_t)": "32'sh00000004", "$bits(narrow_t)": "32'sh00000002",
		"BLUE + 1": "32'sh00000007", "GREEN == 5": "1'b1",
	} {
		got, ds := m.Eval(top, parseExprText(t, expr), nil)
		if len(ds) != 0 || got.Kind != ConstInt || got.Int.String() != want {
			t.Errorf("%s = %v (%v), want %s", expr, got, ds, want)
		}
	}
	et, _ := m.TypeOf(top.syms["c"])
	if en, ok := Resolve(et).(*Enum); !ok || len(en.Members) != 3 || en.Name != "color_t" {
		t.Errorf("c has type %v", et)
	}
}

func TestTypeResolution(t *testing.T) {
	m, c := model(t, `package pk; typedef logic [3:0] nib_t; class PC; endclass endpackage
typedef int word_t;
typedef word_t alias2_t;
typedef struct packed { logic [3:0] a; bit [11:0] b; } pkt_t;
typedef union packed { logic [7:0] raw; struct packed {logic [3:0] hi, lo;} nib; } u_t;
class Base; endclass
class Derived extends Base; endclass
interface bus; endinterface
module top #(parameter type T = int, parameter N = 4) (bus ifc, bus.mp mpi);
  word_t w; alias2_t a2; pkt_t pkt; u_t un; Base base; Derived der; pk::nib_t nib; pk::PC pcls;
  virtual bus vif; T tparam;
  struct packed { logic x; logic [2:0] y; } anon1, anon2;
  enum {E1, E2} ev1, ev2;
  logic [N-1:0] lv; logic [1:0][N-1:0] md; int ua[N]; int ub[2][3]; int qu[$]; int qb[$:9]; int da[]; int as[string]; int aw[*]; int ac[word_t];
  wire [3:0] nw; reg signed [7:0] rs; wire w1;
endmodule`)
	top := defScope(t, m, c, "top")
	typeOf := func(name string) Type {
		t.Helper()
		sym, ok := top.Own(name)
		if !ok {
			t.Fatalf("no symbol %s", name)
		}
		ty, ds := m.TypeOf(sym)
		if len(ds) != 0 {
			t.Fatalf("%s: %v", name, ds)
		}
		return ty
	}
	str := map[string]string{
		"w": "word_t", "a2": "alias2_t", "pkt": "pkt_t", "base": "Base", "der": "Derived", "nib": "nib_t", "pcls": "PC",
		"vif": "virtual bus", "ifc": "virtual bus", "mpi": "virtual bus.mp", "tparam": "T",
		"lv": "logic [3:0]", "md": "logic [3:0] [1:0]", "ua": "int [0:3]", "ub": "int [0:2] [0:1]", "qu": "int [$]", "qb": "int [$:9]",
		"da": "int []", "as": "int [string]", "aw": "int [*]", "ac": "int [word_t]", "nw": "logic [3:0]", "rs": "reg signed [7:0]", "w1": "logic",
	}
	for name, want := range str {
		if got := typeOf(name).String(); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if !Matches(typeOf("w"), typeInt) || !Matches(typeOf("a2"), typeOf("w")) || !Equivalent(typeOf("a2"), typeInt) {
		t.Error("typedef chains match their target")
	}
	d, b := typeOf("der").(*Class), typeOf("base").(*Class)
	if d.Super != b || typeOf("base") != Type(b) {
		t.Errorf("classes: %v super %v", d, d.Super)
	}
	if Assignment(b, d) != Assignable || Assignment(d, b) != CastRequired {
		t.Error("class assignment through resolved types")
	}
	if n, _ := Bits(typeOf("pkt")); n != 16 {
		t.Errorf("$bits(pkt_t) = %d", n)
	}
	if n, _ := Bits(typeOf("un")); n != 8 {
		t.Errorf("$bits(u_t) = %d", n)
	}
	// Two declarators share one anonymous type; two declarations do not.
	if typeOf("anon1") != typeOf("anon2") || typeOf("ev1") != typeOf("ev2") {
		t.Error("one anonymous aggregate for \"struct {...} a, b;\"")
	}
	if n, _ := Bits(typeOf("anon1")); n != 4 {
		t.Errorf("anon struct is %d bits", n)
	}
	if tp, ok := typeOf("tparam").(*TypeParam); !ok || tp.Name != "T" {
		t.Errorf("type parameter: %v", typeOf("tparam"))
	}
	// Resolving a type name that does not exist is reported once.
	_, ds := m.ResolveType(top, &ast.Type{Name: "no_such_t"})
	if len(ds) != 1 || ds[0].Code != diag.CodeUndeclared {
		t.Errorf("unknown type: %v", ds)
	}
	_, ds = m.ResolveType(top, &ast.Type{Name: "w"})
	if len(ds) != 1 || ds[0].Code != diag.CodeInvalidType {
		t.Errorf("a variable is not a type: %v", ds)
	}
	if got, _ := m.ResolveType(top, nil); got != Invalid {
		t.Error("nil type")
	}
}

func TestEvaluationLimitsAndErrors(t *testing.T) {
	m, s := ctxScope(t, "int v; localparam P = 1;")
	// A variable is not a constant.
	if _, ds := evalStr(t, m, s, "v + 1", nil); len(ds) == 0 || ds[0].Code != diag.CodeNotConstant {
		t.Errorf("variable in a constant expression: %v", ds)
	}
	if _, ds := evalStr(t, m, s, "undefined_fn(1)", nil); len(ds) == 0 {
		t.Error("unknown function")
	}
	if _, ds := evalStr(t, m, s, "$random", nil); len(ds) == 0 || ds[0].Code != diag.CodeNotConstant {
		t.Errorf("$random is not constant: %v", ds)
	}
	// The step budget stops a large expression.
	big := strings.Repeat("P + ", 200) + "P"
	if _, ds := evalStr(t, m, s, big, &Env{MaxSteps: 100}); len(ds) == 0 || ds[0].Code != diag.CodeConstantLimit {
		t.Errorf("step limit: %v", ds)
	}
	if c, ds := evalStr(t, m, s, big, nil); len(ds) != 0 || c.Int.String() != "32'sh000000c9" {
		t.Errorf("without the limit: %v %v", c, ds)
	}
	// Deep nesting is bounded, not a stack overflow.
	deep := strings.Repeat("(", 600) + "1" + strings.Repeat(")", 600)
	toks, _ := preprocessor.Preprocess("d.sv", deep, nil)
	if e, _ := parser.ParseExpr(toks); e != nil {
		m.Eval(s, e, nil)
	}
	deepNeg := strings.Repeat("-", 450) + "1"
	toks, _ = preprocessor.Preprocess("d.sv", deepNeg, nil)
	if e, _ := parser.ParseExpr(toks); e != nil {
		if _, ds := m.Eval(s, e, nil); len(ds) == 0 {
			t.Error("a 450-deep expression should hit the bind depth limit")
		}
	}
	// Replication with a huge or negative count.
	for _, expr := range []string{"{0{1'b1}}", "{-1{1'b1}}", "{100000000{8'hFF}}"} {
		evalStr(t, m, s, expr, nil)
	}
}

func TestWalkBound(t *testing.T) {
	m, s := ctxScope(t, "localparam P = 2;")
	be, _ := m.Bind(s, parseExprText(t, "P ? {2{4'hA}} + 1 : 8'(3)"))
	var kinds []string
	WalkBound(be, func(n BoundExpr) bool {
		switch n.(type) {
		case *BName:
			kinds = append(kinds, "name")
		case *BLiteral:
			kinds = append(kinds, "lit")
		case *BReplicate:
			kinds = append(kinds, "rep")
		case *BCast:
			kinds = append(kinds, "cast")
		}
		return true
	})
	if strings.Join(kinds, ",") != "name,lit,rep,lit,lit,cast,lit" && strings.Join(kinds, ",") != "name,rep,lit,lit,lit,cast,lit" {
		t.Errorf("walk order: %v", kinds)
	}
	count := 0
	WalkBound(be, func(BoundExpr) bool { count++; return false })
	if count != 1 {
		t.Errorf("returning false must stop the descent, visited %d", count)
	}
	WalkBound(nil, func(BoundExpr) bool { t.Error("nil"); return true })
}

// Every variable, port, argument and parameter in the vendored files gets a
// type, and every parameter a value, without panicking; most resolve cleanly
// (the rest depend on UVM, on macros the corpus leaves undefined, or on
// constructs this layer does not model).
func TestCorpusTypesAndParameters(t *testing.T) {
	var typed, invalid, params, paramOK int
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
		m := c.Model()
		var visit func(s *Scope, depth int)
		visit = func(s *Scope, depth int) {
			if depth > 100 {
				return
			}
			for _, sym := range s.Symbols() {
				switch sym.Kind {
				case SymVariable, SymNet, SymPort, SymArg, SymParameter:
					typed++
					if ty, _ := m.TypeOf(sym); IsInvalid(ty) {
						invalid++
					}
				}
				if sym.Kind == SymParameter {
					params++
					if _, ds := m.ParamValue(sym, &Env{MaxSteps: 10000}); len(ds) == 0 {
						paramOK++
					}
				}
				if sym.Child != nil {
					visit(sym.Child, depth+1)
				}
			}
		}
		for _, u := range c.Units() {
			visit(m.units[u], 0)
		}
		for _, def := range c.Definitions() {
			if s, ok := m.DefinitionScope(def); ok {
				visit(s, 0)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d typed symbols (%d invalid), %d parameters (%d evaluated cleanly)", typed, invalid, params, paramOK)
	if typed < 2000 || params < 100 {
		t.Fatalf("too few symbols: %d typed, %d parameters", typed, params)
	}
	if invalid*10 > typed {
		t.Errorf("%d of %d symbols have no type", invalid, typed)
	}
	if paramOK*10 < params*7 {
		t.Errorf("only %d of %d parameters evaluate", paramOK, params)
	}
}

// FuzzBind types and evaluates every declaration of arbitrary source.
func FuzzBind(f *testing.F) {
	f.Add("module m #(parameter W = 4, parameter D = W*2) (input [W-1:0] a); localparam int X = $clog2(D) + {2{W}}; logic [X:0] v = a + 1; typedef struct packed {logic [W:0] f;} s_t; s_t s; endmodule")
	f.Add("class A; int x; function int f(int y); return x + y; endfunction endclass module m; A a; localparam P = P + 1; int q[$]; endmodule")
	f.Add("package p; typedef enum {A=1, B} e_t; localparam e_t X = B; endpackage module m; import p::*; localparam Y = X + A; endmodule")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		file, _ := parser.Parse("f.sv", toks)
		c := NewCompilation(Options{})
		c.AddFile(file)
		m := c.Model()
		var visit func(s *Scope, depth int)
		visit = func(s *Scope, depth int) {
			if depth > 50 {
				return
			}
			for _, sym := range s.Symbols() {
				switch sym.Kind {
				case SymVariable, SymNet, SymPort, SymArg, SymParameter, SymEnumMember:
					m.TypeOf(sym)
				}
				if sym.Kind == SymParameter {
					m.ParamValue(sym, &Env{MaxSteps: 5000})
				}
				if v, ok := sym.Decl.(*ast.Variable); ok && v.InitialExpr != nil {
					ty, _ := m.TypeOf(sym)
					m.BindAssign(sym.Scope, ty, v.InitialExpr)
				}
				if sym.Child != nil {
					visit(sym.Child, depth+1)
				}
			}
		}
		for _, u := range c.Units() {
			visit(m.units[u], 0)
		}
		for _, def := range c.Definitions() {
			if s, ok := m.DefinitionScope(def); ok {
				visit(s, 0)
			}
		}
	})
}
