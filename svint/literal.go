package svint

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ParseLiteral reads an integer literal as written in source (LRM 5.7.1):
//
//	123  8'hFF  'b1010  4'sd9  16'hx_z  12'o7?7
//
// Underscores are ignored after the first digit. An unsized decimal is a signed
// 32-bit value, and an unsized based number is 32 bits, both widened if the
// digits need more (the LRM calls this "at least 32 bits"). When there are
// fewer digits than the size, the value is zero-extended, except that a
// leftmost x or z digit extends itself. More digits than the size truncate.
//
// The unbased unsized literals '0, '1, 'x and 'z parse to a one-bit value; the
// caller widens them to their context with Fill. A decimal-based literal may
// hold unknown bits only as a single x, z or ? digit.
func ParseLiteral(text string) (Value, error) {
	if text == "" {
		return Value{}, errors.New("empty literal")
	}
	tick := strings.IndexByte(text, '\'')
	if tick < 0 {
		return parseDecimal(text)
	}
	sizeText := text[:tick]
	rest := text[tick+1:]

	if sizeText == "" && len(rest) == 1 {
		switch rest[0] {
		case '0', '1':
			return FromUint64(1, uint64(rest[0]-'0')), nil
		case 'x', 'X':
			return X(1, false), nil
		case 'z', 'Z', '?':
			return Z(1, false), nil
		}
	}

	width, sized := 32, false
	if sizeText != "" {
		n, err := parseSize(sizeText)
		if err != nil {
			return Value{}, err
		}
		width, sized = n, true
	}

	signed := false
	if rest != "" && (rest[0] == 's' || rest[0] == 'S') {
		signed = true
		rest = rest[1:]
	}
	if rest == "" {
		return Value{}, errors.New("missing base in literal " + quote(text))
	}
	base := rest[0]
	digits := strings.ReplaceAll(strings.TrimSpace(rest[1:]), "_", "")
	if digits == "" {
		return Value{}, errors.New("missing digits in literal " + quote(text))
	}

	switch base {
	case 'b', 'B':
		return parsePow2(digits, 1, width, sized, signed)
	case 'o', 'O':
		return parsePow2(digits, 3, width, sized, signed)
	case 'h', 'H':
		return parsePow2(digits, 4, width, sized, signed)
	case 'd', 'D':
		return parseBasedDecimal(digits, width, sized, signed)
	}
	return Value{}, fmt.Errorf("unknown base %q in literal %s", base, quote(text))
}

func quote(s string) string { return "\"" + s + "\"" }

func parseSize(s string) (int, error) {
	s = strings.ReplaceAll(s, "_", "")
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() <= 0 {
		return 0, errors.New("invalid literal size " + quote(s))
	}
	if !n.IsInt64() || n.Int64() > MaxWidth {
		return 0, fmt.Errorf("literal size %s exceeds the maximum width %d", s, MaxWidth)
	}
	return int(n.Int64()), nil
}

// parseDecimal reads a plain decimal number: signed, at least 32 bits.
func parseDecimal(text string) (Value, error) {
	digits := strings.ReplaceAll(text, "_", "")
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok || n.Sign() < 0 || digits[0] == '+' || digits[0] == '-' {
		return Value{}, errors.New("invalid decimal literal " + quote(text))
	}
	width := max(32, n.BitLen()+1)
	if width > MaxWidth {
		return Value{}, errors.New("decimal literal too wide: " + quote(text))
	}
	return FromBig(width, true, n), nil
}

// parseBasedDecimal reads the digits of 'd. They are one number, or a single
// unknown digit that fills the whole value.
func parseBasedDecimal(digits string, width int, sized, signed bool) (Value, error) {
	if len(digits) == 1 {
		switch digits[0] {
		case 'x', 'X':
			return X(width, signed), nil
		case 'z', 'Z', '?':
			return Z(width, signed), nil
		}
	}
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok || n.Sign() < 0 || digits[0] == '+' || digits[0] == '-' {
		return Value{}, errors.New("invalid decimal digits " + quote(digits))
	}
	if !sized {
		width = max(32, n.BitLen())
		if signed {
			width = max(32, n.BitLen()+1)
		}
	}
	v := FromBig(width, signed, n)
	if !v.Valid() {
		return Value{}, errors.New("literal too wide")
	}
	return v, nil
}

// parsePow2 reads binary, octal or hexadecimal digits, bitsPer bits each.
func parsePow2(digits string, bitsPer, width int, sized, signed bool) (Value, error) {
	need := len(digits) * bitsPer
	if !sized {
		width = max(32, need)
	}
	v := raw(width, signed)
	if !v.Valid() {
		return v, errors.New("literal too wide")
	}
	// Digits are written most significant first; place the last one lowest.
	pos := 0
	for i := len(digits) - 1; i >= 0; i-- {
		val, unk, ok := digitValue(digits[i], bitsPer)
		if !ok {
			return Value{}, fmt.Errorf("invalid digit %q in literal", digits[i])
		}
		for k := 0; k < bitsPer && pos+k < width; k++ {
			if unk != Bit0 {
				v.set(pos+k, unk)
			} else {
				v.set(pos+k, Bit(val>>k&1))
			}
		}
		pos += bitsPer
	}
	// A leftmost x or z digit extends itself when the literal is wider.
	if first := digits[0]; pos < width {
		if _, unk, _ := digitValue(first, bitsPer); unk != Bit0 {
			for i := pos; i < width; i++ {
				v.set(i, unk)
			}
		}
	}
	return v, nil
}

// digitValue decodes one digit. unk is BitX or BitZ for an unknown digit, whose
// every bit it describes, and Bit0 for a known digit with value val.
func digitValue(c byte, bitsPer int) (val int, unk Bit, ok bool) {
	switch {
	case c == 'x' || c == 'X':
		return 0, BitX, true
	case c == 'z' || c == 'Z' || c == '?':
		return 0, BitZ, true
	case c >= '0' && c <= '9':
		val = int(c - '0')
	case c >= 'a' && c <= 'f':
		val = int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		val = int(c-'A') + 10
	default:
		return 0, 0, false
	}
	return val, Bit0, val < 1<<bitsPer
}
