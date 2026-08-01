package lexer

import (
	"sort"

	"github.com/jfetkotto/svparse/token"
)

// multiCharOperators lists every operator/punctuation lexeme longer than
// one character. Sorted by descending length at init time (rather than
// relying on manual ordering below staying correct forever) so maximal
// munch is always right regardless of the order entries are added in:
// "<<<=" must be tried before "<<<" before "<<=" before "<<" before "<".
//
// '{ is deliberately absent -- it's fully handled by scanNumber's
// tick-prefixed-literal path (see number.go), which owns every "'"-led
// token, so it never reaches this table.
var multiCharOperators = []struct {
	text string
	kind token.Kind
}{
	{"<<<=", token.KindAShlAssign}, {">>>=", token.KindAShrAssign},

	{"===", token.KindCaseEq}, {"!==", token.KindCaseNe},
	{"==?", token.KindWildEq}, {"!=?", token.KindWildNe},
	{"<<=", token.KindShlAssign}, {">>=", token.KindShrAssign},
	{"<<<", token.KindAShl}, {">>>", token.KindAShr},
	{"->>", token.KindNonblockTrigger},
	{"|->", token.KindImplies}, {"|=>", token.KindImpliesNext},

	{"==", token.KindEq}, {"!=", token.KindNe}, {"<=", token.KindLe}, {">=", token.KindGe},
	{"&&", token.KindAndAnd}, {"||", token.KindOrOr}, {"**", token.KindStarStar},
	{"<<", token.KindShl}, {">>", token.KindShr},
	{"++", token.KindInc}, {"--", token.KindDec},
	{"+=", token.KindAddAssign}, {"-=", token.KindSubAssign}, {"*=", token.KindMulAssign},
	{"/=", token.KindQuoAssign}, {"%=", token.KindRemAssign},
	{"&=", token.KindAndAssign}, {"|=", token.KindOrAssign}, {"^=", token.KindXorAssign},
	{"::", token.KindColonColon}, {"##", token.KindHashHash},
	{".*", token.KindDotStar}, {"->", token.KindArrow},
	{"+:", token.KindPlusColon}, {"-:", token.KindMinusColon},
	{":=", token.KindColonAssign}, {":/", token.KindColonSlash},
	{"(*", token.KindAttrOpen}, {"*)", token.KindAttrClose},
	{"~&", token.KindNand}, {"~|", token.KindNor}, {"~^", token.KindXnor}, {"^~", token.KindXnorAlt},
}

func init() {
	sort.Slice(multiCharOperators, func(i, j int) bool {
		return len(multiCharOperators[i].text) > len(multiCharOperators[j].text)
	})
}

var singleCharOperators = map[rune]token.Kind{
	'+': token.KindAdd, '-': token.KindSub, '*': token.KindMul, '/': token.KindQuo, '%': token.KindRem,
	'&': token.KindAmp, '|': token.KindPipe, '^': token.KindCaret, '~': token.KindTilde, '!': token.KindBang,
	'<': token.KindLt, '>': token.KindGt, '=': token.KindAssign, '?': token.KindQuest, ':': token.KindColon,
	';': token.KindSemi, ',': token.KindComma, '.': token.KindDot, '#': token.KindHash, '@': token.KindAt,
	'(': token.KindLParen, ')': token.KindRParen, '{': token.KindLBrace, '}': token.KindRBrace,
	'[': token.KindLBrack, ']': token.KindRBrack,
	// ' and $ are NOT here: scanNumber and scanDollarOrSystemIdent own
	// them respectively, both dispatched earlier since they need
	// lookahead beyond what a fixed-width table entry can express.
}

func isOperatorStart(r rune) bool {
	_, ok := singleCharOperators[r]
	return ok
}

// scanOperator matches the longest operator/punctuation lexeme starting
// at the cursor, trying every multi-character candidate before falling
// back to the single-character table.
func (l *lexer) scanOperator() {
	startLine, startChar := l.line, l.char

	for _, op := range multiCharOperators {
		if l.matchesAt(op.text) {
			l.advanceN(len(op.text))
			l.emit(op.kind, op.text, startLine, startChar)
			return
		}
	}

	r := l.runes[l.i]
	if kind, ok := singleCharOperators[r]; ok {
		l.advance()
		l.emit(kind, string(r), startLine, startChar)
		return
	}

	// Unreachable in practice: scanOperator is only invoked (see
	// lexer.go's dispatch) for runes that are keys of singleCharOperators,
	// so the fallback above always matches. Kept as a safety net rather
	// than assumed, consistent with never panicking on unexpected input.
	l.emitInvalid()
}

func (l *lexer) matchesAt(s string) bool {
	for k, want := range s {
		if l.peek(k) != want {
			return false
		}
	}
	return true
}

func (l *lexer) advanceN(n int) {
	for range n {
		l.advance()
	}
}
