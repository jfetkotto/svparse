package lexer

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/token"
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

func TestLexSizeCastSplitsIntoLiteralAndTick(t *testing.T) {
	// "8'(x)" is a size cast (LRM 6.24.1): the size is a plain integer
	// literal and the tick is the cast operator, not a malformed based
	// number.
	toks, errs := Lex("8'(x)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIntLiteral, token.KindTick, token.KindLParen, token.KindIdent, token.KindRParen, token.KindEOF)
	if toks[0].Text != "8" || toks[0].Character != 0 {
		t.Errorf("size token = %+v, want text 8 at column 0", toks[0])
	}
	if toks[1].Text != "'" || toks[1].Character != 1 {
		t.Errorf("tick token = %+v, want text ' at column 1", toks[1])
	}
	if toks[2].Character != 2 {
		t.Errorf("paren token at column %d, want 2", toks[2].Character)
	}
}

func TestLexSizeCastWithUnderscoredSize(t *testing.T) {
	toks, errs := Lex("1_6'(x)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIntLiteral, token.KindTick, token.KindLParen, token.KindIdent, token.KindRParen, token.KindEOF)
	if toks[0].Text != "1_6" || toks[1].Character != 3 {
		t.Errorf("got size %q and tick at column %d", toks[0].Text, toks[1].Character)
	}
}

func TestLexSignedSizeBeforeParenIsStillMalformed(t *testing.T) {
	// "8's(" has a signed marker but no base letter: not a cast.
	_, errs := Lex("8's(x)")
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestLexBasedLiteralWithWhiteSpace(t *testing.T) {
	// LRM 5.7.1: white space is allowed between a literal's size and base,
	// and between its base and digits. The spelling stays one token, spaces
	// included, so a consumer that evaluates it must skip them.
	for _, src := range []string{
		"32 'd 1", "32'd 1", "32 'd1", "'b 0", "1 'b x", "8 'h FF", "4 'sb 1010", "8 'H\tfF", "8\n'h FF", "8 \n  'd 1", "'o 7_7",
	} {
		toks, errs := Lex(src)
		if len(errs) != 0 {
			t.Errorf("Lex(%q) errors: %+v", src, errs)
		}
		assertKinds(t, toks, token.KindIntLiteral, token.KindEOF)
		if toks[0].Text != src {
			t.Errorf("Lex(%q) text = %q, want the whole spelling", src, toks[0].Text)
		}
	}
}

func TestLexSpacedBasedLiteralInContext(t *testing.T) {
	toks, errs := Lex("x = 32 'd 1 + 'h F;")
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIdent, token.KindAssign, token.KindIntLiteral, token.KindAdd, token.KindIntLiteral, token.KindSemi, token.KindEOF)
	if toks[2].Text != "32 'd 1" || toks[4].Text != "'h F" {
		t.Errorf("literals = %q and %q", toks[2].Text, toks[4].Text)
	}
}

func TestLexSpacedLiteralFollowingTokenKeepsItsPosition(t *testing.T) {
	// The literal spans a newline (between its size and base); the token
	// after it must still land on the right line and column.
	toks, _ := Lex("8\n'h FF;")
	assertKinds(t, toks, token.KindIntLiteral, token.KindSemi, token.KindEOF)
	if toks[1].Line != 1 || toks[1].Character != 5 {
		t.Errorf("';' at %d:%d, want 1:5", toks[1].Line, toks[1].Character)
	}
	if l, c := toks[0].End(); l != 1 || c != 5 {
		t.Errorf("literal ends at %d:%d, want 1:5", l, c)
	}
}

func TestLexSizeThenSpaceIsNotJoinedUnlessABaseFollows(t *testing.T) {
	cases := []struct {
		src  string
		want []token.Kind
	}{
		// A size cast with a space, an unbased-unsized literal, a tick-brace,
		// and a time unit are all not based literals: the number stays alone.
		{"8 '(x)", []token.Kind{token.KindIntLiteral, token.KindTick, token.KindLParen, token.KindIdent, token.KindRParen, token.KindEOF}},
		{"3 'x", []token.Kind{token.KindIntLiteral, token.KindUnbasedUnsizedLiteral, token.KindEOF}},
		{"3 '{1}", []token.Kind{token.KindIntLiteral, token.KindTickLBrace, token.KindIntLiteral, token.KindRBrace, token.KindEOF}},
		{"10 ns", []token.Kind{token.KindIntLiteral, token.KindIdent, token.KindEOF}},
		{"a ? 4 : 'h0", []token.Kind{token.KindIdent, token.KindQuest, token.KindIntLiteral, token.KindColon, token.KindIntLiteral, token.KindEOF}},
	}
	for _, c := range cases {
		toks, _ := Lex(c.src)
		assertKinds(t, toks, c.want...)
	}
}

func TestLexBareBaseAtEndOfLineDoesNotSwallowTheNextLine(t *testing.T) {
	// Source being typed: "p = 'h" with nothing after the base, followed by
	// a line whose first token is made of valid hex digits ("a", "bad", "f")
	// or starts with some ("endmodule", "add_item"). That token belongs to
	// the next line, not to the literal.
	for _, next := range []string{"a = b;", "bad", "f(x);", "endmodule", "add_item();", "e"} {
		toks, _ := Lex("p = 'h\n" + next)
		if toks[2].Text != "'h" {
			t.Errorf("next line %q: literal = %q, want just the bare base %q", next, toks[2].Text, "'h")
		}
		if want := strings.Fields(next)[0]; !strings.HasPrefix(want, toks[3].Text) {
			t.Errorf("next line %q: following token = %q, want the line's first token intact", next, toks[3].Text)
		}
	}
	// The same-line spelling still joins.
	toks, _ := Lex("p = 'h FF;")
	assertKinds(t, toks, token.KindIdent, token.KindAssign, token.KindIntLiteral, token.KindSemi, token.KindEOF)
}

func TestLexDigitsThatStartALongerIdentifierAreNotJoined(t *testing.T) {
	toks, _ := Lex("p = 'h add_item")
	if toks[2].Text != "'h" {
		t.Errorf("literal = %q, want the bare base", toks[2].Text)
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
