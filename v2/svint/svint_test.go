package svint

import (
	"math/big"
	"math/rand"
	"testing"
)

func mustLit(t *testing.T, s string) Value {
	t.Helper()
	v, err := ParseLiteral(s)
	if err != nil {
		t.Fatalf("ParseLiteral(%q): %v", s, err)
	}
	return v
}

// canonical fails the test if v breaks the invariant that bits at and above
// the width are zero.
func canonical(t *testing.T, what string, v Value) {
	t.Helper()
	if !v.Valid() {
		return
	}
	if len(v.a) != nwords(v.width) || len(v.b) != len(v.a) {
		t.Fatalf("%s: plane length %d/%d for width %d", what, len(v.a), len(v.b), v.width)
	}
	if rem := v.width % 64; rem != 0 {
		hi := ^uint64(0) << rem
		if v.a[len(v.a)-1]&hi != 0 || v.b[len(v.b)-1]&hi != 0 {
			t.Fatalf("%s: bits above width %d are set: %v", what, v.width, v)
		}
	}
}

func TestParseLiteral(t *testing.T) {
	cases := []struct {
		src    string
		width  int
		signed bool
		bin    string
	}{
		{"0", 32, true, "00000000000000000000000000000000"},
		{"12", 32, true, "00000000000000000000000000001100"},
		{"1_000", 32, true, "00000000000000000000001111101000"},
		{"4'b1010", 4, false, "1010"},
		{"4'B10_10", 4, false, "1010"},
		{"8'hFF", 8, false, "11111111"},
		{"8'hf", 8, false, "00001111"},
		{"8'sh80", 8, true, "10000000"},
		{"4'o17", 4, false, "1111"}, // 001 111 truncated to the low four bits
		{"6'o77", 6, false, "111111"},
		{"4'd9", 4, false, "1001"},
		{"4'sd9", 4, true, "1001"},
		{"3'd9", 3, false, "001"},
		{"8'b1x0z", 8, false, "00001x0z"}, // a known leftmost digit zero-extends
		{"8'bx1", 8, false, "xxxxxxx1"},
		{"8'bz1", 8, false, "zzzzzzz1"},
		{"8'b01", 8, false, "00000001"},
		{"12'hx", 12, false, "xxxxxxxxxxxx"},
		{"12'h?f", 12, false, "zzzzzzzz1111"},
		{"4'dx", 4, false, "xxxx"},
		{"4'dz", 4, false, "zzzz"},
		{"'hF", 32, false, "00000000000000000000000000001111"},
		{"'sd5", 32, true, "00000101"},
		{"'b101", 32, false, "00000000000000000000000000000101"},
		{"'0", 1, false, "0"},
		{"'1", 1, false, "1"},
		{"'x", 1, false, "x"},
		{"'Z", 1, false, "z"},
		{"64'hFFFF_FFFF_FFFF_FFFF", 64, false, "1111111111111111111111111111111111111111111111111111111111111111"},
	}
	for _, c := range cases {
		v, err := ParseLiteral(c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		canonical(t, c.src, v)
		if v.Width() != c.width || v.Signed() != c.signed {
			t.Errorf("%q: got %d-bit signed=%v, want %d-bit signed=%v", c.src, v.Width(), v.Signed(), c.width, c.signed)
		}
	}
	// Exact bit patterns for the cases whose expected strings are unambiguous.
	exact := map[string]string{
		"4'b1010": "1010", "8'hFF": "11111111", "8'hf": "00001111", "8'bx1": "xxxxxxx1",
		"8'bz1": "zzzzzzz1", "8'b01": "00000001", "12'hx": "xxxxxxxxxxxx", "12'h?f": "zzzzzzzz1111",
		"4'dx": "xxxx", "'x": "x", "'Z": "z", "6'o77": "111111", "3'd9": "001", "4'd9": "1001",
		"8'b1x0z": "00001x0z",
	}
	for src, want := range exact {
		if got := mustLit(t, src).Binary(); got != want {
			t.Errorf("%q: bits %s, want %s", src, got, want)
		}
	}
}

func TestParseLiteralWidensUnsizedNumbers(t *testing.T) {
	if v := mustLit(t, "'h1_0000_0000"); v.Width() != 36 || v.Hex() != "100000000" {
		t.Errorf("got %v", v)
	}
	if v := mustLit(t, "99999999999"); v.Width() != 38 || !v.Signed() {
		t.Errorf("got %d bits signed=%v", v.Width(), v.Signed())
	}
	if n, ok := mustLit(t, "99999999999").Int64(); !ok || n != 99999999999 {
		t.Errorf("value %d", n)
	}
}

func TestParseLiteralErrors(t *testing.T) {
	for _, src := range []string{
		"", "'", "8'", "8'h", "8'q1", "0'b1", "8'b2", "8'o8", "8'hg", "8'd12a", "-5", "+5", "1x", "99999999'b1", "8'd-1", "'sz",
	} {
		if v, err := ParseLiteral(src); err == nil {
			t.Errorf("%q: expected an error, got %v", src, v)
		}
	}
}

func TestFormatting(t *testing.T) {
	cases := []struct {
		v                  Value
		bin, hex, oct, dec string
		str                string
	}{
		{FromUint64(8, 0xA5), "10100101", "a5", "245", "165", "8'ha5"},
		{FromInt64(8, -3), "11111101", "fd", "375", "-3", "8'shfd"},
		{mustLit(t, "8'b1x0z0110"), "1x0z0110", "X6", "XZ6", "X", "8'b1x0z0110"},
		{mustLit(t, "4'bxxxx"), "xxxx", "x", "xx", "x", "4'bxxxx"},
		{mustLit(t, "4'bzzzz"), "zzzz", "z", "zz", "z", "4'bzzzz"},
		{mustLit(t, "4'bz10z"), "z10z", "Z", "zZ", "Z", "4'bz10z"},
		{FromUint64(5, 3), "00011", "03", "03", "3", "5'b00011"},
	}
	for _, c := range cases {
		if got := c.v.Binary(); got != c.bin {
			t.Errorf("%v Binary = %s, want %s", c.v, got, c.bin)
		}
		if got := c.v.Hex(); got != c.hex {
			t.Errorf("%v Hex = %s, want %s", c.v, got, c.hex)
		}
		if got := c.v.Octal(); got != c.oct {
			t.Errorf("%v Octal = %s, want %s", c.v, got, c.oct)
		}
		if got := c.v.Decimal(); got != c.dec {
			t.Errorf("%v Decimal = %s, want %s", c.v, got, c.dec)
		}
		if got := c.v.String(); got != c.str {
			t.Errorf("String = %s, want %s", got, c.str)
		}
	}
	if (Value{}).String() != "<invalid>" || (Value{}).Decimal() != "" {
		t.Error("invalid value should format as invalid")
	}
}

func TestInvalidValuesPropagate(t *testing.T) {
	bad := Value{}
	ok := FromUint64(8, 5)
	for name, v := range map[string]Value{
		"New(0)": New(0, false), "New(-1)": New(-1, true), "New(MaxWidth+1)": New(MaxWidth+1, false),
		"FromUint64(0)": FromUint64(0, 1), "Add": Add(bad, ok), "Add2": Add(ok, bad), "And": And(bad, ok), "Not": Not(bad),
		"Neg": Neg(bad), "Pow": Pow(ok, bad), "Shl": Shl(bad, ok), "Concat": Concat(ok, bad), "Cond": Cond(ok, bad, ok),
		"Replicate(0)": Replicate(0, ok), "Replicate huge": Replicate(MaxWidth, ok), "Slice backwards": Slice(ok, 1, 2),
		"Resize(0)": ok.Resize(0), "Concat()": Concat(), "Eq": Eq(bad, ok),
	} {
		if v.Valid() {
			t.Errorf("%s should be invalid, got %v", name, v)
		}
	}
	if Truth(bad) != BitX || bad.IsKnown() {
		t.Error("invalid value must not look known or true")
	}
	if _, ok := bad.Uint64(); ok {
		t.Error("invalid Uint64 should fail")
	}
}

func TestAccessors(t *testing.T) {
	v := mustLit(t, "8'b1x0z0110")
	if v.Bit(7) != Bit1 || v.Bit(6) != BitX || v.Bit(5) != Bit0 || v.Bit(4) != BitZ || v.Bit(8) != BitX || v.Bit(-1) != BitX {
		t.Errorf("bits: %v", v.Binary())
	}
	if v.IsKnown() {
		t.Error("value with x is not known")
	}
	if n, ok := FromUint64(70, 7).Uint64(); !ok || n != 7 {
		t.Errorf("Uint64 = %d %v", n, ok)
	}
	wide := Concat(FromUint64(8, 1), FromUint64(64, 0))
	if _, ok := wide.Uint64(); ok {
		t.Error("a value above 64 bits does not fit")
	}
	if n, ok := FromInt64(8, -1).Int64(); !ok || n != -1 {
		t.Errorf("Int64 = %d", n)
	}
	if n, ok := FromInt64(8, -1).AsUnsigned().Int64(); !ok || n != 255 {
		t.Errorf("unsigned Int64 = %d", n)
	}
	if got := FromUint64(8, 0xF0).OnesCount(); got != 4 {
		t.Errorf("OnesCount = %d", got)
	}
}

// ---- four-state truth tables ----

var fourStates = []Bit{Bit0, Bit1, BitZ, BitX}

func tableCheck(t *testing.T, name string, op func(x, y Value) Value, want func(a, b Bit) Bit) {
	t.Helper()
	for _, a := range fourStates {
		for _, b := range fourStates {
			got := op(FromBit(a), FromBit(b))
			canonical(t, name, got)
			if got.Bit(0) != want(a, b) {
				t.Errorf("%c %s %c = %c, want %c", a.Char(), name, b.Char(), got.Bit(0), want(a, b).Char())
			}
		}
	}
}

func unk(b Bit) bool { return b >= BitZ }

func TestBitwiseTruthTables(t *testing.T) {
	tableCheck(t, "&", And, func(a, b Bit) Bit {
		switch {
		case a == Bit0 || b == Bit0:
			return Bit0
		case a == Bit1 && b == Bit1:
			return Bit1
		}
		return BitX
	})
	tableCheck(t, "|", Or, func(a, b Bit) Bit {
		switch {
		case a == Bit1 || b == Bit1:
			return Bit1
		case a == Bit0 && b == Bit0:
			return Bit0
		}
		return BitX
	})
	tableCheck(t, "^", Xor, func(a, b Bit) Bit {
		if unk(a) || unk(b) {
			return BitX
		}
		return a ^ b
	})
	tableCheck(t, "~^", Xnor, func(a, b Bit) Bit {
		if unk(a) || unk(b) {
			return BitX
		}
		return 1 - (a ^ b)
	})
	for _, a := range fourStates {
		want := BitX
		if a == Bit0 {
			want = Bit1
		} else if a == Bit1 {
			want = Bit0
		}
		if got := Not(FromBit(a)).Bit(0); got != want {
			t.Errorf("~%c = %c, want %c", a.Char(), got.Char(), want.Char())
		}
	}
}

func TestLogicalOperators(t *testing.T) {
	z, o, x := FromUint64(4, 0), FromUint64(4, 4), mustLit(t, "4'b0x00")
	want := func(v Value, b Bit) {
		t.Helper()
		if v.Bit(0) != b || v.Width() != 1 {
			t.Errorf("got %v, want %c", v, b.Char())
		}
	}
	want(LogicalNot(z), Bit1)
	want(LogicalNot(o), Bit0)
	want(LogicalNot(x), BitX)
	want(LogicalAnd(z, x), Bit0)
	want(LogicalAnd(o, x), BitX)
	want(LogicalAnd(o, o), Bit1)
	want(LogicalOr(o, x), Bit1)
	want(LogicalOr(z, x), BitX)
	want(LogicalOr(z, z), Bit0)
	want(Implies(z, x), Bit1)
	want(Implies(o, z), Bit0)
	want(Equiv(o, o), Bit1)
	want(Equiv(z, z), Bit1)
	want(Equiv(o, z), Bit0)
	want(Equiv(o, x), BitX)
	if Truth(mustLit(t, "4'b0x01")) != Bit1 {
		t.Error("a 1 bit makes a vector true even beside an x")
	}
}

func TestReductions(t *testing.T) {
	cases := []struct {
		v                           string
		and, or, xor, nand, nor, xn Bit
	}{
		{"4'b1111", Bit1, Bit1, Bit0, Bit0, Bit0, Bit1},
		{"4'b1011", Bit0, Bit1, Bit1, Bit1, Bit0, Bit0},
		{"4'b0000", Bit0, Bit0, Bit0, Bit1, Bit1, Bit1},
		{"4'b11x1", BitX, Bit1, BitX, BitX, Bit0, BitX},
		{"4'b01x0", Bit0, Bit1, BitX, Bit1, Bit0, BitX},
		{"4'b00z0", Bit0, BitX, BitX, Bit1, BitX, BitX},
	}
	for _, c := range cases {
		v := mustLit(t, c.v)
		for name, p := range map[string][2]Bit{
			"&": {RedAnd(v).Bit(0), c.and}, "|": {RedOr(v).Bit(0), c.or}, "^": {RedXor(v).Bit(0), c.xor},
			"~&": {RedNand(v).Bit(0), c.nand}, "~|": {RedNor(v).Bit(0), c.nor}, "~^": {RedXnor(v).Bit(0), c.xn},
		} {
			if p[0] != p[1] {
				t.Errorf("%s%s = %c, want %c", name, c.v, p[0].Char(), p[1].Char())
			}
		}
	}
}

// ---- arithmetic against reference implementations ----

func TestArithmeticMatchesUint64Reference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		w := 1 + rng.Intn(64)
		mask := ^uint64(0)
		if w < 64 {
			mask = uint64(1)<<w - 1
		}
		a, b := rng.Uint64()&mask, rng.Uint64()&mask
		x, y := FromUint64(w, a), FromUint64(w, b)
		check := func(name string, got Value, want uint64) {
			t.Helper()
			canonical(t, name, got)
			if g, ok := got.Uint64(); !ok || g != want&mask {
				t.Fatalf("w=%d %d %s %d = %v, want %d", w, a, name, b, got, want&mask)
			}
		}
		check("+", Add(x, y), a+b)
		check("-", Sub(x, y), a-b)
		check("*", Mul(x, y), a*b)
		check("&", And(x, y), a&b)
		check("|", Or(x, y), a|b)
		check("^", Xor(x, y), a^b)
		check("~", Not(x), ^a)
		check("neg", Neg(x), -a)
		if b != 0 {
			check("/", Div(x, y), a/b)
			check("%", Mod(x, y), a%b)
		}
		lt, _ := Lt(x, y).Uint64()
		if (lt == 1) != (a < b) {
			t.Fatalf("w=%d %d < %d = %d", w, a, b, lt)
		}
		eq, _ := Eq(x, y).Uint64()
		if (eq == 1) != (a == b) {
			t.Fatalf("eq mismatch")
		}
		sh := uint64(rng.Intn(80))
		amt := FromUint64(8, sh)
		var wantL, wantR uint64
		if sh < uint64(w) {
			wantL, wantR = a<<sh, a>>sh
		}
		check("<<", Shl(x, amt), wantL)
		check(">>", Shr(x, amt), wantR)
	}
}

func TestSignedArithmeticMatchesInt64Reference(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 5000; i++ {
		w := 2 + rng.Intn(62)
		a := int64(rng.Uint64()) >> (64 - w)
		b := int64(rng.Uint64()) >> (64 - w)
		x, y := FromInt64(w, a), FromInt64(w, b)
		wrap := func(n int64) int64 { return n << (64 - w) >> (64 - w) }
		check := func(name string, got Value, want int64) {
			t.Helper()
			canonical(t, name, got)
			if g, ok := got.Int64(); !ok || g != wrap(want) {
				t.Fatalf("w=%d %d %s %d = %v (%d), want %d", w, a, name, b, got, g, wrap(want))
			}
		}
		check("+", Add(x, y), a+b)
		check("-", Sub(x, y), a-b)
		check("*", Mul(x, y), a*b)
		check("neg", Neg(x), -a)
		if b != 0 && !(a == -1<<(w-1) && b == -1) {
			check("/", Div(x, y), a/b)
			check("%", Mod(x, y), a%b)
		}
		if lt, _ := Lt(x, y).Uint64(); (lt == 1) != (a < b) {
			t.Fatalf("w=%d %d < %d = %d", w, a, b, lt)
		}
		sh := rng.Intn(70)
		got := Sshr(x, FromUint64(8, uint64(sh)))
		want := a >> min(sh, 63)
		check(">>>", got, want)
	}
}

func TestWideArithmeticMatchesBig(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 300; i++ {
		w := 65 + rng.Intn(400)
		randBig := func() *big.Int {
			n := new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(w)))
			return n
		}
		a, b := randBig(), randBig()
		m := new(big.Int).Lsh(big.NewInt(1), uint(w))
		x, y := FromBig(w, false, a), FromBig(w, false, b)
		ref := func(n *big.Int) string { return new(big.Int).Mod(n, m).String() }
		for name, p := range map[string][2]string{
			"+": {Add(x, y).Decimal(), ref(new(big.Int).Add(a, b))},
			"-": {Sub(x, y).Decimal(), ref(new(big.Int).Sub(a, b))},
			"*": {Mul(x, y).Decimal(), ref(new(big.Int).Mul(a, b))},
		} {
			if p[0] != p[1] {
				t.Fatalf("w=%d %s: got %s want %s", w, name, p[0], p[1])
			}
		}
		if b.Sign() != 0 {
			if got, want := Div(x, y).Decimal(), new(big.Int).Quo(a, b).String(); got != want {
				t.Fatalf("w=%d /: got %s want %s", w, got, want)
			}
			if got, want := Mod(x, y).Decimal(), new(big.Int).Rem(a, b).String(); got != want {
				t.Fatalf("w=%d %%: got %s want %s", w, got, want)
			}
		}
		if lt, _ := Lt(x, y).Uint64(); (lt == 1) != (a.Cmp(b) < 0) {
			t.Fatalf("w=%d: Lt wrong", w)
		}
	}
}

func TestUnknownOperandsGiveX(t *testing.T) {
	x := mustLit(t, "8'b0000_x000")
	one := FromUint64(8, 1)
	for name, v := range map[string]Value{
		"+": Add(x, one), "-": Sub(one, x), "*": Mul(x, one), "/": Div(x, one), "%": Mod(x, one),
		"neg": Neg(x), "**": Pow(x, one), "<<": Shl(one, x), ">>": Shr(one, x),
	} {
		if v.Binary() != "xxxxxxxx" {
			t.Errorf("%s with an unknown operand = %v, want all x", name, v)
		}
	}
	if Div(one, FromUint64(8, 0)).Binary() != "xxxxxxxx" || Mod(one, FromUint64(8, 0)).Binary() != "xxxxxxxx" {
		t.Error("division by zero must be x")
	}
	for name, v := range map[string]Value{"<": Lt(x, one), "<=": Le(x, one), ">": Gt(x, one), ">=": Ge(x, one)} {
		if v.Bit(0) != BitX {
			t.Errorf("%s with an unknown operand = %v", name, v)
		}
	}
}

func TestMixedSignednessIsUnsigned(t *testing.T) {
	// -1 (signed 8-bit) compared with an unsigned 8-bit 1: both unsigned, so 255 > 1.
	if got := Gt(FromInt64(8, -1), FromUint64(8, 1)); got.Bit(0) != Bit1 {
		t.Errorf("mixed comparison treated the signed operand as signed: %v", got)
	}
	if got := Lt(FromInt64(8, -1), FromInt64(8, 1)); got.Bit(0) != Bit1 {
		t.Errorf("both signed: -1 < 1 should hold: %v", got)
	}
	// A narrower signed operand extends by sign only if the result is signed.
	s := Add(FromInt64(4, -1), FromInt64(8, 0))
	if n, _ := s.Int64(); n != -1 || !s.Signed() || s.Width() != 8 {
		t.Errorf("signed extension: %v", s)
	}
	u := Add(FromInt64(4, -1), FromUint64(8, 0))
	if n, _ := u.Uint64(); n != 15 || u.Signed() {
		t.Errorf("zero extension when mixed: %v", u)
	}
}

func TestPow(t *testing.T) {
	i := func(w int, n int64) Value { return FromInt64(w, n) }
	cases := []struct {
		x, y Value
		want string
	}{
		{i(32, 2), i(32, 10), "1024"},
		{i(32, 0), i(32, 0), "1"},
		{i(32, 5), i(32, 0), "1"},
		{i(32, -2), i(32, 3), "-8"},
		{i(32, 2), i(32, -1), "0"},
		{i(32, -3), i(32, -1), "0"},
		{i(32, 1), i(32, -5), "1"},
		{i(32, -1), i(32, -5), "-1"},
		{i(32, -1), i(32, -4), "1"},
		{i(8, 2), i(32, 8), "0"}, // overflows the 8-bit result
		{FromUint64(8, 3), FromUint64(8, 5), "243"},
	}
	for _, c := range cases {
		if got := Pow(c.x, c.y).Decimal(); got != c.want {
			t.Errorf("%v ** %v = %s, want %s", c.x, c.y, got, c.want)
		}
	}
	if Pow(i(32, 0), i(32, -1)).Binary() != X(32, true).Binary() {
		t.Error("0 ** negative must be x")
	}
	// A huge exponent must finish quickly via modular exponentiation.
	if got := Pow(FromUint64(16, 3), FromUint64(64, 1<<62)); !got.Valid() {
		t.Error("huge exponent")
	}
}

func TestShifts(t *testing.T) {
	s := FromInt64(8, -8) // 11111000
	if got := Sshr(s, FromUint64(4, 2)).Binary(); got != "11111110" {
		t.Errorf(">>> = %s", got)
	}
	if got := Shr(s, FromUint64(4, 2)).Binary(); got != "00111110" {
		t.Errorf(">> = %s", got)
	}
	if got := Shl(s, FromUint64(4, 2)).Binary(); got != "11100000" {
		t.Errorf("<< = %s", got)
	}
	if got := Sshl(s, FromUint64(4, 1)).Binary(); got != "11110000" {
		t.Errorf("<<< = %s", got)
	}
	if got := Sshr(s, FromUint64(8, 200)).Binary(); got != "11111111" {
		t.Errorf("over-shift of a negative = %s", got)
	}
	if got := Sshr(FromUint64(8, 0x80), FromUint64(4, 1)).Binary(); got != "01000000" {
		t.Errorf(">>> on unsigned is logical: %s", got)
	}
	if got := Shl(FromUint64(8, 1), FromUint64(8, 200)).Binary(); got != "00000000" {
		t.Errorf("over-shift = %s", got)
	}
	if got := Shl(mustLit(t, "4'b1x01"), FromUint64(4, 1)).Binary(); got != "x010" {
		t.Errorf("shifting unknown bits = %s", got)
	}
	// The shift amount is unsigned: a signed 8'hFF is 255, not -1.
	if got := Shr(FromUint64(8, 0xFF), FromInt64(8, -1)).Binary(); got != "00000000" {
		t.Errorf("amount must be unsigned: %s", got)
	}
}

func TestEqualityOperators(t *testing.T) {
	v := func(s string) Value { return mustLit(t, s) }
	cases := []struct {
		x, y               string
		eq, caseEq, wildEq Bit
	}{
		{"4'b1010", "4'b1010", Bit1, Bit1, Bit1},
		{"4'b1010", "4'b1011", Bit0, Bit0, Bit0},
		{"4'b10x0", "4'b10x0", BitX, Bit1, Bit1}, // the wildcard in the right operand matches
		{"4'b10x0", "4'b1010", BitX, Bit0, BitX},
		{"4'b10x0", "4'b0110", Bit0, Bit0, Bit0}, // a known bit differs
		{"4'b10z0", "4'b10x0", BitX, Bit0, Bit1},
		{"4'b1010", "4'b10x0", BitX, Bit0, Bit1}, // wildcard in the right operand
		{"4'b1010", "4'b11x0", Bit0, Bit0, Bit0},
		{"4'b10x0", "4'b1?00", BitX, Bit0, BitX}, // x in the left against a known bit
	}
	for _, c := range cases {
		if got := Eq(v(c.x), v(c.y)).Bit(0); got != c.eq {
			t.Errorf("%s == %s = %c, want %c", c.x, c.y, got.Char(), c.eq.Char())
		}
		if got := CaseEq(v(c.x), v(c.y)).Bit(0); got != c.caseEq {
			t.Errorf("%s === %s = %c, want %c", c.x, c.y, got.Char(), c.caseEq.Char())
		}
		if got := WildEq(v(c.x), v(c.y)).Bit(0); got != c.wildEq {
			t.Errorf("%s ==? %s = %c, want %c", c.x, c.y, got.Char(), c.wildEq.Char())
		}
	}
	if Ne(v("4'b1010"), v("4'b1011")).Bit(0) != Bit1 || CaseNe(v("4'bx"), v("4'bx")).Bit(0) != Bit0 || WildNe(v("4'b1010"), v("4'b10x0")).Bit(0) != Bit0 {
		t.Error("negated equality operators")
	}
}

func TestCond(t *testing.T) {
	a, b := mustLit(t, "4'b1010"), mustLit(t, "4'b1001")
	if got := Cond(FromBit(Bit1), a, b).Binary(); got != "1010" {
		t.Errorf("true branch %s", got)
	}
	if got := Cond(FromBit(Bit0), a, b).Binary(); got != "1001" {
		t.Errorf("false branch %s", got)
	}
	if got := Cond(FromBit(BitX), a, b).Binary(); got != "10xx" {
		t.Errorf("merge %s", got)
	}
	if got := Cond(FromBit(BitX), mustLit(t, "4'bz0x1"), mustLit(t, "4'bz0x1")).Binary(); got != "x0x1" {
		t.Errorf("merge of equal unknowns %s", got)
	}
	if got := Cond(FromBit(Bit1), FromUint64(4, 3), FromUint64(8, 9)); got.Width() != 8 {
		t.Errorf("result width %d", got.Width())
	}
}

func TestConcatReplicateSelect(t *testing.T) {
	c := Concat(mustLit(t, "2'b10"), mustLit(t, "3'bx01"), FromUint64(1, 1))
	if c.Binary() != "10x011" || c.Signed() || c.Width() != 6 {
		t.Errorf("Concat = %v", c)
	}
	if r := Replicate(3, mustLit(t, "2'b1z")); r.Binary() != "1z1z1z" {
		t.Errorf("Replicate = %v", r)
	}
	v := mustLit(t, "8'b1100_x01z")
	if got := Slice(v, 5, 2).Binary(); got != "00x0" {
		t.Errorf("Slice = %s", got)
	}
	if got := Slice(v, 9, 6).Binary(); got != "xx11" {
		t.Errorf("Slice past the top = %s", got)
	}
	if got := Slice(v, 1, -2).Binary(); got != "1zxx" {
		t.Errorf("Slice below the bottom = %s", got)
	}
	if got := Index(v, 0).Bit(0); got != BitZ {
		t.Errorf("Index(0) = %c", got.Char())
	}
	if got := Index(v, 99).Bit(0); got != BitX {
		t.Errorf("Index out of range = %c", got.Char())
	}
	wide := Concat(FromUint64(64, 1), FromUint64(64, 2), FromUint64(1, 1))
	canonical(t, "wide concat", wide)
	if wide.Width() != 129 || wide.Bit(65) != Bit1 || wide.Bit(2) != Bit1 || wide.Bit(0) != Bit1 || wide.Bit(128) != Bit0 {
		t.Errorf("wide concat %v", wide)
	}
}

func TestResize(t *testing.T) {
	if got := FromInt64(4, -2).Resize(8).Binary(); got != "11111110" {
		t.Errorf("sign extend %s", got)
	}
	if got := FromUint64(4, 0xE).Resize(8).Binary(); got != "00001110" {
		t.Errorf("zero extend %s", got)
	}
	if got := FromUint64(8, 0xAB).Resize(4).Binary(); got != "1011" {
		t.Errorf("truncate %s", got)
	}
	s := mustLit(t, "4'sbx001")
	if got := s.Resize(8).Binary(); got != "xxxxx001" {
		t.Errorf("signed x extends: %s", got)
	}
	if got := FromInt64(70, -1).Resize(130); got.Bit(129) != Bit1 || !got.Signed() {
		t.Errorf("wide sign extension")
	}
}

func TestClog2(t *testing.T) {
	for in, want := range map[uint64]int64{0: 0, 1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 8: 3, 9: 4, 1 << 20: 20, 1<<20 + 1: 21} {
		if got, _ := Clog2(FromUint64(64, in)).Int64(); got != want {
			t.Errorf("$clog2(%d) = %d, want %d", in, got, want)
		}
	}
	if Clog2(mustLit(t, "8'bx")).Binary() != X(32, true).Binary() {
		t.Error("$clog2(x) must be x")
	}
}

func TestRealAndString(t *testing.T) {
	for in, want := range map[float64]int64{0.4: 0, 0.5: 1, 1.5: 2, -0.5: -1, -1.5: -2, -2.4: -2, 255.5: 256} {
		if got, _ := FromFloat64(in, 16, true).Int64(); got != want {
			t.Errorf("round(%v) = %d, want %d", in, got, want)
		}
	}
	if FromFloat64(1e30, 8, false).Valid() == false {
		t.Error("a huge real wraps, it does not fail")
	}
	if got := FromFloat64(1e30, 8, false); !got.IsKnown() {
		t.Error("huge real should wrap to a known value")
	}
	for _, f := range []float64{1 / zero(), -1 / zero(), zero() / zero()} {
		if FromFloat64(f, 8, false).Binary() != "xxxxxxxx" {
			t.Errorf("%v should convert to x", f)
		}
	}
	if f, ok := FromInt64(8, -3).Float64(); !ok || f != -3 {
		t.Errorf("Float64 = %v", f)
	}
	if _, ok := mustLit(t, "4'bx").Float64(); ok {
		t.Error("x has no real value")
	}
	s := FromString("Hi!")
	if s.Width() != 24 || s.Hex() != "486921" {
		t.Errorf("FromString = %v", s)
	}
	if got, ok := s.Text(); !ok || got != "Hi!" {
		t.Errorf("Text = %q", got)
	}
	if FromString("").Width() != 8 {
		t.Error("empty string is one byte")
	}
	if got, _ := Concat(FromUint64(3, 1), FromString("a")).Text(); got != "\x01a" {
		t.Errorf("Text of a ragged width = %q", got)
	}
}

func zero() float64 { return 0 }

func TestImmutability(t *testing.T) {
	a := FromUint64(8, 0x0F)
	b := FromUint64(8, 0xF0)
	snapshot := a.Binary()
	for _, f := range []func(){
		func() { And(a, b) }, func() { Not(a) }, func() { Add(a, b) }, func() { Shl(a, b) },
		func() { a.Resize(16) }, func() { Cond(FromBit(BitX), a, b) }, func() { Concat(a, b) }, func() { a.AsSigned() },
	} {
		f()
	}
	if a.Binary() != snapshot || a.Signed() {
		t.Error("an operation changed its operand")
	}
}

// Random operations on random four-state values must keep the canonical form
// and never panic.
func TestRandomOperationsStayCanonical(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	randVal := func() Value {
		w := 1 + rng.Intn(150)
		v := New(w, rng.Intn(2) == 0)
		for i := 0; i < w; i++ {
			v.set(i, Bit(rng.Intn(4)))
		}
		return v
	}
	binary := []func(x, y Value) Value{And, Or, Xor, Xnor, Add, Sub, Mul, Div, Mod, Shl, Shr, Sshl, Sshr, Eq, Ne, CaseEq, CaseNe, WildEq, WildNe,
		Lt, Le, Gt, Ge, LogicalAnd, LogicalOr, Implies, Equiv, func(x, y Value) Value { return Concat(x, y) },
		func(x, y Value) Value { return Pow(x, y.Resize(3)) }}
	for i := 0; i < 3000; i++ {
		x, y := randVal(), randVal()
		canonical(t, "input", x)
		for _, f := range binary {
			canonical(t, "binary", f(x, y))
		}
		canonical(t, "not", Not(x))
		canonical(t, "neg", Neg(x))
		canonical(t, "cond", Cond(randVal(), x, y))
		canonical(t, "slice", Slice(x, rng.Intn(200)-20, rng.Intn(100)-20))
		canonical(t, "red", RedXor(x))
	}
}

func FuzzParseLiteral(f *testing.F) {
	for _, s := range []string{"0", "8'hFF", "'b1x0z", "4'sd9", "12'o7?7", "'x", "64'hdead_beef", "1'b", "9999999999999999999'b1", "8'd-1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseLiteral(s)
		if err == nil {
			canonical(t, s, v)
			if !v.Valid() {
				t.Fatalf("%q parsed without error to an invalid value", s)
			}
			_ = v.String()
			_ = v.Decimal()
		}
	})
}

func FuzzOperations(f *testing.F) {
	f.Add("8'hFF", "4'b1x0z", uint8(0))
	f.Add("'h1_0000_0000_0000_0000", "3'sd2", uint8(7))
	f.Fuzz(func(t *testing.T, a, b string, op uint8) {
		x, err1 := ParseLiteral(a)
		y, err2 := ParseLiteral(b)
		if err1 != nil || err2 != nil {
			return
		}
		fs := []func(x, y Value) Value{And, Or, Xor, Add, Sub, Mul, Div, Mod, Shl, Shr, Sshr, Eq, WildEq, Lt, Pow,
			func(x, y Value) Value { return Cond(x, y, y) }, func(x, y Value) Value { return Concat(x, y) },
			func(x, y Value) Value { n, _ := y.Uint64(); return Replicate(int(n%64), x) }}
		canonical(t, "result", fs[int(op)%len(fs)](x, y))
	})
}
