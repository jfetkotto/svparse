package preprocessor

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func lexToPPTokens(t *testing.T, src string) []Token {
	t.Helper()
	p := &preprocessor{}
	return p.lexToTokens("test.sv", src)
}

func TestSplitBalancedArgsExported(t *testing.T) {
	toks := lexToPPTokens(t, "1, foo(2,3), 4)")
	groups, newPos, closed := SplitBalancedArgs(toks, 0)
	if !closed {
		t.Fatalf("expected closed=true")
	}
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d: %+v", len(groups), groups)
	}
	if len(groups[0]) != 1 || groups[0][0].Text != "1" {
		t.Fatalf("unexpected group 0: %+v", groups[0])
	}
	if len(groups[1]) != 6 { // foo ( 2 , 3 )
		t.Fatalf("expected the nested call's comma to stay in one group, got %+v", groups[1])
	}
	if len(groups[2]) != 1 || groups[2][0].Text != "4" {
		t.Fatalf("unexpected group 2: %+v", groups[2])
	}
	// newPos should point just past the ')' that closed this call, i.e.
	// at the KindEOF sentinel lexer.Lex always appends.
	if toks[newPos].Kind != token.KindEOF {
		t.Fatalf("expected newPos to land on EOF, got %+v", toks[newPos])
	}
}

func TestSplitBalancedArgsUnclosedExported(t *testing.T) {
	toks := lexToPPTokens(t, "1, 2")
	_, _, closed := SplitBalancedArgs(toks, 0)
	if closed {
		t.Fatalf("expected closed=false for unterminated input")
	}
}
