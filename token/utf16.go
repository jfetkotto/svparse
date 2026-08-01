// Package token holds the lexer's token kinds and shared position
// helpers. It exists as its own package (rather than living inside a
// future parser package) because both a lexer and anything downstream of
// it need to agree on how positions are represented.
package token

// LSP (and most editor protocols) count character offsets in UTF-16 code
// units, not runes. ASCII and every other BMP character agree between
// the two encodings; supplementary-plane characters (an emoji, say) are
// where they diverge, 1 rune but 2 UTF-16 units -- and where a
// rune-counted column would mis-place every position after it on the
// same line. Consumers that hand positions back to an editor (e.g. as
// part of a text edit) need those positions to be UTF-16-correct or the
// edit lands in the wrong place.

// UTF16Width returns r's width in UTF-16 code units: 2 for a
// supplementary-plane character (outside the Basic Multilingual Plane,
// U+10000 and above), 1 otherwise.
func UTF16Width(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// UTF16Len returns s's length in UTF-16 code units.
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += UTF16Width(r)
	}
	return n
}

// RuneColumn converts a UTF-16 column into an index into lineText's
// runes, clamping past-the-end (or mid-surrogate-pair) positions to the
// nearest boundary at or before them.
func RuneColumn(lineText []rune, utf16Col int) int {
	col := 0
	for i, r := range lineText {
		if col >= utf16Col {
			return i
		}
		col += UTF16Width(r)
	}
	return len(lineText)
}

// UTF16Column is RuneColumn's inverse: the UTF-16 column at which
// lineText's runeIdx-th rune starts.
func UTF16Column(lineText []rune, runeIdx int) int {
	col := 0
	for i, r := range lineText {
		if i >= runeIdx {
			break
		}
		col += UTF16Width(r)
	}
	return col
}
