package preprocessor

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func TestTokenPasteJoinsIdentifiers(t *testing.T) {
	toks, errs := pp(t, "`define JOIN(a,b) a``b\n`JOIN(foo,bar)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "foobar")
	if toks[0].Kind != token.KindIdent {
		t.Fatalf("expected pasted token to be an identifier, got %v", toks[0].Kind)
	}
}

func TestTokenPasteChain(t *testing.T) {
	toks, errs := pp(t, "`define JOIN3(a,b,c) a``b``c\n`JOIN3(x,y,z)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "xyz")
}

func TestTokenPasteAtBodyStartIsDropped(t *testing.T) {
	toks, errs := pp(t, "`define PFX(a) ``a\n`PFX(foo)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "foo")
}

func TestTokenPasteAtBodyEndIsDropped(t *testing.T) {
	toks, errs := pp(t, "`define SFX(a) a``\n`SFX(foo)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "foo")
}

func TestStringizeWrapsExpandedArgument(t *testing.T) {
	toks, errs := pp(t, "`define MSG(x) `\"x`\"\n`MSG(hello)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(toks) != 1 {
		t.Fatalf("expected 1 token, got %+v", toks)
	}
	if toks[0].Kind != token.KindStringLiteral {
		t.Fatalf("expected a string literal, got %v", toks[0].Kind)
	}
	if toks[0].Text != `"hello"` {
		t.Fatalf("expected stringized text %q, got %q", `"hello"`, toks[0].Text)
	}
}

func TestStringizeWrapsMultipleTokens(t *testing.T) {
	// Tokens between the `" ... `" delimiters are space-joined, not
	// reproduced with exact source whitespace -- "value:" lexes as two
	// separate tokens ("value", ":"), so the joined text gets a space
	// between them; exact fidelity is deliberately out of scope (see
	// mergeStringize's doc comment).
	toks, errs := pp(t, "`define MSG(x) `\"value is x`\"\n`MSG(WIDTH)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(toks) != 1 || toks[0].Kind != token.KindStringLiteral {
		t.Fatalf("expected 1 string literal, got %+v", toks)
	}
	if toks[0].Text != `"value is WIDTH"` {
		t.Fatalf("unexpected stringized text: %q", toks[0].Text)
	}
}

func TestPasteAndStringizeOutsideMacroBodyIsAnError(t *testing.T) {
	_, errs := pp(t, "logic a``b;")
	if len(errs) == 0 {
		t.Fatalf("expected an error for a stray `` outside any macro body")
	}
}
