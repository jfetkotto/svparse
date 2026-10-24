package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// Binding powers of the property and sequence operators, lowest to highest
// (LRM table 16-1). The boolean leaves are ordinary expressions, parsed by
// parseExpr; these operators are all keywords or tokens parseExpr stops at.
const (
	pbpLowest     = iota
	pbpImpl       = 2  // |-> |=> #-# #=#   (right)
	pbpUntil      = 3  // until s_until until_with s_until_with implies (right)
	pbpIff        = 4  // iff (right)
	pbpOr         = 5  // or
	pbpAnd        = 6  // and
	pbpNot        = 7  // operand level of not and nexttime
	pbpIntersect  = 8  // intersect
	pbpWithin     = 9  // within
	pbpThroughout = 10 // throughout (right)
	pbpDelay      = 11 // ##
)

// propInfix reports the property or sequence binary operator under the cursor:
// its text, how many tokens it spans, its binding power and associativity.
func (p *parser) propInfix() (text string, n, bp int, right, ok bool) {
	tok := p.peek()
	switch tok.Kind {
	case token.KindImplies, token.KindImpliesNext:
		return tok.Text, 1, pbpImpl, true, true
	case token.KindHashHash:
		return "##", 1, pbpDelay, false, true
	case token.KindHash:
		// "#-#" and "#=#" (LRM 16.12.11) lex as three tokens.
		mid, last := p.peekAt(1), p.peekAt(2)
		if last.Kind == token.KindHash && (mid.Kind == token.KindSub || mid.Kind == token.KindAssign) &&
			mid.Line == tok.Line && mid.Character == tok.Character+1 {
			return "#" + mid.Text + "#", 3, pbpImpl, true, true
		}
	case token.KindKeyword:
		switch tok.Text {
		case "until", "s_until", "until_with", "s_until_with", "implies":
			return tok.Text, 1, pbpUntil, true, true
		case "iff":
			return tok.Text, 1, pbpIff, true, true
		case "or":
			return tok.Text, 1, pbpOr, false, true
		case "and":
			return tok.Text, 1, pbpAnd, false, true
		case "intersect":
			return tok.Text, 1, pbpIntersect, false, true
		case "within":
			return tok.Text, 1, pbpWithin, false, true
		case "throughout":
			return tok.Text, 1, pbpThroughout, true, true
		}
	}
	return "", 0, 0, false, false
}

// parsePropExpr parses a property or sequence expression (LRM 16.12, 16.7),
// including a leading clocking event or "disable iff". Inside it, a
// parenthesised group is itself a property expression, which is what lets
// "(a ##1 b)[*2]" and "(seq, x = 1)" parse.
func (p *parser) parsePropExpr() ast.Expr {
	p.inProp++
	defer func() { p.inProp-- }()
	return p.parsePropBP(pbpLowest)
}

func (p *parser) parsePropBP(minBP int) ast.Expr {
	start := p.pos
	if !p.enterExpr() {
		return p.tooDeep()
	}
	defer p.leaveExpr()

	lhs := p.parsePropOperand()
	for {
		text, n, bp, right, ok := p.propInfix()
		if !ok || bp < minBP {
			return lhs
		}
		if text == "##" {
			p.advance()
			d := &ast.SeqDelay{X: lhs}
			d.Min, d.Max, d.Sugar = p.parseDelayBound()
			d.Y = p.parsePropBP(pbpDelay + 1)
			lhs = finish(p, start, d)
			continue
		}
		for i := 0; i < n; i++ {
			p.advance()
		}
		next := bp + 1
		if right {
			next = bp
		}
		rhs := p.parsePropBP(next)
		lhs = finish(p, start, &ast.Binary{Op: text, X: lhs, Y: rhs})
	}
}

// parseDelayBound parses what follows "##": "N", "(expr)", "[lo:hi]", "[*]"
// or "[+]". It returns the bounds and the sugar form.
func (p *parser) parseDelayBound() (min, max ast.Expr, sugar string) {
	if p.peek().Kind != token.KindLBrack {
		return p.parsePrimary(), nil, ""
	}
	p.advance() // '['
	switch p.peek().Kind {
	case token.KindMul:
		p.advance()
		sugar = "*"
	case token.KindAdd:
		p.advance()
		sugar = "+"
	default:
		min = p.parseExpr()
		if p.peek().Kind == token.KindColon {
			p.advance()
			max = p.parseExpr()
		}
	}
	p.expectKind(token.KindRBrack, "']' to close the cycle delay")
	return min, max, sugar
}

// atRepeat reports whether the cursor is on the "[" of a sequence repetition,
// "[*", "[+]", "[->" or "[=" (LRM 16.9.2).
func (p *parser) atRepeat() bool {
	if p.peek().Kind != token.KindLBrack {
		return false
	}
	switch p.peekAt(1).Kind {
	case token.KindMul, token.KindArrow, token.KindAssign:
		return true
	case token.KindAdd:
		return p.peekAt(2).Kind == token.KindRBrack
	}
	return false
}

// parseRepeat parses one repetition suffix applied to x.
func (p *parser) parseRepeat(x ast.Expr, start int) ast.Expr {
	p.advance() // '['
	n := &ast.SeqRepeat{X: x}
	switch tok := p.advance(); tok.Kind {
	case token.KindMul:
		n.Kind = "*"
		if p.peek().Kind == token.KindRBrack {
			n.Sugar = "*"
		}
	case token.KindAdd:
		n.Kind, n.Sugar = "*", "+"
	case token.KindArrow:
		n.Kind = "->"
	default:
		n.Kind = "="
	}
	if n.Sugar == "" {
		n.Min = p.parseExpr()
		if p.peek().Kind == token.KindColon {
			p.advance()
			n.Max = p.parseExpr()
		}
	}
	p.expectKind(token.KindRBrack, "']' to close the repetition")
	return finish(p, start, n)
}

// parsePropOperand parses one operand: a prefix form, or a boolean or
// parenthesised leaf followed by any repetitions.
func (p *parser) parsePropOperand() ast.Expr {
	start := p.pos
	tok := p.peek()
	switch {
	case tok.Kind == token.KindAt:
		ctl := p.parseTimingControl()
		return finish(p, start, &ast.ClockedProp{Event: ctl, X: p.parsePropBP(pbpLowest)})
	case tok.Kind == token.KindHashHash:
		p.advance()
		d := &ast.SeqDelay{}
		d.Min, d.Max, d.Sugar = p.parseDelayBound()
		d.Y = p.parsePropBP(pbpDelay + 1)
		return finish(p, start, d)
	case tok.Kind == token.KindKeyword:
		if e := p.parsePropKeyword(start, tok.Text); e != nil {
			return e
		}
	}
	e := p.parseExpr()
	for p.atRepeat() {
		e = p.parseRepeat(e, start)
	}
	return e
}

// parsePropKeyword parses the property forms introduced by a keyword, or
// returns nil if the keyword begins an ordinary expression.
func (p *parser) parsePropKeyword(start int, kw string) ast.Expr {
	switch kw {
	case "disable":
		if p.peekAt(1).Text != "iff" {
			return nil
		}
		p.advance()
		p.advance()
		cond := p.parseParenCond()
		return finish(p, start, &ast.DisableIff{Cond: cond, X: p.parsePropBP(pbpLowest)})
	case "not":
		p.advance()
		return finish(p, start, &ast.PropPrefix{Op: kw, X: p.parsePropBP(pbpNot)})
	case "nexttime", "s_nexttime":
		p.advance()
		n := &ast.PropPrefix{Op: kw}
		if p.peek().Kind == token.KindLBrack {
			p.advance()
			n.Arg = p.parseExpr()
			p.expectKind(token.KindRBrack, "']'")
		}
		n.X = p.parsePropBP(pbpNot)
		return finish(p, start, n)
	case "always", "s_always", "eventually", "s_eventually":
		p.advance()
		n := &ast.PropPrefix{Op: kw}
		if p.peek().Kind == token.KindLBrack {
			p.advance()
			n.Arg = p.parseExpr()
			if p.peek().Kind == token.KindColon {
				p.advance()
				n.Max = p.parseExpr()
				n.Range = true
			}
			p.expectKind(token.KindRBrack, "']'")
		}
		n.X = p.parsePropBP(pbpImpl)
		return finish(p, start, n)
	case "accept_on", "reject_on", "sync_accept_on", "sync_reject_on":
		p.advance()
		n := &ast.PropPrefix{Op: kw, Arg: p.parseParenCond()}
		n.X = p.parsePropBP(pbpImpl)
		return finish(p, start, n)
	case "strong", "weak", "first_match":
		if p.peekAt(1).Kind != token.KindLParen {
			return nil
		}
		p.advance()
		p.advance() // '('
		n := &ast.PropPrefix{Op: kw, X: p.parsePropBP(pbpLowest)}
		if kw == "first_match" {
			for p.peek().Kind == token.KindComma {
				p.advance()
				n.Items = append(n.Items, p.parseMatchItem())
			}
		}
		p.expectKind(token.KindRParen, "')'")
		e := ast.Expr(finish(p, start, n))
		for p.atRepeat() {
			e = p.parseRepeat(e, start)
		}
		return e
	case "if":
		p.advance()
		n := &ast.PropIf{Cond: p.parseParenCond()}
		n.Then = p.parsePropBP(pbpLowest)
		if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "else" {
			p.advance()
			n.Else = p.parsePropBP(pbpLowest)
		}
		return finish(p, start, n)
	case "case":
		return p.parsePropCase(start)
	}
	return nil
}

// parseMatchItem parses one sequence match item (LRM 16.10): an assignment, an
// increment or a call.
func (p *parser) parseMatchItem() ast.Expr {
	start := p.pos
	lhs := p.parseUnary()
	if op := p.peek(); isAssignOp(op.Kind) {
		p.advance()
		return finish(p, start, &ast.AssignExpr{Op: op.Text, LHS: lhs, RHS: p.parseExpr()})
	}
	return lhs
}

// parsePropCase parses "case (e) item {, item}: prop; ... default: prop; endcase".
func (p *parser) parsePropCase(start int) ast.Expr {
	p.advance() // "case"
	n := &ast.PropCase{Expr: p.parseParenCond()}
	for {
		tok := p.peek()
		switch {
		case tok.Kind == token.KindKeyword && tok.Text == "endcase":
			p.advance()
			return finish(p, start, n)
		case tok.Kind == token.KindEOF:
			p.errorf(tok, "unterminated property case, expected endcase")
			return finish(p, start, n)
		case isStmtStop(tok):
			p.errorf(tok, "expected endcase before %q", tok.Text)
			return finish(p, start, n)
		}
		itemStart := p.pos
		item := ast.PropCaseItem{}
		if tok.Kind == token.KindKeyword && tok.Text == "default" {
			item.Default = true
			p.advance()
			if p.peek().Kind == token.KindColon {
				p.advance()
			}
		} else {
			item.Exprs = append(item.Exprs, p.parseExpr())
			for p.peek().Kind == token.KindComma {
				p.advance()
				item.Exprs = append(item.Exprs, p.parseExpr())
			}
			p.expectKind(token.KindColon, "':' after the case item")
		}
		item.Body = p.parsePropBP(pbpLowest)
		if p.peek().Kind == token.KindSemi {
			p.advance()
		}
		item.Position = p.spanFrom(itemStart)
		n.Items = append(n.Items, item)
		if p.pos == itemStart {
			p.advance() // never stand still
		}
	}
}

// parsePropertyDecl parses "property name [(ports)]; locals expr [;]
// endproperty" (LRM 16.12) and parseSequenceDecl its sequence counterpart
// (LRM 16.8); the cursor is on the keyword.
//
// Where the body ends is still found by skipToKeyword, so recovery matches what
// it was when these were skipped; the tokens it consumed are then parsed by a
// bounded sub-parser whose errors are suppressed.
func (p *parser) parsePropertyDecl() (ast.Decl, bool) {
	name, ports, decls, body, end, ok := p.parseAssertionDecl("endproperty")
	if !ok {
		return nil, false
	}
	n := &ast.PropertyDecl{Name: name.Text, Ports: ports, Decls: decls, Body: body}
	n.Position = namePosition(name)
	n.EndLine, n.EndCharacter = end.Line, end.Character
	return n, true
}

func (p *parser) parseSequenceDecl() (ast.Decl, bool) {
	name, ports, decls, body, end, ok := p.parseAssertionDecl("endsequence")
	if !ok {
		return nil, false
	}
	n := &ast.SequenceDecl{Name: name.Text, Ports: ports, Decls: decls, Body: body}
	n.Position = namePosition(name)
	n.EndLine, n.EndCharacter = end.Line, end.Character
	return n, true
}

func (p *parser) parseAssertionDecl(endKeyword string) (name preprocessor.Token, ports []ast.Arg, decls []ast.Decl, body ast.Expr, end preprocessor.Token, ok bool) {
	p.advance() // "property"/"sequence"
	name, ok = p.expectIdent()
	if !ok {
		return
	}
	if p.peek().Kind == token.KindLParen {
		ports = p.parseAssertionPorts(p.parenGroup())
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after the %s header", endKeyword[len("end"):])
	}
	bodyStart := p.pos
	p.skipToKeyword(endKeyword, nil)
	toks := p.toks[bodyStart:p.pos]
	if n := len(toks); n > 0 && toks[n-1].Kind == token.KindKeyword && toks[n-1].Text == endKeyword {
		end = toks[n-1]
		toks = toks[:n-1]
	}
	p.skipEndLabel()

	sub := p.newSubParser(toks)
	for sub.peek().Kind != token.KindEOF && sub.atBlockDeclaration() {
		decls = append(decls, sub.parseBlockDeclaration()...)
	}
	if sub.peek().Kind != token.KindEOF {
		body = sub.parsePropExpr()
		if sub.peek().Kind == token.KindSemi {
			sub.advance()
		}
		if tok := sub.peek(); tok.Kind != token.KindEOF {
			sub.errorf(tok, "unexpected %s after the %s body", describeTok(tok), endKeyword[len("end"):])
		}
	}
	p.suppressed = append(p.suppressed, sub.errs...)
	return name, ports, decls, body, end, true
}

// parseAssertionPorts parses the formal arguments of a property or sequence:
// "[local [direction]] [sequence|property|untyped|type] name [= default]".
func (p *parser) parseAssertionPorts(toks []preprocessor.Token) []ast.Arg {
	var out []ast.Arg
	for _, group := range splitTop(toks, token.KindComma) {
		if len(group) == 0 {
			continue
		}
		var typeName string
		rest := make([]preprocessor.Token, 0, len(group))
		for i, t := range group {
			if t.Kind == token.KindKeyword {
				switch {
				case t.Text == "local" && i == 0:
					continue
				case (t.Text == "sequence" || t.Text == "property" || t.Text == "untyped") && typeName == "":
					typeName = t.Text
					continue
				}
			}
			rest = append(rest, t)
		}
		arg, ok := p.parseArgEntry(rest)
		if !ok {
			continue
		}
		if typeName != "" {
			arg.Type.Name = typeName
		}
		out = append(out, arg)
	}
	return out
}

// parseConcurrentAssert parses "assert|assume|cover|restrict property|sequence
// (spec) [action]" (LRM 16.14) and "expect (spec) [stmt] [else stmt]" (LRM
// 16.17), the cursor on the first keyword.
func (p *parser) parseConcurrentAssert(start int) ast.Stmt {
	n := &ast.ConcurrentAssert{Kind: p.advance().Text}
	if n.Kind != "expect" {
		n.Sequence = p.peek().Text == "sequence"
		p.advance() // "property"/"sequence"
	}
	if p.expectKind(token.KindLParen, "'('") {
		n.Spec = p.parsePropExpr()
		p.expectKind(token.KindRParen, "')'")
	} else {
		n.Spec = p.emptyBad()
	}
	if n.Kind == "restrict" {
		p.expectSemi()
		return finishStmt(p, start, n)
	}
	if tok := p.peek(); !(tok.Kind == token.KindKeyword && tok.Text == "else") {
		n.Then = p.parseStatement()
	}
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "else" && n.Kind != "cover" {
		p.advance()
		n.Else = p.parseStatement()
	}
	return finishStmt(p, start, n)
}
