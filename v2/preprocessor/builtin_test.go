package preprocessor

import (
	"testing"

	"github.com/jfetkotto/svparse/v2/token"
)

func TestFileAndLineDirectives(t *testing.T) {
	toks, errs := Preprocess("a.sv", "x\n`__FILE__ `__LINE__\n", nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "x", `"a.sv"`, "2")
	if toks[1].Kind != token.KindStringLiteral || toks[2].Kind != token.KindIntLiteral {
		t.Errorf("kinds: %v %v", toks[1].Kind, toks[2].Kind)
	}
}

func TestCoverageConstantsArePredefined(t *testing.T) {
	toks, errs := Preprocess("a.sv", "`SV_COV_TOGGLE", nil)
	if len(errs) != 0 || len(toks) != 1 || toks[0].Kind != token.KindIntLiteral {
		t.Fatalf("got %v, %+v", texts(toks), errs)
	}
	// A definition of the same name wins.
	toks, _ = Preprocess("a.sv", "`define SV_COV_TOGGLE 99\n`SV_COV_TOGGLE", nil)
	assertTexts(t, toks, "99")
}

func TestStringizeEscapedQuote(t *testing.T) {
	src := "`define msg(x,y) `\"x: `\\`\"y`\\`\"`\"\n`msg(left,right)"
	toks, errs := Preprocess("a.sv", src, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(toks) != 1 || toks[0].Kind != token.KindStringLiteral {
		t.Fatalf("got %v", texts(toks))
	}
}

func TestStringizeKeepsTouchingTokensTogether(t *testing.T) {
	toks, _ := Preprocess("a.sv", "`define s(x) `\"x`\"\n`s(a.b c)", nil)
	assertTexts(t, toks, `"a.b c"`)
}

func TestBeginKeywordsDemotesNewerKeywords(t *testing.T) {
	src := "`begin_keywords \"1364-2001\"\nreg logic;\n`end_keywords\nlogic y;"
	toks, errs := Preprocess("a.sv", src, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "reg", "logic", ";", "logic", "y", ";")
	want := []token.Kind{token.KindKeyword, token.KindIdent, token.KindSemi, token.KindKeyword, token.KindIdent, token.KindSemi}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d %q: kind %v, want %v", i, toks[i].Text, toks[i].Kind, k)
		}
	}
}

func TestBeginKeywordsSystemVerilogVersionKeepsKeywords(t *testing.T) {
	toks, _ := Preprocess("a.sv", "`begin_keywords \"1800-2017\"\nlogic\n`end_keywords", nil)
	if len(toks) != 1 || toks[0].Kind != token.KindKeyword {
		t.Fatalf("got %v", toks)
	}
}
