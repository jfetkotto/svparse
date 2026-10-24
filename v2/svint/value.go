// Package svint implements SystemVerilog's integral values: arbitrary-width,
// four-state (0, 1, x, z) bit vectors with the operators of LRM 11 and the
// literal syntax of LRM 5.7.1. It is what constant evaluation and elaboration
// compute with.
//
// A Value is immutable: every operation returns a new Value. A Value is
// invalid when its width is zero, which is what the zero Value, an out-of-range
// width and any operation on an invalid operand produce, so callers can chain
// operations and check Valid once at the end. Nothing in the package panics.
package svint

import (
	"math/big"
	"math/bits"
	"strings"
)

// MaxWidth bounds the width of a Value. SystemVerilog sets no limit, but an
// unchecked "{1000000000{x}}" must not exhaust memory; a request beyond the
// limit yields an invalid Value.
const MaxWidth = 1 << 20

// Bit is one four-state bit.
type Bit uint8

// The four bit states. The order is the encoding used internally: the low bit
// is the "value" plane, the next the "unknown" plane, so x is 1 in both.
const (
	Bit0 Bit = iota
	Bit1
	BitZ
	BitX
)

// Char returns the bit's character in a literal: '0', '1', 'z' or 'x'.
func (b Bit) Char() byte { return "01zx"[b&3] }

// Value is a four-state integral value of fixed width and signedness (LRM 6.11).
// The planes a and b hold, for each bit, its value and its "unknown" flag: a
// known bit has b = 0; z is (a=0, b=1) and x is (a=1, b=1). Bits at and above
// the width are always zero in both planes, so structurally equal Values are
// equal as slices.
type Value struct {
	width  int
	signed bool
	a, b   []uint64
}

func nwords(width int) int { return (width + 63) / 64 }

// raw returns an all-zero Value, or the invalid Value for a bad width.
func raw(width int, signed bool) Value {
	if width < 1 || width > MaxWidth {
		return Value{}
	}
	n := nwords(width)
	return Value{width: width, signed: signed, a: make([]uint64, n), b: make([]uint64, n)}
}

// canon clears the bits above the width. v must be a Value built by raw.
func (v Value) canon() Value {
	if v.width == 0 {
		return v
	}
	if rem := v.width % 64; rem != 0 {
		m := uint64(1)<<rem - 1
		v.a[len(v.a)-1] &= m
		v.b[len(v.b)-1] &= m
	}
	return v
}

// New returns the zero Value of the given width.
func New(width int, signed bool) Value { return raw(width, signed) }

// FromUint64 returns an unsigned Value of the given width holding x, truncated
// to the width.
func FromUint64(width int, x uint64) Value {
	v := raw(width, false)
	if v.width == 0 {
		return v
	}
	v.a[0] = x
	return v.canon()
}

// FromInt64 returns a signed Value of the given width holding x, sign-extended
// or truncated to the width.
func FromInt64(width int, x int64) Value {
	v := raw(width, true)
	if v.width == 0 {
		return v
	}
	for i := range v.a {
		if x < 0 {
			v.a[i] = ^uint64(0)
		}
	}
	v.a[0] = uint64(x)
	return v.canon()
}

// FromBig returns a Value of the given width and signedness holding x modulo
// 2^width (two's complement for a negative x).
func FromBig(width int, signed bool, x *big.Int) Value {
	v := raw(width, signed)
	if v.width == 0 || x == nil {
		return v
	}
	m := new(big.Int).Lsh(big.NewInt(1), uint(width))
	r := new(big.Int).Mod(x, m) // Mod is Euclidean: never negative
	for i, w := range r.Bits() {
		if i < len(v.a) {
			v.a[i] = uint64(w)
		}
	}
	return v.canon()
}

// Fill returns a Value of the given width with every bit set to b. It is how
// the unbased unsized literals '0, '1, 'x and 'z take the width of their
// context (LRM 5.7.1).
func Fill(b Bit, width int, signed bool) Value {
	v := raw(width, signed)
	if v.width == 0 {
		return v
	}
	for i := range v.a {
		if b&1 != 0 {
			v.a[i] = ^uint64(0)
		}
		if b&2 != 0 {
			v.b[i] = ^uint64(0)
		}
	}
	return v.canon()
}

// X returns a Value of all x bits.
func X(width int, signed bool) Value { return Fill(BitX, width, signed) }

// Z returns a Value of all z bits.
func Z(width int, signed bool) Value { return Fill(BitZ, width, signed) }

// FromBool returns the one-bit unsigned Value 1 for true and 0 for false.
func FromBool(t bool) Value {
	if t {
		return FromUint64(1, 1)
	}
	return FromUint64(1, 0)
}

// FromBit returns b as a one-bit unsigned Value.
func FromBit(b Bit) Value { return Fill(b, 1, false) }

// FromString returns the value of a string literal (LRM 5.9): eight bits per
// character, the first character in the most significant byte. An empty string
// is the one-byte zero, as the LRM specifies.
func FromString(s string) Value {
	if s == "" {
		return FromUint64(8, 0)
	}
	v := raw(8*len(s), false)
	for i := 0; i < len(s); i++ {
		bit := 8 * (len(s) - 1 - i)
		v.a[bit/64] |= uint64(s[i]) << (bit % 64)
	}
	return v
}

// Valid reports whether v is a real Value, not the result of a bad width or of
// an operation on an invalid operand.
func (v Value) Valid() bool { return v.width > 0 }

// Width returns the number of bits, zero for an invalid Value.
func (v Value) Width() int { return v.width }

// Signed reports whether v is signed.
func (v Value) Signed() bool { return v.signed }

// AsSigned returns v reinterpreted as signed, without changing any bit.
func (v Value) AsSigned() Value { v.signed = true; return v }

// AsUnsigned returns v reinterpreted as unsigned, without changing any bit.
func (v Value) AsUnsigned() Value { v.signed = false; return v }

// Bit returns bit i, where bit 0 is the least significant. An index outside the
// value reads as x (LRM 11.5.1).
func (v Value) Bit(i int) Bit {
	if i < 0 || i >= v.width {
		return BitX
	}
	w, s := i/64, uint(i%64)
	return Bit((v.a[w]>>s)&1 | ((v.b[w]>>s)&1)<<1)
}

// set writes bit i of a Value under construction.
func (v Value) set(i int, b Bit) {
	w, s := i/64, uint(i%64)
	v.a[w] = v.a[w]&^(1<<s) | uint64(b&1)<<s
	v.b[w] = v.b[w]&^(1<<s) | uint64(b>>1&1)<<s
}

// IsKnown reports whether v is valid and has no x or z bit.
func (v Value) IsKnown() bool {
	if v.width == 0 {
		return false
	}
	for _, w := range v.b {
		if w != 0 {
			return false
		}
	}
	return true
}

// Identical reports whether a and b have the same width, signedness and bits,
// x and z included. It is Go-level equality, not the === operator (which
// ignores signedness; see CaseEq).
func Identical(a, b Value) bool {
	if a.width != b.width || a.signed != b.signed {
		return false
	}
	return equalWords(a.a, b.a) && equalWords(a.b, b.b)
}

func equalWords(x, y []uint64) bool {
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// Uint64 returns v as an unsigned integer. It fails if v has an x or z bit or
// does not fit in 64 bits.
func (v Value) Uint64() (uint64, bool) {
	if !v.IsKnown() {
		return 0, false
	}
	for _, w := range v.a[1:] {
		if w != 0 {
			return 0, false
		}
	}
	return v.a[0], true
}

// Int64 returns v as a signed integer, reading it as two's complement if v is
// signed. It fails if v has an x or z bit or the number does not fit.
func (v Value) Int64() (int64, bool) {
	n, ok := v.Big()
	if !ok || !n.IsInt64() {
		return 0, false
	}
	return n.Int64(), true
}

// Big returns v as an integer, two's complement if v is signed. It fails if v
// has an x or z bit.
func (v Value) Big() (*big.Int, bool) {
	if !v.IsKnown() {
		return nil, false
	}
	n := new(big.Int)
	words := make([]big.Word, len(v.a))
	for i, w := range v.a {
		words[i] = big.Word(w)
	}
	n.SetBits(words)
	if v.signed && v.Bit(v.width-1) == Bit1 {
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(v.width)))
	}
	return n, true
}

// Binary returns the bits as a string of 0, 1, x and z, most significant first.
func (v Value) Binary() string {
	var sb strings.Builder
	for i := v.width - 1; i >= 0; i-- {
		sb.WriteByte(v.Bit(i).Char())
	}
	return sb.String()
}

// Hex returns the value as hexadecimal digits, most significant first, with
// the leading zeros of the full width kept. A digit is x or z when all four of
// its bits are, and X or Z when only some are unknown (X if any is x).
func (v Value) Hex() string {
	var sb strings.Builder
	for lo := (v.width - 1) / 4 * 4; lo >= 0; lo -= 4 {
		sb.WriteByte(v.digit(lo, 4))
	}
	return sb.String()
}

// Octal is Hex for base 8.
func (v Value) Octal() string {
	var sb strings.Builder
	for lo := (v.width - 1) / 3 * 3; lo >= 0; lo -= 3 {
		sb.WriteByte(v.digit(lo, 3))
	}
	return sb.String()
}

// digit renders the n bits starting at lo as one digit of a 2^n base.
func (v Value) digit(lo, n int) byte {
	val, nx, nz := 0, 0, 0
	for i := 0; i < n; i++ {
		switch b := v.bitOrZero(lo + i); b {
		case Bit1:
			val |= 1 << i
		case BitX:
			nx++
		case BitZ:
			nz++
		}
	}
	switch {
	case nx == n:
		return 'x'
	case nz == n:
		return 'z'
	case nx > 0:
		return 'X'
	case nz > 0:
		return 'Z'
	}
	return "0123456789abcdef"[val]
}

// bitOrZero is Bit, but reads the bits above the width as the padding of the
// top digit of Hex and Octal: 0, or the top bit itself if that is x or z.
func (v Value) bitOrZero(i int) Bit {
	if i >= v.width {
		if top := v.Bit(v.width - 1); top >= BitZ {
			return top
		}
		return Bit0
	}
	return v.Bit(i)
}

// Decimal returns the value in decimal (negative if v is signed and its top
// bit is set). A value with unknown bits prints as a single x, z, X or Z by the
// rules of Hex applied to the whole value (LRM 21.2.1.2).
func (v Value) Decimal() string {
	if !v.Valid() {
		return ""
	}
	if n, ok := v.Big(); ok {
		return n.String()
	}
	nx, nz := 0, 0
	for i := 0; i < v.width; i++ {
		switch v.Bit(i) {
		case BitX:
			nx++
		case BitZ:
			nz++
		}
	}
	switch {
	case nx == v.width:
		return "x"
	case nz == v.width:
		return "z"
	case nx > 0:
		return "X"
	}
	return "Z"
}

// String returns v as a sized literal, such as 8'hff or 4'sb10x0.
func (v Value) String() string {
	if !v.Valid() {
		return "<invalid>"
	}
	s := "'"
	if v.signed {
		s = "'s"
	}
	digits := v.Hex()
	base := "h"
	if v.width%4 != 0 || !v.IsKnown() {
		digits, base = v.Binary(), "b" // hex would blur which bits are x or z
	}
	return itoa(v.width) + s + base + digits
}

func itoa(n int) string { return big.NewInt(int64(n)).String() }

// OnesCount returns the number of bits that are 1.
func (v Value) OnesCount() int {
	n := 0
	for i, w := range v.a {
		n += bits.OnesCount64(w &^ v.b[i])
	}
	return n
}
