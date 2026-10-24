package lexer

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func TestLexSimpleStringLiteral(t *testing.T) {
	assertOneToken(t, `"hello"`, token.KindStringLiteral, `"hello"`)
}

func TestLexStringLiteralWithEscapes(t *testing.T) {
	src := `"a\nb\tc\\d\"e"`
	assertOneToken(t, src, token.KindStringLiteral, src)
}

func TestLexStringLiteralOctalEscape(t *testing.T) {
	assertOneToken(t, `"\101"`, token.KindStringLiteral, `"\101"`)
}

func TestLexStringLiteralHexEscape(t *testing.T) {
	assertOneToken(t, `"\x41"`, token.KindStringLiteral, `"\x41"`)
}

func TestLexStringLiteralLineContinuation(t *testing.T) {
	toks, errs := Lex("\"a\\\nb\"")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindStringLiteral, token.KindEOF)
	if toks[0].Text != "\"a\\\nb\"" {
		t.Fatalf("unexpected text: %q", toks[0].Text)
	}
	// The string literal spans two lines, so whatever follows it should
	// resume position tracking correctly. Confirm via a follow-up token.
	toks2, _ := Lex("\"a\\\nb\" c")
	if toks2[1].Line != 1 {
		t.Fatalf("expected the trailing identifier on line 1 after the continued string, got %+v", toks2[1])
	}
}

func TestLexStringLiteralLineContinuationCRLF(t *testing.T) {
	toks, errs := Lex("\"a\\\r\nb\"")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindStringLiteral, token.KindEOF)
	if toks[0].Text != "\"a\\\r\nb\"" {
		t.Fatalf("unexpected text: %q", toks[0].Text)
	}
	toks2, _ := Lex("\"a\\\r\nb\" c")
	if toks2[1].Line != 1 {
		t.Fatalf("expected the trailing identifier on line 1 after the continued string, got %+v", toks2[1])
	}
}

func TestLexUnterminatedStringAtEOF(t *testing.T) {
	toks, errs := Lex(`"never closed`)
	assertKinds(t, toks, token.KindStringLiteral, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestLexUnterminatedStringAtNewline(t *testing.T) {
	toks, errs := Lex("\"never closed\nbar")
	// The unclosed string stops at the newline; "bar" on the next line
	// lexes normally afterward, and the newline itself still advances the
	// line counter correctly (not swallowed into the string).
	assertKinds(t, toks, token.KindStringLiteral, token.KindIdent, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
	if toks[1].Line != 1 {
		t.Fatalf("expected bar on line 1, got %+v", toks[1])
	}
}
