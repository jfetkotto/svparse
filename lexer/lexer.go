// Package lexer scans SystemVerilog/Verilog source text into a token
// stream (see the sibling token package for the vocabulary). It is
// error-tolerant by design: unrecognized input never stops scanning or
// panics, it's tagged token.KindInvalid and lexing continues, since a
// language server has to make sense of source that's broken mid-edit far
// more often than a batch tool does.
package lexer

import (
	"fmt"

	"github.com/jfetkotto/svparse/token"
)

// Error is a non-fatal problem noticed while lexing (an unrecognized
// character, an unterminated comment or string). Lex never stops or
// panics because of one -- Errors accumulates them for a caller to
// surface as diagnostics if it wants to.
type Error struct {
	Line      int
	Character int
	Message   string
}

// Lex tokenizes src in full. The returned token slice always ends with
// exactly one token.KindEOF token, so a caller (a future parser, chiefly)
// never needs a separate end-of-input check.
func Lex(src string) ([]token.Token, []Error) {
	l := &lexer{runes: []rune(src)}
	l.run()
	return l.toks, l.errs
}

type lexer struct {
	runes []rune
	i     int
	line  int
	char  int // UTF-16 column

	toks []token.Token
	errs []Error
}

func (l *lexer) run() {
	for l.i < len(l.runes) {
		r := l.runes[l.i]
		switch {
		case isWhitespace(r):
			l.advance()
		case r == '/' && l.peek(1) == '/':
			l.skipLineComment()
		case r == '/' && l.peek(1) == '*':
			l.skipBlockComment()
		case r == '\\':
			l.scanEscapedIdent()
		case r == '$':
			l.scanDollarOrSystemIdent()
		case r == '\'' || isDecDigit(r):
			l.scanNumber()
		case r == '"':
			l.scanStringLiteral()
		case r == '`':
			l.scanDirective()
		case isIdentStart(r):
			l.scanIdentOrKeyword()
		case isOperatorStart(r):
			l.scanOperator()
		default:
			l.emitInvalid()
		}
	}
	l.emit(token.KindEOF, "", l.line, l.char)
}

// advance consumes and returns the current rune, updating line/char.
// Line numbers are zero-based; char is a UTF-16 column (see
// token.UTF16Width). A bare '\r' is treated as zero-width, insignificant
// whitespace -- it doesn't advance the column and (unlike '\n') doesn't
// advance the line either, matching how sigils's own line-splitting already
// strips '\r' rather than counting it as document content.
func (l *lexer) advance() rune {
	r := l.runes[l.i]
	l.i++
	switch r {
	case '\n':
		l.line++
		l.char = 0
	case '\r':
		// zero-width; see doc comment above.
	default:
		l.char += token.UTF16Width(r)
	}
	return r
}

// peek returns the rune at i+offset without consuming it, or 0 if that's
// out of range (0 is never a valid SV source character, so it's a safe
// non-match sentinel for the two-character lookaheads used throughout).
func (l *lexer) peek(offset int) rune {
	idx := l.i + offset
	if idx < 0 || idx >= len(l.runes) {
		return 0
	}
	return l.runes[idx]
}

func (l *lexer) emit(kind token.Kind, text string, line, char int) {
	l.toks = append(l.toks, token.Token{Kind: kind, Text: text, Line: line, Character: char})
}

func (l *lexer) errorf(line, char int, format string, args ...any) {
	l.errs = append(l.errs, Error{Line: line, Character: char, Message: fmt.Sprintf(format, args...)})
}

func (l *lexer) skipLineComment() {
	for l.i < len(l.runes) && l.runes[l.i] != '\n' {
		l.advance()
	}
}

func (l *lexer) skipBlockComment() {
	startLine, startChar := l.line, l.char
	l.advance() // '/'
	l.advance() // '*'
	for l.i < len(l.runes) {
		if l.runes[l.i] == '*' && l.peek(1) == '/' {
			l.advance() // '*'
			l.advance() // '/'
			return
		}
		l.advance()
	}
	l.errorf(startLine, startChar, "unterminated block comment")
}

// scanEscapedIdent scans a backslash-escaped identifier: '\' followed by
// a run of non-whitespace characters, terminated by (and not including)
// the next whitespace or end of input. Text is the exact source slice,
// backslash included -- callers that want just the "name" strip it
// themselves; keeping Text as the literal source form is simpler and
// matches every other token kind's Text convention.
//
// Two special cases handled before the general escaped-identifier path:
// '\' immediately followed by a newline ('\n', or '\r\n') is a line
// continuation (KindLineContinuation), not an (attempted) identifier --
// SV “ `define “ bodies use this to continue their replacement text
// onto the next line, and there'd be no other way to represent it, since
// the lexer emits no newline tokens at all. '\' followed by any other
// whitespace or EOF, with zero characters consumed, isn't a valid
// escaped identifier either (the LRM requires at least one character
// after the backslash) -- that's KindInvalid, not a bogus empty-content
// KindIdent.
func (l *lexer) scanEscapedIdent() {
	startLine, startChar := l.line, l.char
	start := l.i
	l.advance() // '\'

	if l.i < len(l.runes) && l.runes[l.i] == '\r' && l.peek(1) == '\n' {
		l.advance() // '\r'
		l.advance() // '\n'
		l.emit(token.KindLineContinuation, string(l.runes[start:l.i]), startLine, startChar)
		return
	}
	if l.i < len(l.runes) && l.runes[l.i] == '\n' {
		l.advance()
		l.emit(token.KindLineContinuation, string(l.runes[start:l.i]), startLine, startChar)
		return
	}

	for l.i < len(l.runes) && !isWhitespace(l.runes[l.i]) {
		l.advance()
	}
	if l.i == start+1 {
		l.errorf(startLine, startChar, "'\\' not followed by an identifier character")
		l.emit(token.KindInvalid, string(l.runes[start:l.i]), startLine, startChar)
		return
	}
	l.emit(token.KindIdent, string(l.runes[start:l.i]), startLine, startChar)
}

// scanDollarOrSystemIdent scans a system task/function name ($display,
// $clog2, ...): '$' immediately followed by an identifier. A '$' not
// followed by an identifier start is its own token.KindDollar token (used
// standalone, e.g. an unbounded queue/range like "q[$]").
func (l *lexer) scanDollarOrSystemIdent() {
	startLine, startChar := l.line, l.char
	start := l.i
	l.advance() // '$'
	if l.i < len(l.runes) && isIdentStart(l.runes[l.i]) {
		for l.i < len(l.runes) && isIdentChar(l.runes[l.i]) {
			l.advance()
		}
		l.emit(token.KindSystemIdent, string(l.runes[start:l.i]), startLine, startChar)
		return
	}
	l.emit(token.KindDollar, "$", startLine, startChar)
}

func (l *lexer) scanIdentOrKeyword() {
	startLine, startChar := l.line, l.char
	start := l.i
	for l.i < len(l.runes) && isIdentChar(l.runes[l.i]) {
		l.advance()
	}
	text := string(l.runes[start:l.i])
	kind := token.KindIdent
	if token.Keywords[text] {
		kind = token.KindKeyword
	}
	l.emit(kind, text, startLine, startChar)
}

func (l *lexer) emitInvalid() {
	startLine, startChar := l.line, l.char
	r := l.advance()
	l.emit(token.KindInvalid, string(r), startLine, startChar)
	l.errorf(startLine, startChar, "unrecognized character %q", r)
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// isIdentStart/isIdentChar are the SV grammar's ASCII-only identifier
// production (Annex A: [a-zA-Z_][a-zA-Z0-9_$]*), kept in the token package
// so every consumer that has to agree on where an identifier begins and
// ends -- this lexer, and anything downstream deciding what word a cursor
// sits on or whether a proposed name is writable -- shares one definition
// rather than approximating it separately. See token/ident.go.
func isIdentStart(r rune) bool { return token.IsIdentStart(r) }

func isIdentChar(r rune) bool { return token.IsIdentChar(r) }
