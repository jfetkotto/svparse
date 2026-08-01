package token

// Kind identifies a token's lexical category. It's a bounded enum of
// categories, not one constant per keyword: SV has 240+ reserved words
// (see Keywords), and a Go constant per keyword is a commitment a future
// parser should make only if it actually needs kind-based dispatch on
// specific keywords -- plain string comparison against Token.Text within
// KindKeyword works fine at the scale a hand-written recursive-descent
// parser operates at.
type Kind int

const (
	KindInvalid Kind = iota
	KindEOF

	// Identifiers and literals.
	KindIdent                 // foo, \escaped+ident
	KindKeyword               // module, always_ff, class, ... (see Keywords)
	KindSystemIdent           // $display, $clog2
	KindIntLiteral            // 42, 8'hFF, 4'b10x1, 'h1F
	KindRealLiteral           // 1.5, 1.5e10
	KindTimeLiteral           // 10ns, 1.5us
	KindUnbasedUnsizedLiteral // '0 '1 'x 'X 'z 'Z
	KindStringLiteral         // "..."

	// Compiler directive: `` `name `` (backtick immediately followed by an
	// identifier). Text excludes the backtick. Not interpreted here -- a
	// future preprocessor consumes these from the token stream.
	KindDirective

	// Line continuation: '\' immediately followed by '\n', used inside a
	// `` `define `` body to continue its replacement text onto the next
	// line. Meaningless outside that context; a preprocessor consumes it
	// while scanning a macro body's logical line and it should never
	// reach a parser.
	KindLineContinuation

	// Macro token-paste operator: `` `` `` (two backticks). Only
	// meaningful inside a `` `define `` body -- it disappears during macro
	// expansion, merged into a single token with its two neighbors (see
	// preprocessor.substitute); a preprocessor drops one found anywhere
	// else with a recorded error, and it should never reach a parser.
	KindPaste

	// Macro stringize delimiter: `` `" `` (backtick immediately followed
	// by a double quote). Appears in matched open/close pairs inside a
	// `` `define `` body ("`\"...`\""); a preprocessor collapses each pair
	// and everything between them into a single KindStringLiteral token
	// during macro expansion (see preprocessor.substitute). Meaningless
	// outside that context, same as KindPaste, and should never reach a
	// parser.
	KindMacroQuote

	// Attribute instance delimiters: (* and *). Recognized as fixed
	// 2-character tokens via maximal munch when adjacent -- a pragmatic
	// lexer-level approximation, not a full LRM disambiguation (the
	// grammar only treats them specially at particular syntactic
	// positions; a lexer has no such context).
	KindAttrOpen
	KindAttrClose

	// Punctuation, 1 character.
	KindAdd    // +
	KindSub    // -
	KindMul    // *
	KindQuo    // /
	KindRem    // %
	KindAmp    // &
	KindPipe   // |
	KindCaret  // ^
	KindTilde  // ~
	KindBang   // !
	KindLt     // <
	KindGt     // >
	KindAssign // =
	KindQuest  // ?
	KindColon  // :
	KindSemi   // ;
	KindComma  // ,
	KindDot    // .
	KindHash   // #
	KindAt     // @
	KindLParen // (
	KindRParen // )
	KindLBrace // {
	KindRBrace // }
	KindLBrack // [
	KindRBrack // ]
	KindTick   // '
	KindDollar // $ (standalone, e.g. queue[$] -- see $identifier -> KindSystemIdent)

	// Punctuation/operators, 2 characters.
	KindEq          // ==
	KindNe          // !=
	KindLe          // <=
	KindGe          // >=
	KindAndAnd      // &&
	KindOrOr        // ||
	KindStarStar    // **
	KindShl         // <<
	KindShr         // >>
	KindInc         // ++
	KindDec         // --
	KindAddAssign   // +=
	KindSubAssign   // -=
	KindMulAssign   // *=
	KindQuoAssign   // /=
	KindRemAssign   // %=
	KindAndAssign   // &=
	KindOrAssign    // |=
	KindXorAssign   // ^=
	KindColonColon  // ::
	KindHashHash    // ##
	KindTickLBrace  // '{
	KindDotStar     // .*
	KindArrow       // ->
	KindPlusColon   // +:
	KindMinusColon  // -:
	KindColonAssign // := (dist weight)
	KindColonSlash  // :/ (dist weight)
	KindNand        // ~&
	KindNor         // ~|
	KindXnor        // ~^
	KindXnorAlt     // ^~

	// Punctuation/operators, 3 characters.
	KindCaseEq          // ===
	KindCaseNe          // !==
	KindWildEq          // ==?
	KindWildNe          // !=?
	KindShlAssign       // <<=
	KindShrAssign       // >>=
	KindAShl            // <<< (arithmetic shift left)
	KindAShr            // >>> (arithmetic shift right)
	KindNonblockTrigger // ->>
	KindImplies         // |-> (overlapped implication)
	KindImpliesNext     // |=> (nonoverlapped implication)

	// Punctuation/operators, 4 characters.
	KindAShlAssign // <<<=
	KindAShrAssign // >>>=
)

var kindNames = map[Kind]string{
	KindInvalid: "Invalid", KindEOF: "EOF",

	KindIdent: "Ident", KindKeyword: "Keyword", KindSystemIdent: "SystemIdent",
	KindIntLiteral: "IntLiteral", KindRealLiteral: "RealLiteral", KindTimeLiteral: "TimeLiteral",
	KindUnbasedUnsizedLiteral: "UnbasedUnsizedLiteral", KindStringLiteral: "StringLiteral",

	KindDirective: "Directive", KindLineContinuation: "LineContinuation",
	KindPaste: "``", KindMacroQuote: "`\"",
	KindAttrOpen: "AttrOpen", KindAttrClose: "AttrClose",

	KindAdd: "+", KindSub: "-", KindMul: "*", KindQuo: "/", KindRem: "%",
	KindAmp: "&", KindPipe: "|", KindCaret: "^", KindTilde: "~", KindBang: "!",
	KindLt: "<", KindGt: ">", KindAssign: "=", KindQuest: "?", KindColon: ":",
	KindSemi: ";", KindComma: ",", KindDot: ".", KindHash: "#", KindAt: "@",
	KindLParen: "(", KindRParen: ")", KindLBrace: "{", KindRBrace: "}",
	KindLBrack: "[", KindRBrack: "]", KindTick: "'", KindDollar: "$",

	KindEq: "==", KindNe: "!=", KindLe: "<=", KindGe: ">=",
	KindAndAnd: "&&", KindOrOr: "||", KindStarStar: "**",
	KindShl: "<<", KindShr: ">>", KindInc: "++", KindDec: "--",
	KindAddAssign: "+=", KindSubAssign: "-=", KindMulAssign: "*=", KindQuoAssign: "/=", KindRemAssign: "%=",
	KindAndAssign: "&=", KindOrAssign: "|=", KindXorAssign: "^=",
	KindColonColon: "::", KindHashHash: "##", KindTickLBrace: "'{", KindDotStar: ".*",
	KindArrow: "->", KindPlusColon: "+:", KindMinusColon: "-:",
	KindColonAssign: ":=", KindColonSlash: ":/",
	KindNand: "~&", KindNor: "~|", KindXnor: "~^", KindXnorAlt: "^~",

	KindCaseEq: "===", KindCaseNe: "!==", KindWildEq: "==?", KindWildNe: "!=?",
	KindShlAssign: "<<=", KindShrAssign: ">>=", KindAShl: "<<<", KindAShr: ">>>",
	KindNonblockTrigger: "->>", KindImplies: "|->", KindImpliesNext: "|=>",

	KindAShlAssign: "<<<=", KindAShrAssign: ">>>=",
}

// String returns a human-readable name for k, primarily for debugging and
// test failure messages -- for operator/punctuation kinds it's the exact
// lexeme; for everything else it's the category name.
func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "Unknown"
}
