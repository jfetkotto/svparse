package lexer

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func assertOneToken(t *testing.T, src string, wantKind token.Kind, wantText string) {
	t.Helper()
	toks, errs := Lex(src)
	if len(toks) != 2 { // literal + EOF
		t.Fatalf("Lex(%q) = %+v, want exactly one literal token + EOF", src, toks)
	}
	if toks[0].Kind != wantKind || toks[0].Text != wantText {
		t.Fatalf("Lex(%q)[0] = %+v, want Kind=%v Text=%q", src, toks[0], wantKind, wantText)
	}
	if len(errs) != 0 {
		t.Fatalf("Lex(%q) unexpected errors: %+v", src, errs)
	}
}

func TestLexPlainDecimalInt(t *testing.T) {
	assertOneToken(t, "42", token.KindIntLiteral, "42")
}

func TestLexDecimalIntWithUnderscores(t *testing.T) {
	assertOneToken(t, "1_000_000", token.KindIntLiteral, "1_000_000")
}

func TestLexRealNumberFraction(t *testing.T) {
	assertOneToken(t, "3.14", token.KindRealLiteral, "3.14")
}

func TestLexRealNumberExponentOnly(t *testing.T) {
	assertOneToken(t, "1e10", token.KindRealLiteral, "1e10")
}

func TestLexRealNumberExponentWithSign(t *testing.T) {
	assertOneToken(t, "1.5e-10", token.KindRealLiteral, "1.5e-10")
	assertOneToken(t, "1.5E+10", token.KindRealLiteral, "1.5E+10")
}

func TestLexBareTrailingEIsNotConsumedAsExponent(t *testing.T) {
	// "3e" with nothing after the 'e' isn't a valid exponent -- the number
	// ends at "3" and "e" becomes its own (separate) identifier token.
	toks, _ := Lex("3e")
	assertKinds(t, toks, token.KindIntLiteral, token.KindIdent, token.KindEOF)
	if toks[0].Text != "3" || toks[1].Text != "e" {
		t.Fatalf("unexpected split: %+v", toks[:2])
	}
}

func TestLexBasedNumberSizedHex(t *testing.T) {
	assertOneToken(t, "8'hFF", token.KindIntLiteral, "8'hFF")
}

func TestLexBasedNumberSizedBinaryWithDontCare(t *testing.T) {
	assertOneToken(t, "4'b10x1", token.KindIntLiteral, "4'b10x1")
}

func TestLexBasedNumberUnsized(t *testing.T) {
	assertOneToken(t, "'h1F", token.KindIntLiteral, "'h1F")
}

func TestLexBasedNumberSigned(t *testing.T) {
	assertOneToken(t, "8'sh1F", token.KindIntLiteral, "8'sh1F")
	assertOneToken(t, "'sd5", token.KindIntLiteral, "'sd5")
}

func TestLexBasedNumberOctalAndDecimal(t *testing.T) {
	assertOneToken(t, "3'o7", token.KindIntLiteral, "3'o7")
	assertOneToken(t, "8'd255", token.KindIntLiteral, "8'd255")
}

func TestLexBasedNumberWithUnderscoreSeparators(t *testing.T) {
	assertOneToken(t, "16'hDEAD_BEEF", token.KindIntLiteral, "16'hDEAD_BEEF")
}

func TestLexUnbasedUnsizedLiterals(t *testing.T) {
	for _, lit := range []string{"'0", "'1", "'x", "'X", "'z", "'Z"} {
		assertOneToken(t, lit, token.KindUnbasedUnsizedLiteral, lit)
	}
}

func TestLexAssignmentPatternOpenNotConfusedWithLiteral(t *testing.T) {
	assertOneToken(t, "'{", token.KindTickLBrace, "'{")
}

func TestLexCastOperatorTickIsNotAMalformedLiteral(t *testing.T) {
	// "int'(2.1 * 3.7)" -- the cast operator, "type'(expr)". A bare "'"
	// immediately followed by "(" (no base letter, no unbased-unsized
	// digit, no "{") is this operator, not a malformed number: emitted as
	// its own KindTick token, with the "(" that follows lexed normally,
	// and no error recorded.
	toks, errs := Lex("int'(2)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindKeyword, token.KindTick, token.KindLParen, token.KindIntLiteral, token.KindRParen, token.KindEOF)
	if toks[1].Text != "'" {
		t.Fatalf("expected the tick token's text to be %q, got %q", "'", toks[1].Text)
	}
}

func TestLexMalformedTickLiteralIsInvalidWithError(t *testing.T) {
	toks, errs := Lex("'q")
	// "'" isn't followed by a base letter, an unbased-unsized digit, or
	// '{ -- malformed. "q" itself then lexes as a separate identifier.
	assertKinds(t, toks, token.KindInvalid, token.KindIdent, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected one error for the malformed literal, got %+v", errs)
	}
}

func TestLexSizedLiteralRequiresBaseLetter(t *testing.T) {
	// "8'1" isn't valid SV (a sized literal needs a base letter) -- must
	// not be silently misread as an unbased-unsized literal. The "'" is
	// consumed as part of the malformed literal; "1" (not a base letter)
	// is left for the next token, which becomes its own IntLiteral.
	toks, errs := Lex("8'1")
	assertKinds(t, toks, token.KindInvalid, token.KindIntLiteral, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
	if toks[0].Text != "8'" {
		t.Fatalf("unexpected invalid-token text: %q", toks[0].Text)
	}
	if toks[1].Text != "1" {
		t.Fatalf("unexpected trailing token text: %q", toks[1].Text)
	}
}

func TestLexTimeLiteralAllUnits(t *testing.T) {
	for _, unit := range []string{"s", "ms", "us", "ns", "ps", "fs"} {
		assertOneToken(t, "10"+unit, token.KindTimeLiteral, "10"+unit)
	}
}

func TestLexTimeLiteralRealMantissa(t *testing.T) {
	assertOneToken(t, "1.5ns", token.KindTimeLiteral, "1.5ns")
}

func TestLexTimeUnitRequiresNoWhitespace(t *testing.T) {
	// "10 ns" (a space between) is NOT a time literal -- it's a plain
	// number followed by a separate identifier.
	toks, _ := Lex("10 ns")
	assertKinds(t, toks, token.KindIntLiteral, token.KindIdent, token.KindEOF)
	if toks[0].Text != "10" || toks[1].Text != "ns" {
		t.Fatalf("unexpected split: %+v", toks[:2])
	}
}

func TestLexTimeUnitBoundaryCheckAvoidsGreedyMisread(t *testing.T) {
	// "10nsx" -- "nsx" isn't a real time unit; "ns" must not be greedily
	// matched and split off from the "x" that follows it.
	toks, _ := Lex("10nsx")
	assertKinds(t, toks, token.KindIntLiteral, token.KindIdent, token.KindEOF)
	if toks[0].Text != "10" || toks[1].Text != "nsx" {
		t.Fatalf("unexpected split: %+v", toks[:2])
	}
}

func TestLexNumberThenDotOperatorNotMisreadAsReal(t *testing.T) {
	// "8.f" -- '.' not immediately followed by a digit means this isn't a
	// fractional part; "8", ".", and "f" are three separate tokens.
	toks, _ := Lex("8.f")
	assertKinds(t, toks, token.KindIntLiteral, token.KindDot, token.KindIdent, token.KindEOF)
	if toks[0].Text != "8" {
		t.Fatalf("expected the number to stop at \"8\", got %+v", toks[0])
	}
}
