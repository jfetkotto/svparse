package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// maxExprDepth bounds how deeply expressions may nest (parentheses, unary
// chains, conditional chains, ...), counted once per level of recursion
// through parseExprBP, a prefix operator, or a tagged operand. Real code
// stays far below it; it exists
// so adversarial or fuzzed input cannot exhaust the goroutine stack, which
// would be a fatal error rather than a recoverable panic. Past the limit the
// offending group is reported once and skipped as a single BadExpr.
const maxExprDepth = 512

// ParseExpr parses toks as a single expression (LRM 11, Annex A.8).
//
// It never fails and never returns nil: source it cannot make sense of
// becomes a *ast.BadExpr in the tree and an Error in the result, and parsing
// carries on from the next delimiter. Tokens left over after a complete
// expression are reported as an error but not consumed into the tree.
//
// What it deliberately does not parse yet: a "randomize() with { ... }"
// constraint block and "dist" (both arrive with the constraint grammar),
// "matches" patterns and "&&&" (statement conditions), and property/sequence
// operators (assertions).
func ParseExpr(toks []preprocessor.Token) (ast.Expr, []Error) {
	p := &parser{toks: toks}
	e := p.parseExpr()
	if tok := p.peek(); tok.Kind != token.KindEOF {
		p.errorf(tok, "unexpected %s after expression", describeTok(tok))
	}
	return e, p.errs
}

// Binding powers, lowest to highest, following LRM table 11-2. Conditional
// ("?:") and implication ("->", "<->") are right-associative; every other
// binary operator is left-associative.
const (
	bpLowest  = iota
	bpImplies // -> <->
	bpCond    // ?:
	bpLogOr   // ||
	bpLogAnd  // &&
	bpBitOr   // |
	bpBitXor  // ^ ~^ ^~
	bpBitAnd  // &
	bpEq      // == != === !== ==? !=?
	bpRel     // < <= > >= inside
	bpShift   // << >> <<< >>>
	bpAdd     // + -
	bpMul     // * / %
	bpPow     // **
)

// infixBP reports a token's binary-operator binding power and whether it
// associates to the right. "inside" and "?" are handled by the caller.
func infixBP(tok preprocessor.Token) (bp int, rightAssoc, ok bool) {
	switch tok.Kind {
	case token.KindArrow, token.KindEquiv:
		return bpImplies, true, true
	case token.KindOrOr:
		return bpLogOr, false, true
	case token.KindAndAnd:
		return bpLogAnd, false, true
	case token.KindPipe:
		return bpBitOr, false, true
	case token.KindCaret, token.KindXnor, token.KindXnorAlt:
		return bpBitXor, false, true
	case token.KindAmp:
		return bpBitAnd, false, true
	case token.KindEq, token.KindNe, token.KindCaseEq, token.KindCaseNe, token.KindWildEq, token.KindWildNe:
		return bpEq, false, true
	case token.KindLt, token.KindLe, token.KindGt, token.KindGe:
		return bpRel, false, true
	case token.KindShl, token.KindShr, token.KindAShl, token.KindAShr:
		return bpShift, false, true
	case token.KindAdd, token.KindSub:
		return bpAdd, false, true
	case token.KindMul, token.KindQuo, token.KindRem:
		return bpMul, false, true
	case token.KindStarStar:
		return bpPow, false, true
	}
	return 0, false, false
}

func isAssignOp(k token.Kind) bool {
	switch k {
	case token.KindAssign, token.KindAddAssign, token.KindSubAssign, token.KindMulAssign,
		token.KindQuoAssign, token.KindRemAssign, token.KindAndAssign, token.KindOrAssign,
		token.KindXorAssign, token.KindShlAssign, token.KindShrAssign,
		token.KindAShlAssign, token.KindAShrAssign:
		return true
	}
	return false
}

func isUnaryOp(k token.Kind) bool {
	switch k {
	case token.KindAdd, token.KindSub, token.KindBang, token.KindTilde,
		token.KindAmp, token.KindPipe, token.KindCaret,
		token.KindNand, token.KindNor, token.KindXnor, token.KindXnorAlt:
		return true
	}
	return false
}

// typeArgFuncs are the system functions whose first argument may be a data
// type as well as an expression (LRM 20.6, 20.7): their name-shaped operand
// is recorded as an ast.TypeOrExpr for name resolution to settle.
var typeArgFuncs = map[string]bool{
	"$bits": true, "$typename": true, "$size": true, "$left": true, "$right": true,
	"$low": true, "$high": true, "$increment": true, "$dimensions": true,
	"$unpacked_dimensions": true,
}

// isExprTypeKeyword reports whether text begins a data type in expression
// position (a cast target, a $bits operand, a "#(int)" override).
func isExprTypeKeyword(text string) bool {
	return dataTypeKeywords[text] || text == "void" || text == "const" || text == "virtual"
}

// finish stamps e with the span from token index start to the last token
// consumed, and returns it.
func finish[E interface {
	ast.Expr
	SetPos(ast.Position)
}](p *parser, start int, e E) E {
	e.SetPos(p.spanFrom(start))
	return e
}

// spanFrom is the source span from token index i through the last token
// consumed.
//
// If nothing was consumed the span is zero-width at the start of token i.
// That is where a placeholder for a missing operand belongs: the start of
// the next token is never before the construct that is looking for the
// operand (a missing left operand is created before that construct has
// consumed anything). The placeholder can, though, lie past the end of the
// last token the enclosing construct did consume, when whitespace or a
// token belonging to an outer construct follows ("0?0 0": the missing
// else-branch sits at the stray second 0). So an enclosing span is widened
// to contain the most recent zero-width node created since it began.
//
// End never precedes Start (tokens from a macro expansion can be positioned
// out of order relative to their invocation).
func (p *parser) spanFrom(i int) ast.Position {
	first := p.peekAt(i - p.pos)
	pos := ast.Position{File: first.File, Line: first.Line, Character: first.Character}
	pos.EndLine, pos.EndCharacter = pos.Line, pos.Character

	if p.pos <= i { // nothing consumed
		p.zeroWidthSet, p.zeroWidthAt = true, i
		p.zeroWidthLine, p.zeroWidthCol = pos.Line, pos.Character
		return pos
	}

	after := func(l1, c1, l2, c2 int) bool { return l1 > l2 || (l1 == l2 && c1 > c2) }
	if p.pos-1 < len(p.toks) {
		endLine, endChar := p.toks[p.pos-1].End()
		if !after(pos.Line, pos.Character, endLine, endChar) {
			pos.EndLine, pos.EndCharacter = endLine, endChar
		}
	}
	if p.zeroWidthSet && p.zeroWidthAt >= i && after(p.zeroWidthLine, p.zeroWidthCol, pos.EndLine, pos.EndCharacter) {
		pos.EndLine, pos.EndCharacter = p.zeroWidthLine, p.zeroWidthCol
	}
	return pos
}

func describeTok(tok preprocessor.Token) string {
	if tok.Kind == token.KindEOF {
		return "end of input"
	}
	return "\"" + tok.Text + "\""
}

// expectKind consumes a token of the given kind or records an error and
// leaves the cursor alone.
func (p *parser) expectKind(kind token.Kind, what string) bool {
	tok := p.peek()
	if tok.Kind == kind {
		p.advance()
		return true
	}
	p.errorf(tok, "expected %s, found %s", what, describeTok(tok))
	return false
}

// exprStopper reports whether tok ends an expression without belonging to
// one: a closing delimiter, a separator, or the end of input. A BadExpr
// never consumes one, so the enclosing construct can still see its own
// terminator.
func exprStopper(tok preprocessor.Token) bool {
	switch tok.Kind {
	case token.KindEOF, token.KindRParen, token.KindRBrack, token.KindRBrace,
		token.KindComma, token.KindSemi, token.KindColon:
		return true
	}
	return false
}

// startsPrimary reports whether tok can begin a primary (LRM 11.1).
func startsPrimary(tok preprocessor.Token) bool {
	switch tok.Kind {
	case token.KindIdent, token.KindSystemIdent, token.KindIntLiteral, token.KindRealLiteral,
		token.KindTimeLiteral, token.KindUnbasedUnsizedLiteral, token.KindStringLiteral,
		token.KindDollar, token.KindLParen, token.KindLBrace, token.KindTickLBrace:
		return true
	case token.KindKeyword:
		switch tok.Text {
		case "this", "super", "null", "new", "tagged", "type", "local":
			return true
		}
		return isExprTypeKeyword(tok.Text)
	}
	return false
}

func startsExpr(tok preprocessor.Token) bool {
	return startsPrimary(tok) || isUnaryOp(tok.Kind) || tok.Kind == token.KindInc || tok.Kind == token.KindDec
}

// parseExpr parses a full expression, including the lowest-precedence
// implication operators.
func (p *parser) parseExpr() ast.Expr {
	return p.parseExprBP(bpLowest)
}

// parseExprBP is the Pratt loop: parse an operand, then fold in operators
// whose binding power is at least minBP.
func (p *parser) parseExprBP(minBP int) ast.Expr {
	start := p.pos
	if !p.enterExpr() {
		return p.tooDeep()
	}
	defer p.leaveExpr()

	lhs := p.parseUnary()
	for {
		tok := p.peek()

		if tok.Kind == token.KindQuest && bpCond >= minBP {
			p.advance()
			then := p.parseExprBP(bpLowest)
			var els ast.Expr
			if p.expectKind(token.KindColon, "':' in conditional expression") {
				els = p.parseExprBP(bpCond) // right-associative
			} else {
				els = p.emptyBad()
			}
			lhs = finish(p, start, &ast.Conditional{Cond: lhs, Then: then, Else: els})
			continue
		}

		if tok.Kind == token.KindKeyword && tok.Text == "inside" && bpRel >= minBP {
			p.advance()
			lhs = finish(p, start, &ast.Inside{X: lhs, Ranges: p.parseInsideSet()})
			continue
		}

		bp, rightAssoc, ok := infixBP(tok)
		if !ok || bp < minBP {
			return lhs
		}
		p.advance()
		next := bp + 1
		if rightAssoc {
			next = bp
		}
		rhs := p.parseExprBP(next)
		lhs = finish(p, start, &ast.Binary{Op: tok.Text, X: lhs, Y: rhs})
	}
}

// parseInsideSet parses the "{ open_range_list }" of an inside expression
// (LRM 11.4.13): value expressions and "[lo:hi]" ranges.
func (p *parser) parseInsideSet() []ast.Expr {
	if !p.expectKind(token.KindLBrace, "'{' after inside") {
		return nil
	}
	var items []ast.Expr
	if p.peek().Kind != token.KindRBrace {
		for {
			items = append(items, p.parseRangeOrValue())
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	}
	p.expectKind(token.KindRBrace, "'}' to close the inside set")
	return items
}

// parseRangeOrValue parses "[lo:hi]" as an ast.ValueRange, else a plain
// value expression.
func (p *parser) parseRangeOrValue() ast.Expr {
	if p.peek().Kind != token.KindLBrack {
		return p.parseExpr()
	}
	start := p.pos
	p.advance() // '['
	lo := p.parseExpr()
	var hi ast.Expr
	if p.expectKind(token.KindColon, "':' in a value range") {
		hi = p.parseExpr()
	} else {
		hi = p.emptyBad()
	}
	p.expectKind(token.KindRBrack, "']' to close the value range")
	return finish(p, start, &ast.ValueRange{Lo: lo, Hi: hi})
}

// parseUnary parses prefix operators (which bind tighter than every binary
// operator, "**" included), then a primary with its postfix operators.
func (p *parser) parseUnary() ast.Expr {
	p.skipAttributeInstances()
	start := p.pos
	tok := p.peek()
	prefix := isUnaryOp(tok.Kind) || tok.Kind == token.KindInc || tok.Kind == token.KindDec
	if !prefix {
		return p.parsePostfix(p.parsePrimary(), start)
	}

	// A chain of prefix operators recurses once per operator.
	if !p.enterExpr() {
		return p.tooDeep()
	}
	defer p.leaveExpr()
	p.advance()
	x := p.parseUnary()
	if isUnaryOp(tok.Kind) {
		return finish(p, start, &ast.Unary{Op: tok.Text, X: x})
	}
	return finish(p, start, &ast.IncDec{Op: tok.Text, Prefix: true, X: x})
}

// parsePrimary parses one primary (LRM 11.1) without postfix operators.
func (p *parser) parsePrimary() ast.Expr {
	start := p.pos
	tok := p.peek()
	switch tok.Kind {
	case token.KindIntLiteral:
		return p.literal(start, ast.LitInt)
	case token.KindRealLiteral:
		return p.literal(start, ast.LitReal)
	case token.KindTimeLiteral:
		return p.literal(start, ast.LitTime)
	case token.KindUnbasedUnsizedLiteral:
		return p.literal(start, ast.LitUnbasedUnsized)
	case token.KindStringLiteral:
		return p.literal(start, ast.LitString)
	case token.KindDollar:
		return p.literal(start, ast.LitUnbounded)
	case token.KindSystemIdent:
		p.advance()
		return finish(p, start, &ast.Ident{Name: tok.Text})
	case token.KindIdent:
		p.advance()
		var e ast.Expr = finish(p, start, &ast.Ident{Name: tok.Text})
		if p.peek().Kind == token.KindHash && p.peekAt(1).Kind == token.KindLParen {
			e = finish(p, start, &ast.ParameterizedName{Name: tok.Text, Overrides: p.parseParamValueList()})
		}
		return e
	case token.KindLParen:
		return p.parseParen()
	case token.KindLBrace:
		return p.parseBraced()
	case token.KindTickLBrace:
		return p.parseAssignPattern(nil, start)
	case token.KindKeyword:
		return p.parseKeywordPrimary()
	}
	return p.badPrimary()
}

func (p *parser) literal(start int, kind ast.LiteralKind) ast.Expr {
	tok := p.advance()
	return finish(p, start, &ast.Literal{Kind: kind, Text: tok.Text})
}

func (p *parser) parseKeywordPrimary() ast.Expr {
	start := p.pos
	tok := p.peek()
	switch tok.Text {
	case "null":
		p.advance()
		return finish(p, start, &ast.Literal{Kind: ast.LitNull, Text: "null"})
	case "this":
		p.advance()
		return finish(p, start, &ast.This{})
	case "super":
		p.advance()
		return finish(p, start, &ast.Super{})
	case "new":
		return p.parseNew(nil, start)
	case "tagged":
		return p.parseTagged()
	case "type":
		return p.parseTypeRef()
	case "local":
		if p.peekAt(1).Kind == token.KindColonColon {
			p.advance()
			return finish(p, start, &ast.Ident{Name: "local"})
		}
	}
	if isExprTypeKeyword(tok.Text) {
		return p.parseTypeOperand()
	}
	return p.badPrimary()
}

// parseTypeOperand parses a data type in expression position as an
// ast.TypeExpr. It reuses the declaration parser's parseTypeBase for the
// name and sign, but parses the packed dimensions itself (parseTypeDims):
// parseDims keeps unvalidated raw tokens and ignores a missing ']', which
// would let garbage through as a "clean" type.
func (p *parser) parseTypeOperand() ast.Expr {
	start := p.pos
	first := p.peek()
	if first.Text == "virtual" {
		return p.parseVirtualTypeOperand(start)
	}
	t := p.parseTypeBase()
	if (t.Signed || t.Unsigned) && (t.PackageQualifier != "" || (t.Name != "" && !token.Keywords[t.Name])) {
		// parseTypeBase accepts a sign next to any name, since in a
		// declaration the token after "signed" might still be the declared
		// name. Here there is no declarator, and only built-in integer
		// types take a sign (LRM 6.11).
		p.errorf(first, "signed/unsigned cannot qualify the user-defined type %q", t.Name)
	}
	t.PackedDims = p.parseTypeDims()
	return finish(p, start, &ast.TypeExpr{Type: t})
}

// parseVirtualTypeOperand parses "virtual [interface] Name[.modport]" in
// expression position, as in "uvm_resource_db#(virtual clk_if)::set(...)".
func (p *parser) parseVirtualTypeOperand(start int) ast.Expr {
	p.advance() // "virtual"
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "interface" {
		p.advance()
	}
	t := p.parseTypeBase()
	t.Virtual = true
	if t.Name == "" {
		p.errorf(p.peek(), "expected an interface name after 'virtual', found %s", describeTok(p.peek()))
	}
	if p.peek().Kind == token.KindDot && p.peekAt(1).Kind == token.KindIdent {
		p.advance()
		t.Modport = p.advance().Text
	}
	t.PackedDims = p.parseTypeDims()
	return finish(p, start, &ast.TypeExpr{Type: t})
}

// parseTypeDims parses zero or more "[expr]", "[expr:expr]" or "[]"
// dimensions. Each bound is parsed as an expression, so it is validated and
// any error reported; the tree is stored in LeftExpr/RightExpr and the raw
// tokens are kept on the ast.Dim as the declaration parser does.
func (p *parser) parseTypeDims() []ast.Dim {
	var dims []ast.Dim
	for p.peek().Kind == token.KindLBrack {
		p.advance() // '['
		var d ast.Dim
		if p.peek().Kind != token.KindRBrack {
			from := p.pos
			d.LeftExpr = p.parseExpr()
			d.Left = p.toks[from:p.pos]
			if p.peek().Kind == token.KindColon {
				p.advance()
				from = p.pos
				d.RightExpr = p.parseExpr()
				d.Right = p.toks[from:p.pos]
			} else {
				d.LeftExpr = markTypeOrExpr(d.LeftExpr) // a lone name: size or index type
			}
		}
		p.expectKind(token.KindRBrack, "']'")
		dims = append(dims, d)
	}
	return dims
}

// parseExprOrType parses an operand of a position that accepts a type or a
// value. A data-type keyword already comes back from parseExpr as a
// TypeExpr; a name that could be either is wrapped in an ast.TypeOrExpr.
func (p *parser) parseExprOrType() ast.Expr {
	e := p.parseExpr()
	return markTypeOrExpr(e)
}

func markTypeOrExpr(e ast.Expr) ast.Expr {
	switch e.(type) {
	case *ast.Ident, *ast.ScopedName, *ast.ParameterizedName:
		w := &ast.TypeOrExpr{X: e}
		w.SetPos(e.Pos())
		return w
	}
	return e
}

// parseParen parses "( expr )", "( lhs op= rhs )" (an assignment
// expression, LRM 11.3.6) or "( min : typ : max )" (LRM 11.11).
func (p *parser) parseParen() ast.Expr {
	start := p.pos
	p.advance() // '('
	innerStart := p.pos
	var inner ast.Expr
	if p.inProp > 0 {
		inner = p.parsePropBP(pbpLowest)
		if p.peek().Kind == token.KindComma {
			// "(seq, a = 1, f())": match items on a sequence (LRM 16.10).
			m := &ast.SeqMatch{X: inner}
			for p.peek().Kind == token.KindComma {
				p.advance()
				m.Items = append(m.Items, p.parseMatchItem())
			}
			inner = finish(p, innerStart, m)
		}
	} else {
		inner = p.parseExpr()
	}

	switch tok := p.peek(); {
	case isAssignOp(tok.Kind):
		p.advance()
		inner = finish(p, innerStart, &ast.AssignExpr{Op: tok.Text, LHS: inner, RHS: p.parseExpr()})
	case tok.Kind == token.KindColon:
		p.advance()
		typ := p.parseExpr()
		var max ast.Expr
		if p.expectKind(token.KindColon, "':' in a min:typ:max triple") {
			max = p.parseExpr()
		} else {
			max = p.emptyBad()
		}
		inner = finish(p, innerStart, &ast.MinTypMax{Min: inner, Typ: typ, Max: max})
	}
	p.expectKind(token.KindRParen, "')'")
	return finish(p, start, &ast.Paren{X: inner})
}

// parseBraced parses everything that opens with '{': a concatenation, a
// replication, or a streaming concatenation.
func (p *parser) parseBraced() ast.Expr {
	start := p.pos
	p.advance() // '{'

	if tok := p.peek(); tok.Kind == token.KindShl || tok.Kind == token.KindShr {
		return p.parseStreaming(start)
	}
	if p.peek().Kind == token.KindRBrace {
		p.advance()
		return finish(p, start, &ast.Concat{})
	}

	first := p.parseExpr()
	if p.peek().Kind == token.KindLBrace { // "{ count { elems } }"
		p.advance()
		elems := p.parseExprList()
		p.expectKind(token.KindRBrace, "'}' to close the replicated list")
		p.expectKind(token.KindRBrace, "'}' to close the replication")
		return finish(p, start, &ast.Replicate{Count: first, Elems: elems})
	}

	elems := []ast.Expr{first}
	for p.peek().Kind == token.KindComma {
		p.advance()
		elems = append(elems, p.parseExpr())
	}
	p.expectKind(token.KindRBrace, "'}' to close the concatenation")
	return finish(p, start, &ast.Concat{Elems: elems})
}

// parseExprList parses a possibly empty comma-separated expression list up
// to (not including) the closing delimiter, which the caller consumes.
func (p *parser) parseExprList() []ast.Expr {
	var out []ast.Expr
	if p.peek().Kind == token.KindRBrace || p.peek().Kind == token.KindRParen {
		return out
	}
	for {
		out = append(out, p.parseExpr())
		if p.peek().Kind != token.KindComma {
			return out
		}
		p.advance()
	}
}

// parseStreaming parses "{ << [slice] { elems } }" with the opening '{'
// already consumed (LRM 11.4.14).
func (p *parser) parseStreaming(start int) ast.Expr {
	op := p.advance() // "<<" or ">>"
	s := &ast.Streaming{Op: op.Text}
	if p.peek().Kind != token.KindLBrace {
		s.SliceSize = p.parseExprOrType()
	}
	if p.expectKind(token.KindLBrace, "'{' to open the streamed list") {
		for {
			elemStart := p.pos
			elem := p.parseExpr()
			if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "with" && p.peekAt(1).Kind == token.KindLBrack {
				elem = p.parseWithRange(elem, elemStart)
			}
			s.Elems = append(s.Elems, elem)
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
		p.expectKind(token.KindRBrace, "'}' to close the streamed list")
	}
	p.expectKind(token.KindRBrace, "'}' to close the streaming concatenation")
	return finish(p, start, s)
}

// parseWithRange parses the "with [ ... ]" tail of a streaming element; the
// cursor is on "with".
func (p *parser) parseWithRange(x ast.Expr, start int) ast.Expr {
	p.advance() // "with"
	p.advance() // '['
	w := &ast.WithRange{X: x, Left: p.parseExpr()}
	switch tok := p.peek(); tok.Kind {
	case token.KindColon, token.KindPlusColon, token.KindMinusColon:
		p.advance()
		w.Kind = rangeKindOf(tok.Kind)
		w.Right = p.parseExpr()
	}
	p.expectKind(token.KindRBrack, "']'")
	return finish(p, start, w)
}

func rangeKindOf(k token.Kind) ast.RangeKind {
	switch k {
	case token.KindPlusColon:
		return ast.RangePlus
	case token.KindMinusColon:
		return ast.RangeMinus
	}
	return ast.RangeColon
}

// parseAssignPattern parses "'{ items }" (LRM 10.9); the cursor is on the
// "'{" token. typ is the "T" of "T'{ ... }", or nil.
func (p *parser) parseAssignPattern(typ ast.Expr, start int) ast.Expr {
	p.advance() // "'{"
	ap := &ast.AssignPattern{Type: typ}
	if p.peek().Kind != token.KindRBrace {
		for {
			ap.Items = append(ap.Items, p.parsePatternItem())
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	}
	p.expectKind(token.KindRBrace, "'}' to close the assignment pattern")
	return finish(p, start, ap)
}

func (p *parser) parsePatternItem() ast.Expr {
	start := p.pos
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "default" && p.peekAt(1).Kind == token.KindColon {
		p.advance()
		p.advance()
		return finish(p, start, &ast.PatternItem{IsDefault: true, Value: p.parseExpr()})
	}
	e := p.parseExpr()
	switch p.peek().Kind {
	case token.KindColon:
		p.advance()
		return finish(p, start, &ast.PatternItem{Key: e, Value: p.parseExpr()})
	case token.KindLBrace: // "count { elems }"
		p.advance()
		elems := p.parseExprList()
		p.expectKind(token.KindRBrace, "'}' to close the replicated list")
		return finish(p, start, &ast.Replicate{Count: e, Elems: elems})
	}
	return e
}

// parseNew parses a constructor, "new", "new(args)", "new[size](init)" or
// "new expr", with the cursor on "new". scope is the "C" of "C::new".
func (p *parser) parseNew(scope ast.Expr, start int) ast.Expr {
	p.advance() // "new"
	switch tok := p.peek(); {
	case tok.Kind == token.KindLBrack:
		p.advance()
		n := &ast.NewArray{Size: p.parseExpr()}
		p.expectKind(token.KindRBrack, "']'")
		if p.peek().Kind == token.KindLParen {
			p.advance()
			n.Init = p.parseExpr()
			p.expectKind(token.KindRParen, "')'")
		}
		return finish(p, start, n)
	case tok.Kind == token.KindLParen:
		p.advance()
		return finish(p, start, &ast.New{Scope: scope, Args: p.parseCallArgs(false)})
	case startsExpr(tok):
		return finish(p, start, &ast.New{Scope: scope, Copy: p.parseExpr()})
	}
	return finish(p, start, &ast.New{Scope: scope})
}

// parseTagged parses "tagged Member [primary]" (LRM 7.3.2), cursor on
// "tagged".
func (p *parser) parseTagged() ast.Expr {
	start := p.pos
	p.advance() // "tagged"
	t := &ast.Tagged{}
	if tok := p.peek(); tok.Kind == token.KindIdent || tok.Kind == token.KindKeyword {
		p.advance()
		t.Member = tok.Text
	} else {
		p.expectKind(token.KindIdent, "a union member name after 'tagged'")
	}
	if startsPrimary(p.peek()) {
		// The operand recurses without passing through parseExprBP, so it
		// needs its own depth accounting ("tagged A tagged A tagged ...").
		if !p.enterExpr() {
			t.X = p.tooDeep()
		} else {
			operandStart := p.pos
			t.X = p.parsePostfix(p.parsePrimary(), operandStart)
			p.leaveExpr()
		}
	}
	return finish(p, start, t)
}

// parseTypeRef parses "type(expr_or_type)" (LRM 6.23), cursor on "type".
func (p *parser) parseTypeRef() ast.Expr {
	start := p.pos
	p.advance() // "type"
	r := &ast.TypeRef{}
	if p.expectKind(token.KindLParen, "'(' after type") {
		r.X = p.parseExprOrType()
		p.expectKind(token.KindRParen, "')'")
	} else {
		r.X = p.emptyBad()
	}
	return finish(p, start, r)
}

// parseParamValueList parses "#( ... )" after a class name; the cursor is
// on '#'. Each entry is "value" or ".name(value)", where value is a type or
// an expression. The raw tokens stay on the ParamOverride beside ValueExpr.
func (p *parser) parseParamValueList() []ast.ParamOverride {
	p.advance() // '#'
	p.advance() // '('
	var out []ast.ParamOverride
	if p.peek().Kind != token.KindRParen {
		for {
			out = append(out, p.parseParamValue())
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	}
	p.expectKind(token.KindRParen, "')' to close the parameter value list")
	return out
}

func (p *parser) parseParamValue() ast.ParamOverride {
	itemStart := p.pos
	var o ast.ParamOverride
	if p.peek().Kind == token.KindDot && p.peekAt(1).Kind == token.KindIdent {
		p.advance() // '.'
		nameTok := p.advance()
		o.Name = nameTok.Text
		o.Position = namePosition(nameTok)
		if p.expectKind(token.KindLParen, "'(' after the parameter name") {
			if p.peek().Kind != token.KindRParen {
				valStart := p.pos
				o.ValueExpr = p.parseExprOrType()
				o.Value = p.toks[valStart:p.pos]
			}
			p.expectKind(token.KindRParen, "')'")
		}
		return o
	}
	o.ValueExpr = p.parseExprOrType()
	o.Value = p.toks[itemStart:p.pos]
	o.Position = p.spanFrom(itemStart)
	return o
}

// parsePostfix applies selects, member access, scope resolution, calls,
// casts, typed assignment patterns, and postfix ++/-- to lhs, which began at
// token index start.
func (p *parser) parsePostfix(lhs ast.Expr, start int) ast.Expr {
	for {
		tok := p.peek()
		switch tok.Kind {
		case token.KindLBrack:
			if p.inProp > 0 && p.atRepeat() {
				return lhs // a sequence repetition, read by the property parser
			}
			lhs = p.parseSelect(lhs, start)

		case token.KindDot:
			p.advance()
			member := p.peek()
			if member.Kind != token.KindIdent && member.Kind != token.KindKeyword {
				p.errorf(member, "expected a member name after '.', found %s", describeTok(member))
				return finish(p, start, &ast.MemberSelect{X: lhs})
			}
			p.advance()
			lhs = finish(p, start, &ast.MemberSelect{X: lhs, Member: member.Text})

		case token.KindColonColon:
			if !isScope(lhs) {
				return lhs
			}
			p.advance()
			member := p.peek()
			switch {
			case member.Kind == token.KindKeyword && member.Text == "new":
				lhs = p.parseNew(lhs, start)
			case member.Kind == token.KindIdent || member.Kind == token.KindKeyword:
				p.advance()
				if member.Kind == token.KindIdent && p.peek().Kind == token.KindHash && p.peekAt(1).Kind == token.KindLParen {
					// "pkg::C#(8)": a package-qualified parameterized class.
					lhs = finish(p, start, &ast.ParameterizedName{Scope: lhs, Name: member.Text, Overrides: p.parseParamValueList()})
				} else {
					lhs = finish(p, start, &ast.ScopedName{Scope: lhs, Name: member.Text})
				}
			default:
				p.errorf(member, "expected a name after '::', found %s", describeTok(member))
				return finish(p, start, &ast.ScopedName{Scope: lhs})
			}

		case token.KindLParen:
			if !isCallable(lhs) {
				return lhs
			}
			lhs = p.parseCall(lhs, start)

		case token.KindTick:
			if p.peekAt(1).Kind != token.KindLParen {
				return lhs
			}
			p.advance() // '
			p.advance() // (
			x := p.parseExpr()
			p.expectKind(token.KindRParen, "')' to close the cast")
			lhs = finish(p, start, &ast.Cast{Type: markTypeOrExpr(lhs), X: x})

		case token.KindTickLBrace:
			if !isPatternType(lhs) {
				return lhs
			}
			lhs = p.parseAssignPattern(lhs, start)

		case token.KindInc, token.KindDec:
			if !isLvalueShape(lhs) {
				return lhs
			}
			p.advance()
			lhs = finish(p, start, &ast.IncDec{Op: tok.Text, X: lhs})

		case token.KindKeyword:
			// An array method called without parentheses but with an iterator
			// clause, "q.find_first with (item > 1)" (LRM 7.12.1): the argument
			// list is optional there, so this is a call with no arguments.
			if _, ok := lhs.(*ast.MemberSelect); !ok || tok.Text != "with" ||
				(p.peekAt(1).Kind != token.KindLParen && p.peekAt(1).Kind != token.KindLBrace) {
				return lhs
			}
			c := &ast.Call{Func: lhs}
			p.parseWithClause(c)
			lhs = finish(p, start, c)

		default:
			return lhs
		}
	}
}

// parseSelect parses "[i]", "[l:r]", "[l+:r]" or "[l-:r]" after lhs.
func (p *parser) parseSelect(lhs ast.Expr, start int) ast.Expr {
	p.advance() // '['
	left := p.parseExpr()
	switch tok := p.peek(); tok.Kind {
	case token.KindColon, token.KindPlusColon, token.KindMinusColon:
		p.advance()
		right := p.parseExpr()
		p.expectKind(token.KindRBrack, "']'")
		return finish(p, start, &ast.RangeSelect{X: lhs, Kind: rangeKindOf(tok.Kind), Left: left, Right: right})
	}
	p.expectKind(token.KindRBrack, "']'")
	return finish(p, start, &ast.Index{X: lhs, Index: left})
}

// parseCall parses "( args ) [with ( expr )]" after a callable lhs.
func (p *parser) parseCall(fn ast.Expr, start int) ast.Expr {
	p.advance() // '('
	id, _ := fn.(*ast.Ident)
	c := &ast.Call{Func: fn, Args: p.parseCallArgs(id != nil && typeArgFuncs[id.Name])}
	p.parseWithClause(c)
	return finish(p, start, c)
}

// parseWithClause parses the "with" tail of a call into c: an array method's
// iterator expression "with (x > 3)", an inline constraint block "with { ...
// }", or the two together "with (v) { ... }" (LRM 7.12, 18.7). The cursor is
// where "with" would be; nothing is consumed if it is not there.
func (p *parser) parseWithClause(c *ast.Call) {
	tok := p.peek()
	if tok.Kind != token.KindKeyword || tok.Text != "with" {
		return
	}
	switch p.peekAt(1).Kind {
	case token.KindLParen:
		p.advance() // "with"
		c.With = p.parseParen()
		if p.peek().Kind == token.KindLBrace {
			c.Constraints = p.parseConstraintBlock()
		}
	case token.KindLBrace:
		p.advance() // "with"
		c.Constraints = p.parseConstraintBlock()
	}
}

// parseConstraintBlock parses "{ ... }" into an ast.ConstraintBlock, keeping the
// raw tokens beside the parsed items. The
// cursor is on '{'. The scan stops at a block end or the end of input, since a
// constraint block contains no begin/end, so a missing '}' cannot run on.
func (p *parser) parseConstraintBlock() *ast.ConstraintBlock {
	start := p.pos
	p.advance() // '{'
	from := p.pos
	depth := 0
scan:
	for {
		tok := p.peek()
		switch {
		case tok.Kind == token.KindEOF, isStmtStop(tok):
			p.errorf(tok, "unterminated constraint block, expected '}'")
			b := &ast.ConstraintBlock{Tokens: p.toks[from:p.pos]}
			b.Items = p.parseConstraintItems(b.Tokens)
			b.SetPos(p.spanFrom(start))
			return b
		case tok.Kind == token.KindLBrace || tok.Kind == token.KindTickLBrace:
			depth++
		case tok.Kind == token.KindRBrace:
			if depth == 0 {
				break scan
			}
			depth--
		}
		p.advance()
	}
	b := &ast.ConstraintBlock{Tokens: p.toks[from:p.pos]}
	b.Items = p.parseConstraintItems(b.Tokens)
	p.advance() // '}'
	b.SetPos(p.spanFrom(start))
	return b
}

// parseCallArgs parses an argument list with the '(' already consumed,
// through the closing ')'. An omitted argument ("f(a,,c)") is an
// ast.Empty. If typeFirst, the first argument may be a type.
func (p *parser) parseCallArgs(typeFirst bool) []ast.Expr {
	var args []ast.Expr
	if p.peek().Kind == token.KindRParen {
		p.advance()
		return args
	}
	for {
		args = append(args, p.parseArg(typeFirst && len(args) == 0))
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	p.expectKind(token.KindRParen, "')' to close the argument list")
	return args
}

func (p *parser) parseArg(maybeType bool) ast.Expr {
	start := p.pos
	tok := p.peek()
	switch {
	case tok.Kind == token.KindComma || tok.Kind == token.KindRParen:
		return finish(p, start, &ast.Empty{})
	case tok.Kind == token.KindDot && p.peekAt(1).Kind == token.KindIdent:
		p.advance() // '.'
		na := &ast.NamedArg{Name: p.advance().Text}
		if p.peek().Kind == token.KindLParen {
			p.advance()
			if p.peek().Kind != token.KindRParen {
				na.Value = p.parseExpr()
			}
			p.expectKind(token.KindRParen, "')'")
		}
		return finish(p, start, na)
	case maybeType:
		return p.parseExprOrType()
	}
	return p.parseExpr()
}

func isScope(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.ParameterizedName, *ast.ScopedName:
		return true
	}
	return false
}

func isCallable(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.ScopedName, *ast.MemberSelect:
		return true
	}
	return false
}

// isPatternType reports whether e can be the "T" of "T'{...}".
func isPatternType(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.ScopedName, *ast.ParameterizedName, *ast.TypeExpr:
		return true
	}
	return false
}

func isLvalueShape(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.ScopedName, *ast.MemberSelect, *ast.Index, *ast.RangeSelect:
		return true
	}
	return false
}

// badPrimary reports a token that cannot start an expression. It consumes
// that token unless it is a delimiter or separator the enclosing construct
// needs (see exprStopper), so list loops always make progress by consuming
// their commas, never by luck.
func (p *parser) badPrimary() ast.Expr {
	start := p.pos
	tok := p.peek()
	p.errorf(tok, "expected an expression, found %s", describeTok(tok))
	if !exprStopper(tok) {
		p.advance()
	}
	return p.bad(start)
}

// emptyBad is a zero-width BadExpr for a required operand that was missing
// after an error already reported at the call site.
func (p *parser) emptyBad() ast.Expr {
	return p.bad(p.pos)
}

func (p *parser) bad(start int) ast.Expr {
	b := &ast.BadExpr{Tokens: p.toks[start:p.pos]}
	b.SetPos(p.spanFrom(start))
	return b
}

func (p *parser) enterExpr() bool {
	if p.exprDepth >= maxExprDepth {
		return false
	}
	p.exprDepth++
	return true
}

func (p *parser) leaveExpr() { p.exprDepth-- }

// tooDeep reports excessive nesting once and skips the rest of the current
// group (to the next unmatched closer, comma, or semicolon) as one BadExpr,
// so the recursion unwinds instead of continuing to descend.
func (p *parser) tooDeep() ast.Expr {
	start := p.pos
	p.errorf(p.peek(), "expression nested more than %d levels deep", maxExprDepth)
	depth := 0
scan:
	for {
		switch p.peek().Kind {
		case token.KindEOF:
			break scan
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth == 0 {
				break scan
			}
			depth--
		case token.KindComma, token.KindSemi:
			if depth == 0 {
				break scan
			}
		}
		p.advance()
	}
	return p.bad(start)
}
