package lexer

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

// Every entry in multiCharOperators, lexed on its own, must round-trip to
// exactly that Kind and Text -- the direct correctness check for the
// table itself, independent of maximal-munch interaction with neighbors.
func TestLexEveryMultiCharOperatorInIsolation(t *testing.T) {
	for _, op := range multiCharOperators {
		assertOneToken(t, op.text, op.kind, op.text)
	}
}

func TestLexEverySingleCharOperatorInIsolation(t *testing.T) {
	for r, kind := range singleCharOperators {
		assertOneToken(t, string(r), kind, string(r))
	}
}

func TestLexShiftOperatorsMaximalMunch(t *testing.T) {
	assertOneToken(t, "<<<=", token.KindAShlAssign, "<<<=")
	assertOneToken(t, ">>>=", token.KindAShrAssign, ">>>=")
	assertOneToken(t, "<<<", token.KindAShl, "<<<")
	assertOneToken(t, "<<=", token.KindShlAssign, "<<=")
	assertOneToken(t, "<<", token.KindShl, "<<")
	assertOneToken(t, "<", token.KindLt, "<")
}

func TestLexEqualityOperatorsMaximalMunch(t *testing.T) {
	assertOneToken(t, "===", token.KindCaseEq, "===")
	assertOneToken(t, "==?", token.KindWildEq, "==?")
	assertOneToken(t, "==", token.KindEq, "==")
	assertOneToken(t, "=", token.KindAssign, "=")
}

func TestLexAttributeDelimitersNotConfusedWithMultiplyOrParen(t *testing.T) {
	assertOneToken(t, "(*", token.KindAttrOpen, "(*")
	assertOneToken(t, "*)", token.KindAttrClose, "*)")

	// A space between them means they're NOT the attribute delimiter --
	// ordinary '(' followed by a separate '*' (multiply/wildcard).
	toks, _ := Lex("( *")
	assertKinds(t, toks, token.KindLParen, token.KindMul, token.KindEOF)
}

func TestLexDivisionNotConfusedWithComment(t *testing.T) {
	toks, _ := Lex("a / b")
	assertKinds(t, toks, token.KindIdent, token.KindQuo, token.KindIdent, token.KindEOF)
}

func TestLexDivideAssignVsDivideThenComment(t *testing.T) {
	assertOneToken(t, "/=", token.KindQuoAssign, "/=")

	// "a //comment" -- the '/' must be consumed by the comment rule, not
	// misread as an operator start racing against it.
	toks, _ := Lex("a //comment")
	assertKinds(t, toks, token.KindIdent, token.KindEOF)
}

func TestLexXnorBothSpellings(t *testing.T) {
	assertOneToken(t, "~^", token.KindXnor, "~^")
	assertOneToken(t, "^~", token.KindXnorAlt, "^~")
}

func TestLexRealisticExpressionSequence(t *testing.T) {
	// "a <= b + 1 ? c : d" -- exercises several operators back to back
	// with no whitespace-driven separation ambiguity ("<=" not "<" "=").
	toks, _ := Lex("a <= b + 1 ? c : d")
	assertKinds(t, toks,
		token.KindIdent, token.KindLe, token.KindIdent, token.KindAdd, token.KindIntLiteral,
		token.KindQuest, token.KindIdent, token.KindColon, token.KindIdent, token.KindEOF,
	)
}

func TestLexNonblockingAssignVsLessEqual(t *testing.T) {
	// "<=" lexes to KindLe regardless of context (nonblocking-assignment
	// vs less-than-or-equal is a parser-level distinction, not a lexical
	// one -- both spellings are identical at the lexer).
	assertOneToken(t, "<=", token.KindLe, "<=")
}
