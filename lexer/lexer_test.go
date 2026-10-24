package lexer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func kinds(toks []token.Token) []token.Kind {
	out := make([]token.Kind, len(toks))
	for i, t := range toks {
		out[i] = t.Kind
	}
	return out
}

func assertKinds(t *testing.T, toks []token.Token, want ...token.Kind) {
	t.Helper()
	got := kinds(toks)
	if len(got) != len(want) {
		t.Fatalf("got %d tokens %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestLexAlwaysEndsWithEOF(t *testing.T) {
	toks, _ := Lex("")
	assertKinds(t, toks, token.KindEOF)
}

func TestLexIdentifierAndKeyword(t *testing.T) {
	toks, errs := Lex("module foo")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindKeyword, token.KindIdent, token.KindEOF)
	if toks[0].Text != "module" || toks[1].Text != "foo" {
		t.Fatalf("unexpected token text: %+v", toks[:2])
	}
}

func TestLexIdentifierPositions(t *testing.T) {
	toks, _ := Lex("  foo")
	if len(toks) < 1 || toks[0].Line != 0 || toks[0].Character != 2 {
		t.Fatalf("expected foo at (0, 2), got %+v", toks[0])
	}
}

func TestLexTracksLineNumbers(t *testing.T) {
	toks, _ := Lex("foo\nbar")
	assertKinds(t, toks, token.KindIdent, token.KindIdent, token.KindEOF)
	if toks[0].Line != 0 || toks[1].Line != 1 || toks[1].Character != 0 {
		t.Fatalf("unexpected positions: %+v", toks[:2])
	}
}

func TestLexCRLFDoesNotCountTowardColumn(t *testing.T) {
	// "foo\r\nbar" -- the \r must not shift bar's column or count as a
	// second line advance.
	toks, _ := Lex("foo\r\nbar")
	assertKinds(t, toks, token.KindIdent, token.KindIdent, token.KindEOF)
	if toks[1].Line != 1 || toks[1].Character != 0 {
		t.Fatalf("expected bar at (1, 0), got (%d, %d)", toks[1].Line, toks[1].Character)
	}
}

func TestLexEscapedIdentifier(t *testing.T) {
	toks, _ := Lex(`\bus+index rest`)
	assertKinds(t, toks, token.KindIdent, token.KindIdent, token.KindEOF)
	if toks[0].Text != `\bus+index` {
		t.Fatalf("got %q, want %q", toks[0].Text, `\bus+index`)
	}
	if toks[1].Text != "rest" {
		t.Fatalf("expected the terminating space to not be consumed into the escaped ident, got %+v", toks[1])
	}
}

func TestLexLineContinuationLF(t *testing.T) {
	toks, errs := Lex("foo\\\nbar")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIdent, token.KindLineContinuation, token.KindIdent, token.KindEOF)
	if toks[1].Text != "\\\n" {
		t.Fatalf("unexpected line-continuation text: %q", toks[1].Text)
	}
	if toks[2].Line != 1 || toks[2].Character != 0 {
		t.Fatalf("expected bar at (1, 0) after the continuation, got (%d, %d)", toks[2].Line, toks[2].Character)
	}
}

func TestLexLineContinuationCRLF(t *testing.T) {
	toks, errs := Lex("foo\\\r\nbar")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIdent, token.KindLineContinuation, token.KindIdent, token.KindEOF)
	if toks[2].Line != 1 || toks[2].Character != 0 {
		t.Fatalf("expected bar at (1, 0) after the continuation, got (%d, %d)", toks[2].Line, toks[2].Character)
	}
}

func TestLexBackslashFollowedByWhitespaceIsInvalid(t *testing.T) {
	toks, errs := Lex(`\ foo`)
	assertKinds(t, toks, token.KindInvalid, token.KindIdent, token.KindEOF)
	if toks[0].Text != `\` {
		t.Fatalf("unexpected invalid-token text: %q", toks[0].Text)
	}
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestLexBackslashAtEOFIsInvalid(t *testing.T) {
	toks, errs := Lex(`\`)
	assertKinds(t, toks, token.KindInvalid, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestLexStringLiteralOwnLineContinuationStillWorks(t *testing.T) {
	// scanStringEscape's own '\'-newline handling (inside a string) is a
	// separate code path from scanEscapedIdent's -- confirm it's untouched.
	toks, errs := Lex("\"a\\\nb\"")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindStringLiteral, token.KindEOF)
	if toks[0].Text != "\"a\\\nb\"" {
		t.Fatalf("unexpected text: %q", toks[0].Text)
	}
}

func TestLexSystemIdentifier(t *testing.T) {
	toks, _ := Lex("$display $clog2")
	assertKinds(t, toks, token.KindSystemIdent, token.KindSystemIdent, token.KindEOF)
	if toks[0].Text != "$display" || toks[1].Text != "$clog2" {
		t.Fatalf("unexpected text: %+v", toks[:2])
	}
}

func TestLexBareDollarIsItsOwnToken(t *testing.T) {
	toks, _ := Lex("q[$]")
	// What this test pins down is that '$' alone (not followed by an
	// identifier start) is KindDollar, not folded into a KindSystemIdent.
	assertKinds(t, toks, token.KindIdent, token.KindLBrack, token.KindDollar, token.KindRBrack, token.KindEOF)
	if toks[2].Kind != token.KindDollar || toks[2].Text != "$" {
		t.Fatalf("expected a bare KindDollar token, got %+v", toks[2])
	}
}

func TestLexLineComment(t *testing.T) {
	toks, errs := Lex("foo // bar baz\nqux")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIdent, token.KindIdent, token.KindEOF)
	if toks[1].Text != "qux" || toks[1].Line != 1 {
		t.Fatalf("expected qux on line 1, got %+v", toks[1])
	}
}

func TestLexBlockComment(t *testing.T) {
	toks, errs := Lex("foo /* comment\nspanning lines */ bar")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertKinds(t, toks, token.KindIdent, token.KindIdent, token.KindEOF)
	if toks[1].Text != "bar" || toks[1].Line != 1 {
		t.Fatalf("expected bar on line 1 after the block comment, got %+v", toks[1])
	}
}

func TestLexUnterminatedBlockCommentRecordsErrorButDoesNotHang(t *testing.T) {
	toks, errs := Lex("foo /* never closed")
	assertKinds(t, toks, token.KindIdent, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one error, got %+v", errs)
	}
}

func TestLexInvalidCharacterTaggedNotFatal(t *testing.T) {
	// '`' (a compiler directive marker) isn't handled until a later
	// commit -- it becomes its own KindInvalid token, and lexing
	// continues afterward.
	toks, errs := Lex("foo ` bar")
	assertKinds(t, toks, token.KindIdent, token.KindInvalid, token.KindIdent, token.KindEOF)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one error for the unrecognized '`', got %+v", errs)
	}
}

func TestLexNeverPanicsOnArbitraryInput(t *testing.T) {
	inputs := []string{
		"", " ", "\x00", "\xff", "🎉", "'", "\\", "/*", "//", "$", "\r\r\r\n\n",
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Lex(%q) panicked: %v", in, r)
				}
			}()
			Lex(in)
		}()
	}
}

// benchSource builds a file with the punctuation mix real RTL has: the
// single-character tokens ';' ',' '(' ')' '.' dominate, and none of them
// starts a multi-character operator.
func benchSource(modules int) string {
	var b strings.Builder
	for i := range modules {
		fmt.Fprintf(&b, "module m%d #(parameter int W = 8) (\n", i)
		b.WriteString("  input  wire logic [W-1:0] a,\n  input  wire logic [W-1:0] b,\n  output var  logic [W-1:0] y\n);\n")
		for j := range 20 {
			fmt.Fprintf(&b, "  logic [W-1:0] t%d;\n  assign t%d = (a[%d] & b[%d]) | (a >> 1) ^ {b[0], a[1:0]};\n", j, j, j, j)
		}
		fmt.Fprintf(&b, "  leaf #(.W(W)) u%d (.a(a), .b(b), .y(y));\n", i)
		b.WriteString("endmodule\n\n")
	}
	return b.String()
}

func BenchmarkLexRTL(b *testing.B) {
	src := benchSource(40)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for range b.N {
		if _, errs := Lex(src); len(errs) != 0 {
			b.Fatalf("unexpected lex errors: %+v", errs)
		}
	}
}
