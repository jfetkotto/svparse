package lexer

import "github.com/jfetkotto/svparse/token"

// scanNumber handles every SV numeric literal form: plain/real decimal
// numbers, based numbers ([size]'[s]base value), the unbased-unsized
// literal, and time literals (a number immediately followed, with no
// intervening whitespace, by a time unit). It's invoked whenever the
// current character is a decimal digit (the start of a size prefix or a
// plain/real number) or a bare "'" (an unsized based number, an
// unbased-unsized literal, or '{ ).
func (l *lexer) scanNumber() {
	startLine, startChar := l.line, l.char
	start := l.i

	if l.runes[l.i] == '\'' {
		l.scanTickPrefixedLiteral(startLine, startChar, start, "")
		return
	}

	l.consumeDecimalDigits()
	sizeText := string(l.runes[start:l.i])

	if l.i < len(l.runes) && l.runes[l.i] == '\'' {
		l.scanTickPrefixedLiteral(startLine, startChar, start, sizeText)
		return
	}

	isReal := l.consumeOptionalFraction()
	if l.consumeOptionalExponent() {
		isReal = true
	}

	// A time literal's mantissa can be either plain or real (10ns, 1.5ns);
	// the unit must be immediately adjacent, no whitespace, which
	// matchTimeUnit itself enforces via its trailing-boundary check.
	if _, ok := l.matchTimeUnit(); ok {
		l.emit(token.KindTimeLiteral, string(l.runes[start:l.i]), startLine, startChar)
		return
	}

	text := string(l.runes[start:l.i])
	if isReal {
		l.emit(token.KindRealLiteral, text, startLine, startChar)
	} else {
		l.emit(token.KindIntLiteral, text, startLine, startChar)
	}
}

// scanTickPrefixedLiteral handles everything starting at a "'": the
// assignment-pattern open delimiter '{, the unbased-unsized literal ('0
// '1 'x 'X 'z 'Z -- only legal without a size prefix), and based numbers
// ([s]base value). sizeText is the size digit run the caller already
// scanned ("" if this literal has no size prefix, i.e. it started at the
// "'" itself); start/startLine/startChar mark the beginning of the whole
// token, size included.
func (l *lexer) scanTickPrefixedLiteral(startLine, startChar, start int, sizeText string) {
	l.advance() // '

	if l.i < len(l.runes) && l.runes[l.i] == '{' {
		l.advance()
		l.emit(token.KindTickLBrace, "'{", startLine, startChar)
		return
	}

	if sizeText == "" && l.i < len(l.runes) && isUnbasedUnsizedDigit(l.runes[l.i]) {
		l.advance()
		l.emit(token.KindUnbasedUnsizedLiteral, string(l.runes[start:l.i]), startLine, startChar)
		return
	}

	if l.i < len(l.runes) && (l.runes[l.i] == 's' || l.runes[l.i] == 'S') {
		l.advance()
	}
	if l.i >= len(l.runes) || !isBaseLetter(l.runes[l.i]) {
		text := string(l.runes[start:l.i])
		if text == "'" && l.i < len(l.runes) && l.runes[l.i] == '(' {
			// The cast operator, "type'(expr)" -- a bare "'" immediately
			// followed by "(", not a based number at all. Emitted as its
			// own token (KindTick); the "(" that follows is picked up
			// normally by the next call into the dispatch loop.
			l.emit(token.KindTick, text, startLine, startChar)
			return
		}
		l.errorf(startLine, startChar, "malformed numeric literal %q", text)
		l.emit(token.KindInvalid, text, startLine, startChar)
		return
	}
	base := l.runes[l.i]
	l.advance()
	l.consumeBaseDigits(base)
	l.emit(token.KindIntLiteral, string(l.runes[start:l.i]), startLine, startChar)
}

func (l *lexer) consumeOptionalFraction() bool {
	if l.i < len(l.runes) && l.runes[l.i] == '.' && l.i+1 < len(l.runes) && isDecDigit(l.runes[l.i+1]) {
		l.advance() // '.'
		l.consumeDecimalDigits()
		return true
	}
	return false
}

func (l *lexer) consumeOptionalExponent() bool {
	if l.i >= len(l.runes) || (l.runes[l.i] != 'e' && l.runes[l.i] != 'E') {
		return false
	}
	save, saveLine, saveChar := l.i, l.line, l.char
	l.advance() // e/E
	if l.i < len(l.runes) && (l.runes[l.i] == '+' || l.runes[l.i] == '-') {
		l.advance()
	}
	if l.i < len(l.runes) && isDecDigit(l.runes[l.i]) {
		l.consumeDecimalDigits()
		return true
	}
	// Not actually an exponent (e.g. a bare "3e" or "3e+" with no digits
	// after) -- back out so "e"/"e+" is left for the next token, not
	// silently swallowed into a malformed number.
	l.i, l.line, l.char = save, saveLine, saveChar
	return false
}

// matchTimeUnit checks for one of the six SV time units immediately at
// the cursor (no whitespace before it, since the caller only calls this
// right after scanning a number) and, if found, consumes it -- but only
// if the character right after the unit isn't itself an identifier
// character, so "10nsx" doesn't get misread as the time literal "10ns"
// followed by a stray "x"; it's left as the plain number "10" followed by
// the identifier "nsx" instead.
func (l *lexer) matchTimeUnit() (string, bool) {
	for _, unit := range []string{"ms", "us", "ns", "ps", "fs", "s"} {
		n := len(unit)
		matched := true
		for k := range n {
			if l.peek(k) != rune(unit[k]) {
				matched = false
				break
			}
		}
		if !matched || isIdentChar(l.peek(n)) {
			continue
		}
		for range n {
			l.advance()
		}
		return unit, true
	}
	return "", false
}

func (l *lexer) consumeDecimalDigits() {
	for l.i < len(l.runes) && (isDecDigit(l.runes[l.i]) || l.runes[l.i] == '_') {
		l.advance()
	}
}

// consumeBaseDigits consumes value digits (plus '_' separators) valid for
// base (one of b/B o/O d/D h/H). It doesn't enforce every LRM well-
// formedness rule -- e.g. a decimal-base value is technically only
// allowed to be a plain digit run OR a single x/z digit, never a mix --
// that's a semantic well-formedness check, not a lexical one, and out of
// scope for this package (see the "purely syntactic" design principle).
func (l *lexer) consumeBaseDigits(base rune) {
	valid := baseDigit(base)
	for l.i < len(l.runes) && (l.runes[l.i] == '_' || valid(l.runes[l.i])) {
		l.advance()
	}
}

func baseDigit(base rune) func(rune) bool {
	switch base {
	case 'b', 'B':
		return func(r rune) bool { return r == '0' || r == '1' || isXZQ(r) }
	case 'o', 'O':
		return func(r rune) bool { return (r >= '0' && r <= '7') || isXZQ(r) }
	case 'h', 'H':
		return func(r rune) bool {
			return isDecDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || isXZQ(r)
		}
	default: // 'd', 'D'
		return func(r rune) bool { return isDecDigit(r) || isXZQ(r) }
	}
}

func isXZQ(r rune) bool {
	switch r {
	case 'x', 'X', 'z', 'Z', '?':
		return true
	}
	return false
}

func isDecDigit(r rune) bool { return r >= '0' && r <= '9' }

func isUnbasedUnsizedDigit(r rune) bool {
	switch r {
	case '0', '1', 'x', 'X', 'z', 'Z':
		return true
	}
	return false
}

func isBaseLetter(r rune) bool {
	switch r {
	case 'b', 'B', 'o', 'O', 'd', 'D', 'h', 'H':
		return true
	}
	return false
}
