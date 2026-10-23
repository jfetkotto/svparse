package svint

import "math/big"

// Operands of the binary operators are first brought to a common type (LRM
// 11.8.1): the wider width, and signed only if both are signed. Operands are
// zero-extended when the result is unsigned, even if they were signed on their
// own. Operators that cannot be defined for unknown bits return x, per LRM 11.4.

// resize returns v with the given width, extending by its sign bit if signed
// and by zeros otherwise; extending a signed value whose sign bit is x or z
// extends that bit. Truncation keeps the low bits.
func (v Value) resize(width int) Value {
	if !v.Valid() {
		return v
	}
	out := raw(width, v.signed)
	if !out.Valid() {
		return out
	}
	n := min(width, v.width)
	for i := 0; i < n; i++ {
		out.set(i, v.Bit(i))
	}
	if width > v.width && v.signed {
		ext := v.Bit(v.width - 1)
		for i := v.width; i < width; i++ {
			out.set(i, ext)
		}
	}
	return out
}

// Resize returns v with the given width, sign- or zero-extended according to
// its own signedness, or truncated. The result is invalid for a bad width.
func (v Value) Resize(width int) Value { return v.resize(width) }

// coerce brings x and y to a common width and signedness.
func coerce(x, y Value) (Value, Value) {
	if !x.Valid() || !y.Valid() {
		return Value{}, Value{}
	}
	signed := x.signed && y.signed
	x.signed, y.signed = signed, signed // zero-extend unless both are signed
	w := max(x.width, y.width)
	return x.resize(w), y.resize(w)
}

// ---- bitwise (LRM 11.4.8) ----

func bitwise(x, y Value, f func(ax, bx, ay, by uint64) (a, b uint64)) Value {
	x, y = coerce(x, y)
	if !x.Valid() {
		return Value{}
	}
	out := raw(x.width, x.signed)
	for i := range out.a {
		out.a[i], out.b[i] = f(x.a[i], x.b[i], y.a[i], y.b[i])
	}
	return out.canon()
}

// And is the bitwise &: 0 & anything is 0, 1 & 1 is 1, everything else is x.
func And(x, y Value) Value {
	return bitwise(x, y, func(ax, bx, ay, by uint64) (uint64, uint64) {
		zx, zy := ^ax&^bx, ^ay&^by // known 0
		ox, oy := ax&^bx, ay&^by   // known 1
		one, zero := ox&oy, zx|zy
		unk := ^(one | zero)
		return one | unk, unk
	})
}

// Or is the bitwise |: 1 | anything is 1, 0 | 0 is 0, everything else is x.
func Or(x, y Value) Value {
	return bitwise(x, y, func(ax, bx, ay, by uint64) (uint64, uint64) {
		zx, zy := ^ax&^bx, ^ay&^by
		ox, oy := ax&^bx, ay&^by
		one, zero := ox|oy, zx&zy
		unk := ^(one | zero)
		return one | unk, unk
	})
}

// Xor is the bitwise ^: x if either bit is unknown.
func Xor(x, y Value) Value {
	return bitwise(x, y, func(ax, bx, ay, by uint64) (uint64, uint64) {
		unk := bx | by
		return (ax ^ ay) | unk, unk
	})
}

// Xnor is the bitwise ~^.
func Xnor(x, y Value) Value { return Not(Xor(x, y)) }

// Not is the bitwise ~: unknown bits become x.
func Not(x Value) Value {
	if !x.Valid() {
		return x
	}
	out := raw(x.width, x.signed)
	for i := range out.a {
		out.a[i] = ^x.a[i] | x.b[i]
		out.b[i] = x.b[i]
	}
	return out.canon()
}

// ---- reduction and logical (LRM 11.4.9, 11.4.7) ----

// Truth is the logical value of x (LRM 11.4.7): Bit1 if any bit is 1, Bit0 if
// every bit is 0, otherwise BitX.
func Truth(x Value) Bit {
	if !x.Valid() {
		return BitX
	}
	unknown := false
	for i := range x.a {
		if x.a[i]&^x.b[i] != 0 {
			return Bit1
		}
		if x.b[i] != 0 {
			unknown = true
		}
	}
	if unknown {
		return BitX
	}
	return Bit0
}

// RedAnd is the reduction &: 0 if any bit is 0, else x if any is unknown.
func RedAnd(x Value) Value {
	if !x.Valid() {
		return x
	}
	unknown := false
	for i := 0; i < x.width; i++ {
		switch x.Bit(i) {
		case Bit0:
			return FromBit(Bit0)
		case BitX, BitZ:
			unknown = true
		}
	}
	if unknown {
		return FromBit(BitX)
	}
	return FromBit(Bit1)
}

// RedOr is the reduction |: 1 if any bit is 1, else x if any is unknown.
func RedOr(x Value) Value {
	if !x.Valid() {
		return x
	}
	return FromBit(Truth(x))
}

// RedXor is the reduction ^: x if any bit is unknown, else the parity.
func RedXor(x Value) Value {
	if !x.Valid() {
		return x
	}
	if !x.IsKnown() {
		return FromBit(BitX)
	}
	return FromBool(x.OnesCount()%2 == 1)
}

// RedNand, RedNor and RedXnor invert the reductions; x stays x.
func RedNand(x Value) Value { return Not(RedAnd(x)) }
func RedNor(x Value) Value  { return Not(RedOr(x)) }
func RedXnor(x Value) Value { return Not(RedXor(x)) }

func fromTruth(b Bit) Value { return FromBit(b) }

// LogicalNot is !x.
func LogicalNot(x Value) Value {
	switch Truth(x) {
	case Bit0:
		return FromBit(Bit1)
	case Bit1:
		return FromBit(Bit0)
	}
	return FromBit(BitX)
}

// LogicalAnd is x && y: 0 if either is false, 1 if both are true, else x.
func LogicalAnd(x, y Value) Value {
	a, b := Truth(x), Truth(y)
	switch {
	case a == Bit0 || b == Bit0:
		return fromTruth(Bit0)
	case a == Bit1 && b == Bit1:
		return fromTruth(Bit1)
	}
	return fromTruth(BitX)
}

// LogicalOr is x || y: 1 if either is true, 0 if both are false, else x.
func LogicalOr(x, y Value) Value {
	a, b := Truth(x), Truth(y)
	switch {
	case a == Bit1 || b == Bit1:
		return fromTruth(Bit1)
	case a == Bit0 && b == Bit0:
		return fromTruth(Bit0)
	}
	return fromTruth(BitX)
}

// Implies is x -> y, that is !x || y.
func Implies(x, y Value) Value { return LogicalOr(LogicalNot(x), y) }

// Equiv is x <-> y, that is (x -> y) && (y -> x).
func Equiv(x, y Value) Value { return LogicalAnd(Implies(x, y), Implies(y, x)) }

// ---- arithmetic (LRM 11.4.3) ----

// arith applies f to the operands as integers, in the common type. An unknown
// bit in either operand makes the whole result x.
func arith(x, y Value, f func(a, b *big.Int) (*big.Int, bool)) Value {
	x, y = coerce(x, y)
	if !x.Valid() {
		return Value{}
	}
	a, okA := x.Big()
	b, okB := y.Big()
	if !okA || !okB {
		return X(x.width, x.signed)
	}
	r, ok := f(a, b)
	if !ok {
		return X(x.width, x.signed)
	}
	return FromBig(x.width, x.signed, r)
}

// Add is x + y.
func Add(x, y Value) Value {
	return arith(x, y, func(a, b *big.Int) (*big.Int, bool) { return a.Add(a, b), true })
}

// Sub is x - y.
func Sub(x, y Value) Value {
	return arith(x, y, func(a, b *big.Int) (*big.Int, bool) { return a.Sub(a, b), true })
}

// Mul is x * y.
func Mul(x, y Value) Value {
	return arith(x, y, func(a, b *big.Int) (*big.Int, bool) { return a.Mul(a, b), true })
}

// Div is x / y, truncating toward zero; division by zero is x.
func Div(x, y Value) Value {
	return arith(x, y, func(a, b *big.Int) (*big.Int, bool) {
		if b.Sign() == 0 {
			return nil, false
		}
		return a.Quo(a, b), true
	})
}

// Mod is x % y; the result takes the sign of x, and a zero divisor gives x.
func Mod(x, y Value) Value {
	return arith(x, y, func(a, b *big.Int) (*big.Int, bool) {
		if b.Sign() == 0 {
			return nil, false
		}
		return a.Rem(a, b), true
	})
}

// Neg is -x.
func Neg(x Value) Value {
	if !x.Valid() {
		return x
	}
	n, ok := x.Big()
	if !ok {
		return X(x.width, x.signed)
	}
	return FromBig(x.width, x.signed, n.Neg(n))
}

// Pow is x ** y (LRM 11.4.3.1). The result has x's width and signedness; y is
// read on its own, negative only if it is signed. A negative exponent gives 0
// for |x| > 1, x for a zero base, 1 for a base of 1, and +-1 for a base of -1.
func Pow(x, y Value) Value {
	if !x.Valid() || !y.Valid() {
		return Value{}
	}
	a, okA := x.Big()
	e, okE := y.Big()
	if !okA || !okE {
		return X(x.width, x.signed)
	}
	if e.Sign() < 0 {
		switch {
		case a.Sign() == 0:
			return X(x.width, x.signed)
		case a.IsInt64() && a.Int64() == 1:
			return FromInt64(x.width, 1).withSign(x.signed)
		case a.IsInt64() && a.Int64() == -1:
			if e.Bit(0) == 0 {
				return FromInt64(x.width, 1).withSign(x.signed)
			}
			return FromInt64(x.width, -1).withSign(x.signed)
		}
		return New(x.width, x.signed)
	}
	mod := new(big.Int).Lsh(big.NewInt(1), uint(x.width))
	return FromBig(x.width, x.signed, new(big.Int).Exp(a, e, mod))
}

func (v Value) withSign(signed bool) Value { v.signed = signed; return v }

// ---- shifts (LRM 11.4.10) ----

// shift moves x by amt bit positions. Vacated bits are zero, except that an
// arithmetic right shift of a signed value copies the sign bit. An unknown
// shift amount makes the result all x. The result keeps x's type.
func shift(x, amt Value, left, arith bool) Value {
	if !x.Valid() || !amt.Valid() {
		return Value{}
	}
	n, ok := amt.AsUnsigned().Big()
	if !ok {
		return X(x.width, x.signed)
	}
	out := raw(x.width, x.signed)
	fill := Bit0
	if arith && x.signed {
		fill = x.Bit(x.width - 1)
	}
	var k int
	if n.IsInt64() && n.Int64() < int64(x.width) {
		k = int(n.Int64())
	} else {
		k = x.width // everything shifts out
	}
	for i := 0; i < x.width; i++ {
		var src int
		if left {
			src = i - k
		} else {
			src = i + k
		}
		switch {
		case src >= 0 && src < x.width:
			out.set(i, x.Bit(src))
		case !left:
			out.set(i, fill)
		}
	}
	return out
}

// Shl is x << amt.
func Shl(x, amt Value) Value { return shift(x, amt, true, false) }

// Shr is x >> amt, a logical shift.
func Shr(x, amt Value) Value { return shift(x, amt, false, false) }

// Sshl is x <<< amt, which is the same as Shl.
func Sshl(x, amt Value) Value { return shift(x, amt, true, true) }

// Sshr is x >>> amt, an arithmetic shift for a signed x and logical otherwise.
func Sshr(x, amt Value) Value { return shift(x, amt, false, true) }

// ---- relational and equality (LRM 11.4.4, 11.4.5, 11.4.6) ----

func compare(x, y Value) (int, bool) {
	x, y = coerce(x, y)
	if !x.Valid() {
		return 0, false
	}
	a, okA := x.Big()
	b, okB := y.Big()
	if !okA || !okB {
		return 0, false
	}
	return a.Cmp(b), true
}

func rel(x, y Value, f func(c int) bool) Value {
	c, ok := compare(x, y)
	if !ok {
		return FromBit(BitX)
	}
	return FromBool(f(c))
}

// Lt, Le, Gt and Ge are the relational operators; they give x if either
// operand has an unknown bit.
func Lt(x, y Value) Value { return rel(x, y, func(c int) bool { return c < 0 }) }
func Le(x, y Value) Value { return rel(x, y, func(c int) bool { return c <= 0 }) }
func Gt(x, y Value) Value { return rel(x, y, func(c int) bool { return c > 0 }) }
func Ge(x, y Value) Value { return rel(x, y, func(c int) bool { return c >= 0 }) }

// Eq is the logical equality ==: 0 if some bit known in both operands
// differs, else x if any bit is unknown, else 1.
func Eq(x, y Value) Value {
	x, y = coerce(x, y)
	if !x.Valid() {
		return Value{}
	}
	unknown := false
	for i := 0; i < x.width; i++ {
		bx, by := x.Bit(i), y.Bit(i)
		if bx >= BitZ || by >= BitZ {
			unknown = true
		} else if bx != by {
			return FromBit(Bit0)
		}
	}
	if unknown {
		return FromBit(BitX)
	}
	return FromBit(Bit1)
}

// Ne is !=, the inverse of Eq.
func Ne(x, y Value) Value { return Not(Eq(x, y)) }

// CaseEq is ===: every bit, x and z included, must match; the result is 0 or 1.
func CaseEq(x, y Value) Value {
	x, y = coerce(x, y)
	if !x.Valid() {
		return Value{}
	}
	return FromBool(equalWords(x.a, y.a) && equalWords(x.b, y.b))
}

// CaseNe is !==.
func CaseNe(x, y Value) Value { return Not(CaseEq(x, y)) }

// WildEq is ==?: an x or z bit in y matches anything, while one in x against a
// known bit of y makes the result x unless another bit differs for certain.
func WildEq(x, y Value) Value {
	x, y = coerce(x, y)
	if !x.Valid() {
		return Value{}
	}
	unknown := false
	for i := 0; i < x.width; i++ {
		bx, by := x.Bit(i), y.Bit(i)
		switch {
		case by >= BitZ:
			// wildcard
		case bx >= BitZ:
			unknown = true
		case bx != by:
			return FromBit(Bit0)
		}
	}
	if unknown {
		return FromBit(BitX)
	}
	return FromBit(Bit1)
}

// WildNe is !=?.
func WildNe(x, y Value) Value { return Not(WildEq(x, y)) }

// ---- conditional, concatenation, selects (LRM 11.4.11, 11.4.12, 11.5) ----

// Cond is sel ? t : f. If sel is unknown the result is the bitwise merge of the
// operands, with x wherever they differ.
func Cond(sel, t, f Value) Value {
	t, f = coerce(t, f)
	if !t.Valid() {
		return Value{}
	}
	switch Truth(sel) {
	case Bit1:
		return t
	case Bit0:
		return f
	}
	out := raw(t.width, t.signed)
	for i := 0; i < t.width; i++ {
		if bt := t.Bit(i); bt == f.Bit(i) && bt < BitZ {
			out.set(i, bt)
		} else {
			out.set(i, BitX)
		}
	}
	return out
}

// Concat is {v0, v1, ...}: v0 is the most significant. The result is unsigned.
func Concat(vs ...Value) Value {
	total := 0
	for _, v := range vs {
		if !v.Valid() {
			return Value{}
		}
		total += v.width
		if total > MaxWidth {
			return Value{}
		}
	}
	out := raw(total, false)
	pos := total
	for _, v := range vs {
		pos -= v.width
		for i := 0; i < v.width; i++ {
			out.set(pos+i, v.Bit(i))
		}
	}
	return out
}

// Replicate is {n{v}}. The result is unsigned, and invalid for n < 1 or if it
// would exceed MaxWidth.
func Replicate(n int, v Value) Value {
	if n < 1 || !v.Valid() || v.width > MaxWidth/n {
		return Value{}
	}
	out := raw(n*v.width, false)
	for k := 0; k < n; k++ {
		for i := 0; i < v.width; i++ {
			out.set(k*v.width+i, v.Bit(i))
		}
	}
	return out
}

// Index is the bit select v[i] as a one-bit unsigned Value; x if i is out of
// range.
func Index(v Value, i int) Value {
	if !v.Valid() {
		return v
	}
	return FromBit(v.Bit(i))
}

// Slice is the part select v[msb:lsb] with msb >= lsb. Bits outside v read as
// x. The result is unsigned; it is invalid if msb < lsb or the result is too
// wide.
func Slice(v Value, msb, lsb int) Value {
	if !v.Valid() || msb < lsb || msb-lsb >= MaxWidth {
		return Value{}
	}
	out := raw(msb-lsb+1, false)
	for i := 0; i <= msb-lsb; i++ {
		out.set(i, v.Bit(lsb+i))
	}
	return out
}

// Clog2 is $clog2 (LRM 20.8.1): the ceiling of the base-2 logarithm of x read
// as unsigned, 0 for 0 and 1. The result is 32-bit signed, x if x is unknown.
func Clog2(x Value) Value {
	n, ok := x.AsUnsigned().Big()
	if !ok {
		return X(32, true)
	}
	if n.Cmp(big.NewInt(1)) <= 0 {
		return FromInt64(32, 0)
	}
	n.Sub(n, big.NewInt(1))
	return FromInt64(32, int64(n.BitLen()))
}
