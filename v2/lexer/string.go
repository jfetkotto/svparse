package lexer

import "github.com/jfetkotto/svparse/token"

// scanStringLiteral scans a double-quoted string, handling SV's escape
// sequences: \n \t \\ \" \v \f \a (single-character escapes, all handled
// identically -- just consume the one character after the backslash),
// \ooo (up to 3 octal digits), \xHH (up to 2 hex digits), and a trailing
// backslash immediately before a newline (LF or CRLF -- see advance's own
// "'\r' is zero-width" handling) as a line continuation (the newline is
// consumed as part of the escape, so it doesn't end the string). Any
// other character following a backslash is tolerated the same way as the
// single-character escapes -- not a recognized SV escape, but not worth
// failing the whole literal over either.
func (l *lexer) scanStringLiteral() {
	startLine, startChar := l.line, l.char
	start := l.i
	l.advance() // opening '"'

	for l.i < len(l.runes) {
		switch l.runes[l.i] {
		case '"':
			l.advance()
			l.emit(token.KindStringLiteral, string(l.runes[start:l.i]), startLine, startChar)
			return
		case '\n':
			// An unescaped newline inside a string isn't legal SV. Don't
			// consume it -- leave it for the main loop's ordinary newline
			// handling so line/column tracking doesn't skew, and report
			// what we have as unterminated.
			l.errorf(startLine, startChar, "unterminated string literal")
			l.emit(token.KindStringLiteral, string(l.runes[start:l.i]), startLine, startChar)
			return
		case '\\':
			l.scanStringEscape()
		default:
			l.advance()
		}
	}

	l.errorf(startLine, startChar, "unterminated string literal")
	l.emit(token.KindStringLiteral, string(l.runes[start:l.i]), startLine, startChar)
}

func (l *lexer) scanStringEscape() {
	l.advance() // backslash
	if l.i >= len(l.runes) {
		return
	}
	switch {
	case l.runes[l.i] == '\r':
		l.advance() // '\r' -- zero-width, per advance's own CR handling
		if l.i < len(l.runes) && l.runes[l.i] == '\n' {
			l.advance() // line continuation (CRLF form)
		}
	case l.runes[l.i] == '\n':
		l.advance() // line continuation (LF form)
	case isOctalDigit(l.runes[l.i]):
		for k := 0; k < 3 && l.i < len(l.runes) && isOctalDigit(l.runes[l.i]); k++ {
			l.advance()
		}
	case l.runes[l.i] == 'x':
		l.advance()
		for k := 0; k < 2 && l.i < len(l.runes) && isHexDigit(l.runes[l.i]); k++ {
			l.advance()
		}
	default:
		// n, t, \\, ", v, f, a, or any unrecognized escape: all handled
		// the same way, consuming exactly the one character.
		l.advance()
	}
}

func isOctalDigit(r rune) bool { return r >= '0' && r <= '7' }

func isHexDigit(r rune) bool {
	return isDecDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
