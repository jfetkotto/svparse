package lexer

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func TestLexDirective(t *testing.T) {
	assertOneToken(t, "`define", token.KindDirective, "define")
	assertOneToken(t, "`ifdef", token.KindDirective, "ifdef")
	assertOneToken(t, "`include", token.KindDirective, "include")
	assertOneToken(t, "`timescale", token.KindDirective, "timescale")
}

func TestLexDirectiveTextExcludesBacktick(t *testing.T) {
	toks, _ := Lex("`FOO")
	if toks[0].Text != "FOO" {
		t.Fatalf("Text = %q, want %q (backtick excluded)", toks[0].Text, "FOO")
	}
}

func TestLexBacktickNotFollowedByIdentIsInvalid(t *testing.T) {
	toks, errs := Lex("` 1")
	assertKinds(t, toks, token.KindInvalid, token.KindIntLiteral, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestLexDirectiveThenIdentifier(t *testing.T) {
	// "`FOO" is one token; the space-separated "BAR" after it is not part
	// of the directive name.
	toks, _ := Lex("`FOO BAR")
	assertKinds(t, toks, token.KindDirective, token.KindIdent, token.KindEOF)
	if toks[0].Text != "FOO" || toks[1].Text != "BAR" {
		t.Fatalf("unexpected text: %+v", toks[:2])
	}
}

func TestLexTokenPaste(t *testing.T) {
	assertOneToken(t, "``", token.KindPaste, "``")
}

func TestLexMacroQuote(t *testing.T) {
	assertOneToken(t, "`\"", token.KindMacroQuote, "`\"")
}

func TestLexTokenPasteBetweenIdentifiers(t *testing.T) {
	toks, errs := Lex("a``b")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIdent, token.KindPaste, token.KindIdent, token.KindEOF)
}

func TestLexMacroQuotePairAroundIdentifier(t *testing.T) {
	toks, errs := Lex("`\"x`\"")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindMacroQuote, token.KindIdent, token.KindMacroQuote, token.KindEOF)
}
