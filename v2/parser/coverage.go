package parser

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/token"
)

// parseCovergroup parses "covergroup name [(ports)] [event]; items endgroup"
// (LRM 19.3), the cursor on "covergroup".
//
// Where the covergroup ends is still found by skipToKeyword, so recovery is
// what it was when covergroups were skipped. The header and the items are then
// parsed from the tokens it consumed by bounded sub-parsers, with errors
// suppressed. A name on a covergroup that "extends" another is the new
// covergroup's own (LRM 19.5.1).
func (p *parser) parseCovergroup() (ast.Decl, bool) {
	p.advance() // "covergroup"
	n := &ast.Covergroup{}
	extends := false
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "extends" {
		p.advance()
		extends = true
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	n.Name = nameTok.Text
	if extends {
		n.Extends = nameTok.Text // "covergroup extends cg;" redefines cg (LRM 19.5.1)
	}
	n.Position = namePosition(nameTok)

	hdr := p.covergroupHeader()
	bodyStart := p.pos
	p.skipToKeyword("endgroup", nil)
	body := p.toks[bodyStart:p.pos]
	if k := len(body); k > 0 && body[k-1].Kind == token.KindKeyword && body[k-1].Text == "endgroup" {
		n.EndLine, n.EndCharacter = body[k-1].Line, body[k-1].Character
		body = body[:k-1]
	}
	p.skipEndLabel()

	sub := p.newSubParser(hdr)
	if sub.peek().Kind == token.KindLParen {
		n.Ports = sub.parseArgList()
	}
	switch tok := sub.peek(); {
	case tok.Kind == token.KindKeyword && tok.Text == "with":
		sub.advance()
		sub.advance() // "function"
		sub.advance() // "sample"
		n.Sample = true
		n.SamplePorts = sub.parseArgList()
	case tok.Kind == token.KindAt:
		if nxt := sub.peekAt(1); nxt.Kind == token.KindAt {
			n.BlockEvent = true // "@@(begin f)": block events are not modeled
		} else {
			n.Event = sub.parseTimingControl()
		}
	}

	items := p.newSubParser(body)
	for items.peek().Kind != token.KindEOF {
		before := items.pos
		if d := items.parseCoverItem(); d != nil {
			n.Items = append(n.Items, d)
		}
		if items.pos == before {
			items.advance() // guarantee progress
		}
	}
	p.suppressed = append(p.suppressed, sub.errs...)
	p.suppressed = append(p.suppressed, items.errs...)
	p.suppressed = append(p.suppressed, items.suppressed...)
	return n, true
}

// covergroupHeader consumes the covergroup header through its ';' and returns
// its tokens without the ';'. It stops, reporting the missing ';', at the first
// token that can only start a covergroup item or end the group.
func (p *parser) covergroupHeader() []preprocessor.Token {
	start := p.pos
	depth := 0
	for {
		tok := p.peek()
		switch {
		case tok.Kind == token.KindEOF:
			p.errorf(tok, "unexpected end of file, expected ';'")
			return p.toks[start:p.pos]
		case tok.Kind == token.KindSemi && depth == 0:
			hdr := p.toks[start:p.pos]
			p.advance()
			return hdr
		case tok.Kind == token.KindKeyword && depth == 0 &&
			(tok.Text == "coverpoint" || tok.Text == "cross" || tok.Text == "endgroup" || isEndKeyword(tok.Text)):
			p.errorf(tok, "expected ';' before %q", tok.Text)
			return p.toks[start:p.pos]
		case tok.Kind == token.KindLParen || tok.Kind == token.KindLBrack || tok.Kind == token.KindLBrace:
			depth++
		case tok.Kind == token.KindRParen || tok.Kind == token.KindRBrack || tok.Kind == token.KindRBrace:
			if depth > 0 {
				depth--
			}
		}
		p.advance()
	}
}

// parseCoverItem parses one covergroup item (LRM 19.3): an option, a coverpoint
// or a cross, with the optional "[type] label :" before the last two.
func (p *parser) parseCoverItem() ast.Decl {
	p.skipAttributeInstances()
	tok := p.peek()
	if tok.Kind == token.KindIdent && p.peekAt(1).Kind == token.KindDot && (tok.Text == "option" || tok.Text == "type_option") {
		return p.parseCoverOption()
	}
	// The keyword that decides the item, after any "type label :" prefix.
	idx := -1
	depth := 0
scan:
	for i := p.pos; i < len(p.toks); i++ {
		switch t := p.toks[i]; t.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindSemi:
			break scan
		case token.KindKeyword:
			if depth == 0 && (t.Text == "coverpoint" || t.Text == "cross") {
				idx = i
				break scan
			}
		}
	}
	if idx < 0 {
		p.errorf(tok, "unexpected %s in a covergroup", describeTok(tok))
		p.skipHeaderToSemi()
		return nil
	}
	prefix := p.toks[p.pos:idx]
	var label preprocessor.Token
	var typ ast.Type
	if k := len(prefix); k >= 2 && prefix[k-1].Kind == token.KindColon && prefix[k-2].Kind == token.KindIdent {
		label = prefix[k-2]
		if k > 2 {
			ts := p.newSubParser(prefix[:k-2])
			typ = ts.parseDataType()
			p.suppressed = append(p.suppressed, ts.errs...)
		}
	}
	pos := namePosition(p.toks[idx])
	if label.Text != "" {
		pos = namePosition(label)
	}
	p.pos = idx
	if p.toks[idx].Text == "cross" {
		return p.parseCoverCross(label.Text, pos)
	}
	return p.parseCoverPoint(label.Text, typ, pos)
}

// parseCoverOption parses "option.name = expr;" and "type_option.name = expr;".
func (p *parser) parseCoverOption() ast.Decl {
	kw := p.advance()
	p.advance() // '.'
	n := &ast.CoverOption{Type: kw.Text == "type_option"}
	n.Position = namePosition(kw)
	if name, ok := p.expectIdent(); ok {
		n.Name = name.Text
	}
	if p.expectKind(token.KindAssign, "'='") {
		n.Value = p.parseExpr()
	} else {
		n.Value = p.emptyBad()
	}
	p.expectSemi()
	return n
}

// parseCoverPoint parses "coverpoint expr [iff (cond)] { bins }" (LRM 19.5), the
// cursor on "coverpoint".
func (p *parser) parseCoverPoint(label string, typ ast.Type, pos ast.Position) ast.Decl {
	p.advance() // "coverpoint"
	n := &ast.CoverPoint{Name: label, Type: typ}
	n.Position = pos
	n.X = p.parseExpr()
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "iff" {
		p.advance()
		n.Iff = p.parseParenCond()
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
		return n
	}
	if !p.expectKind(token.KindLBrace, "'{' or ';' after the coverpoint") {
		return n
	}
	for {
		tok := p.peek()
		if tok.Kind == token.KindRBrace {
			p.advance()
			break
		}
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unterminated coverpoint, expected '}'")
			break
		}
		before := p.pos
		if d := p.parseBinOrOption(); d != nil {
			n.Bins = append(n.Bins, d)
		}
		if p.pos == before {
			p.advance()
		}
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
	}
	return n
}

func isBinsKeyword(tok preprocessor.Token) bool {
	return tok.Kind == token.KindKeyword && (tok.Text == "bins" || tok.Text == "illegal_bins" || tok.Text == "ignore_bins")
}

// parseBinOrOption parses one item of a coverpoint body (LRM 19.5.1).
func (p *parser) parseBinOrOption() ast.Decl {
	p.skipAttributeInstances()
	tok := p.peek()
	if tok.Kind == token.KindIdent && p.peekAt(1).Kind == token.KindDot && (tok.Text == "option" || tok.Text == "type_option") {
		return p.parseCoverOption()
	}
	if !(isBinsKeyword(tok) || (tok.Kind == token.KindKeyword && tok.Text == "wildcard")) {
		p.errorf(tok, "expected a bins declaration, found %s", describeTok(tok))
		p.skipHeaderToSemi()
		return nil
	}
	b := p.parseBinHead()
	if !p.expectKind(token.KindAssign, "'=' in a bins declaration") {
		p.skipHeaderToSemi()
		return b
	}
	switch tok := p.peek(); {
	case tok.Kind == token.KindKeyword && tok.Text == "default":
		p.advance()
		b.Kind = "default"
		if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "sequence" {
			p.advance()
			b.Kind = "default sequence"
		}
	case tok.Kind == token.KindLBrace:
		b.Kind = "values"
		b.Values = p.parseInsideSet()
		if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "with" {
			p.advance()
			b.With = p.parseParenCond()
		}
	case tok.Kind == token.KindLParen && p.atTransition():
		b.Kind = "trans"
		for {
			b.Trans = append(b.Trans, p.parseTransSet())
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	case tok.Kind == token.KindIdent && p.peekAt(1).Kind == token.KindKeyword && p.peekAt(1).Text == "with":
		b.Kind = "with"
		b.Source = p.parseHierName(p.pos)
		p.advance() // "with"
		b.With = p.parseParenCond()
	default:
		b.Kind = "set"
		b.Source = p.parseExpr()
	}
	if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "iff" {
		p.advance()
		b.Iff = p.parseParenCond()
	}
	p.expectSemi()
	return b
}

// parseBinHead parses "[wildcard] bins_keyword [name] [[size]]", leaving the
// cursor on the '='.
func (p *parser) parseBinHead() *ast.CoverBin {
	b := &ast.CoverBin{}
	first := p.peek()
	if first.Text == "wildcard" {
		b.Wildcard = true
		p.advance()
	}
	kw := p.advance()
	b.Keyword = kw.Text
	b.Position = namePosition(kw)
	if tok := p.peek(); tok.Kind == token.KindIdent {
		p.advance()
		b.Name = tok.Text
		b.Position = namePosition(tok)
	}
	if p.peek().Kind == token.KindLBrack {
		p.advance()
		b.Array = true
		if p.peek().Kind != token.KindRBrack {
			b.Size = p.parseExpr()
		}
		p.expectKind(token.KindRBrack, "']'")
	}
	return b
}

// atTransition reports whether the "(" under the cursor opens a transition
// list, "(a => b)", rather than a parenthesised set expression: a "=>" occurs
// directly inside the group.
func (p *parser) atTransition() bool {
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		t := p.toks[i]
		switch t.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			depth--
			if depth <= 0 {
				return false
			}
		case token.KindAssign:
			if depth == 1 && i+1 < len(p.toks) && isTransArrow(t, p.toks[i+1]) {
				return true
			}
		case token.KindSemi:
			return false
		}
	}
	return false
}

// isTransArrow reports whether a and b are the adjacent tokens "=" and ">" of
// the transition operator "=>".
func isTransArrow(a, b preprocessor.Token) bool {
	return a.Kind == token.KindAssign && b.Kind == token.KindGt && a.Line == b.Line && b.Character == a.Character+1
}

// parseTransSet parses "( step {=> step} )" (LRM 19.5.1.1), the cursor on '('.
func (p *parser) parseTransSet() ast.TransSet {
	start := p.pos
	set := ast.TransSet{}
	if !p.expectKind(token.KindLParen, "'('") {
		set.Position = p.spanFrom(start)
		return set
	}
	open := p.pos
	p.collectUntil(token.KindRParen)
	toks := p.toks[open:p.pos]
	p.expectKind(token.KindRParen, "')'")

	from, depth := 0, 0
	flush := func(to int) {
		set.Steps = append(set.Steps, p.parseTransStep(toks[from:to]))
	}
	for i, t := range toks {
		switch t.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindAssign:
			if depth == 0 && i+1 < len(toks) && isTransArrow(t, toks[i+1]) {
				flush(i)
				from = i + 2
			}
		}
	}
	flush(len(toks))
	set.Position = p.spanFrom(start)
	return set
}

// parseTransStep parses "value {, value} [repetition]".
func (p *parser) parseTransStep(toks []preprocessor.Token) ast.TransStep {
	step := ast.TransStep{}
	if len(toks) > 0 {
		step.Position = tokensSpan(toks)
	}
	sub := p.newSubParser(toks)
	sub.inProp++ // so "1 [*3]" is a repetition, not an index
	for sub.peek().Kind != token.KindEOF && !sub.atRepeat() {
		step.Values = append(step.Values, sub.parseRangeOrValue())
		if sub.peek().Kind != token.KindComma {
			break
		}
		sub.advance()
	}
	if sub.atRepeat() {
		if r, ok := sub.parseRepeat(nil, sub.pos).(*ast.SeqRepeat); ok {
			step.RepKind, step.RepMin, step.RepMax = r.Kind, r.Min, r.Max
		}
	}
	if tok := sub.peek(); tok.Kind != token.KindEOF {
		sub.errorf(tok, "unexpected %s in a transition step", describeTok(tok))
	}
	p.suppressed = append(p.suppressed, sub.errs...)
	return step
}

// parseCoverCross parses "cross a, b [iff (cond)] { body }" (LRM 19.6), the
// cursor on "cross".
func (p *parser) parseCoverCross(label string, pos ast.Position) ast.Decl {
	p.advance() // "cross"
	n := &ast.CoverCross{Name: label}
	n.Position = pos
	for {
		n.Items = append(n.Items, p.parseExpr())
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "iff" {
		p.advance()
		n.Iff = p.parseParenCond()
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
		return n
	}
	if !p.expectKind(token.KindLBrace, "'{' or ';' after the cross") {
		return n
	}
	for {
		tok := p.peek()
		if tok.Kind == token.KindRBrace {
			p.advance()
			break
		}
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unterminated cross, expected '}'")
			break
		}
		before := p.pos
		switch {
		case tok.Kind == token.KindIdent && p.peekAt(1).Kind == token.KindDot && (tok.Text == "option" || tok.Text == "type_option"):
			n.Body = append(n.Body, p.parseCoverOption())
		case isBinsKeyword(tok):
			n.Body = append(n.Body, p.parseBinsSelection())
		default:
			if decls, ok := p.parseDecl(); ok {
				n.Body = append(n.Body, decls...)
			} else {
				p.errorf(tok, "unexpected %s in a cross body", describeTok(tok))
			}
		}
		if p.pos == before {
			p.advance()
		}
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
	}
	return n
}

// parseBinsSelection parses "bins_keyword name = select_expression [iff (cond)];"
// in a cross body (LRM 19.6.1.2).
func (p *parser) parseBinsSelection() ast.Decl {
	b := p.parseBinHead()
	b.Kind = "select"
	if p.expectKind(token.KindAssign, "'='") {
		b.Source = p.parseSelectOr()
	}
	if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "iff" {
		p.advance()
		b.Iff = p.parseParenCond()
	}
	p.expectSemi()
	return b
}

// parseSelectOr parses a cross select expression (LRM 19.6.1.2):
// "binsof(x) intersect {..}", "!", "&&", "||", parentheses, "with (expr)" and
// "matches expr". The pieces reuse Binary, Unary, Call and Paren nodes, with the
// keywords as the Binary operators.
func (p *parser) parseSelectOr() ast.Expr {
	start := p.pos
	lhs := p.parseSelectAnd()
	for p.peek().Kind == token.KindOrOr {
		p.advance()
		lhs = finish(p, start, &ast.Binary{Op: "||", X: lhs, Y: p.parseSelectAnd()})
	}
	return lhs
}

func (p *parser) parseSelectAnd() ast.Expr {
	start := p.pos
	lhs := p.parseSelectUnary()
	for p.peek().Kind == token.KindAndAnd {
		p.advance()
		lhs = finish(p, start, &ast.Binary{Op: "&&", X: lhs, Y: p.parseSelectUnary()})
	}
	return lhs
}

func (p *parser) parseSelectUnary() ast.Expr {
	start := p.pos
	if !p.enterExpr() {
		return p.tooDeep()
	}
	defer p.leaveExpr()
	var e ast.Expr
	switch tok := p.peek(); {
	case tok.Kind == token.KindBang:
		p.advance()
		return finish(p, start, &ast.Unary{Op: "!", X: p.parseSelectUnary()})
	case tok.Kind == token.KindLParen:
		p.advance()
		inner := p.parseSelectOr()
		p.expectKind(token.KindRParen, "')'")
		e = finish(p, start, &ast.Paren{X: inner})
	case tok.Kind == token.KindKeyword && tok.Text == "binsof":
		p.advance()
		fn := finish(p, start, &ast.Ident{Name: "binsof"})
		call := &ast.Call{Func: fn}
		if p.expectKind(token.KindLParen, "'(' after binsof") {
			call.Args = []ast.Expr{p.parseExpr()}
			p.expectKind(token.KindRParen, "')'")
		}
		e = finish(p, start, call)
	default:
		e = p.parseUnary()
	}
	for {
		tok := p.peek()
		if tok.Kind != token.KindKeyword {
			return e
		}
		switch tok.Text {
		case "intersect":
			p.advance()
			setStart := p.pos
			set := finish(p, setStart, &ast.Concat{Elems: p.parseInsideSet()})
			e = finish(p, start, &ast.Binary{Op: "intersect", X: e, Y: set})
		case "with":
			p.advance()
			e = finish(p, start, &ast.Binary{Op: "with", X: e, Y: p.parseParenCond()})
		case "matches":
			p.advance()
			e = finish(p, start, &ast.Binary{Op: "matches", X: e, Y: p.parseUnary()})
		default:
			return e
		}
	}
}
