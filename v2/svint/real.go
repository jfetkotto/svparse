package svint

import (
	"math"
	"math/big"
)

// FromFloat64 converts a real to an integral Value of the given width and
// signedness, rounding to the nearest integer with ties away from zero and
// wrapping modulo 2^width (LRM 6.12.2). A NaN or infinity converts to all x.
func FromFloat64(f float64, width int, signed bool) Value {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return X(width, signed)
	}
	n, _ := new(big.Float).SetFloat64(math.Round(f)).Int(nil)
	return FromBig(width, signed, n)
}

// Float64 converts v to a real, reading it as two's complement if signed. It
// fails if v has an x or z bit.
func (v Value) Float64() (float64, bool) {
	n, ok := v.Big()
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).SetInt(n).Float64()
	return f, true
}

// Text returns v as a string literal value (LRM 5.9): each byte of the value
// is a character, most significant first, with NUL bytes omitted as the LRM
// specifies for string conversion. A width that is not a multiple of eight is
// padded with zeros on the left. It fails if v has an unknown bit.
func (v Value) Text() (string, bool) {
	if !v.IsKnown() {
		return "", false
	}
	var out []byte
	for hi := (v.width+7)/8*8 - 8; hi >= 0; hi -= 8 {
		var c byte
		for i := 0; i < 8; i++ {
			if v.bitOrZero(hi+i) == Bit1 {
				c |= 1 << i
			}
		}
		if c != 0 {
			out = append(out, c)
		}
	}
	return string(out), true
}
