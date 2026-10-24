package lexer

import "github.com/jfetkotto/svparse/token"

// scanDirective scans whatever follows a backtick: a compiler directive
// (a backtick immediately followed by an identifier -- “ `define“,
// “ `ifdef“, “ `include“, “ `timescale“, ...; Text excludes the
// backtick), the macro token-paste operator (two adjacent backticks,
// KindPaste), or a macro stringize delimiter (“ `" “, KindMacroQuote).
// None of these is
// interpreted here -- a preprocessor consumes them from the token stream
// this lexer produces (expanding a directive/macro reference, or
// resolving a paste/stringize pair during expansion -- see
// preprocessor.substitute).
func (l *lexer) scanDirective() {
	startLine, startChar := l.line, l.char
	l.advance() // '`'

	if l.i < len(l.runes) && l.runes[l.i] == '`' {
		l.advance()
		l.emit(token.KindPaste, "``", startLine, startChar)
		return
	}
	if l.i < len(l.runes) && l.runes[l.i] == '"' {
		l.advance()
		l.emit(token.KindMacroQuote, "`\"", startLine, startChar)
		return
	}

	if l.i >= len(l.runes) || !isIdentStart(l.runes[l.i]) {
		l.errorf(startLine, startChar, "'`' not followed by a directive name")
		l.emit(token.KindInvalid, "`", startLine, startChar)
		return
	}

	start := l.i
	for l.i < len(l.runes) && isIdentChar(l.runes[l.i]) {
		l.advance()
	}
	l.emit(token.KindDirective, string(l.runes[start:l.i]), startLine, startChar)
}
