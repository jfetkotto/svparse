package parser

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/token"
)

// finishItem stamps a constraint item with its span.
func finishItem[I interface {
	ast.ConstraintItem
	SetPos(ast.Position)
}](p *parser, start int, it I) I {
	it.SetPos(p.spanFrom(start))
	return it
}

// parseConstraintItems parses the text between a constraint block's braces
// (LRM 18.5) with a bounded sub-parser. Its errors are kept in p.suppressed
// like every other expression and statement error.
func (p *parser) parseConstraintItems(toks []preprocessor.Token) []ast.ConstraintItem {
	sub := p.newSubParser(toks)
	var items []ast.ConstraintItem
	for sub.peek().Kind != token.KindEOF {
		before := sub.pos
		items = append(items, sub.parseConstraintItem())
		if sub.pos == before {
			sub.advance() // guarantee progress
		}
	}
	p.suppressed = append(p.suppressed, sub.errs...)
	return items
}

// parseConstraintSet parses a constraint_set: "{ items }" or a single item.
func (p *parser) parseConstraintSet() ast.ConstraintItem {
	if p.peek().Kind != token.KindLBrace {
		return p.parseConstraintItem()
	}
	start := p.pos
	if !p.enterStmt() {
		return p.rawConstraint(start)
	}
	defer p.leaveStmt()
	p.advance() // '{'
	n := &ast.ConstraintSet{}
	for {
		tok := p.peek()
		if tok.Kind == token.KindRBrace {
			p.advance()
			break
		}
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unterminated constraint set, expected '}'")
			break
		}
		before := p.pos
		n.Items = append(n.Items, p.parseConstraintItem())
		if p.pos == before {
			p.advance()
		}
	}
	return finishItem(p, start, n)
}

// rawConstraint keeps the rest of the item, up to its ';', as a ConstraintRaw.
func (p *parser) rawConstraint(start int) ast.ConstraintItem {
	for k := p.peek().Kind; k != token.KindSemi && k != token.KindEOF; k = p.peek().Kind {
		p.advance()
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
	}
	if p.pos == start {
		p.advance()
	}
	return finishItem(p, start, &ast.ConstraintRaw{Tokens: p.toks[start:p.pos]})
}

// parseConstraintItem parses one constraint_expression (LRM 18.5).
func (p *parser) parseConstraintItem() ast.ConstraintItem {
	start := p.pos
	if !p.enterStmt() {
		return p.rawConstraint(start)
	}
	defer p.leaveStmt()

	tok := p.peek()
	if tok.Kind == token.KindKeyword {
		switch tok.Text {
		case "soft":
			p.advance()
			n := &ast.ConstraintExpr{Soft: true}
			n.X = p.parseConstraintExpr()
			p.expectSemi()
			return finishItem(p, start, n)
		case "solve":
			return p.parseSolve(start)
		case "disable":
			if p.peekAt(1).Text == "soft" {
				p.advance()
				p.advance()
				n := &ast.ConstraintDisableSoft{X: p.parseExpr()}
				p.expectSemi()
				return finishItem(p, start, n)
			}
		case "unique":
			if p.peekAt(1).Kind == token.KindLBrace {
				return p.parseUniqueConstraint(start)
			}
		case "if":
			p.advance()
			n := &ast.ConstraintIf{Cond: p.parseParenCond()}
			n.Then = p.parseConstraintSet()
			if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "else" {
				p.advance()
				n.Else = p.parseConstraintSet()
			}
			return finishItem(p, start, n)
		case "foreach":
			return p.parseConstraintForeach(start)
		}
	}

	// An expression, possibly the condition of an implication. "->" has the
	// lowest binding power, so the condition is read above it.
	cond := p.parseExprBP(bpCond)
	switch next := p.peek(); {
	case next.Kind == token.KindArrow:
		p.advance()
		n := &ast.ConstraintImplies{Cond: cond}
		n.Body = p.parseConstraintSet()
		return finishItem(p, start, n)
	case next.Kind == token.KindEquiv:
		p.advance()
		cond = finish(p, start, &ast.Binary{Op: next.Text, X: cond, Y: p.parseExpr()})
	}
	n := &ast.ConstraintExpr{X: p.withDist(cond, start)}
	p.expectSemi()
	return finishItem(p, start, n)
}

// parseConstraintExpr parses "expr [dist { ... }]".
func (p *parser) parseConstraintExpr() ast.Expr {
	start := p.pos
	return p.withDist(p.parseExpr(), start)
}

// withDist folds a following "dist { ... }" clause (LRM 18.5.4) into x.
func (p *parser) withDist(x ast.Expr, start int) ast.Expr {
	tok := p.peek()
	if !(tok.Kind == token.KindKeyword && tok.Text == "dist") {
		return x
	}
	p.advance()
	d := &ast.Dist{X: x}
	if !p.expectKind(token.KindLBrace, "'{' after dist") {
		return finish(p, start, d)
	}
	for p.peek().Kind != token.KindRBrace && p.peek().Kind != token.KindEOF {
		itemStart := p.pos
		item := &ast.DistItem{}
		if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "default" {
			p.advance()
			item.Value = finish(p, itemStart, &ast.Ident{Name: "default"})
		} else {
			item.Value = p.parseRangeOrValue()
		}
		switch w := p.peek(); w.Kind {
		case token.KindColonAssign:
			p.advance()
			item.Weight = p.parseExpr()
		case token.KindColonSlash:
			p.advance()
			item.PerRange = true
			item.Weight = p.parseExpr()
		}
		d.Items = append(d.Items, *finish(p, itemStart, item))
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	p.expectKind(token.KindRBrace, "'}' to close the dist")
	return finish(p, start, d)
}

// parseSolve parses "solve a, b before c, d;" (LRM 18.5.10).
func (p *parser) parseSolve(start int) ast.ConstraintItem {
	p.advance() // "solve"
	n := &ast.ConstraintSolve{}
	n.Solve = p.parseExprList()
	if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "before" {
		p.advance()
		n.Before = p.parseExprList()
	} else {
		p.errorf(t, "expected 'before' in a solve constraint, found %s", describeTok(t))
	}
	p.expectSemi()
	return finishItem(p, start, n)
}

// parseUniqueConstraint parses "unique { a, b[1], [lo:hi] };" (LRM 18.5.5).
func (p *parser) parseUniqueConstraint(start int) ast.ConstraintItem {
	p.advance() // "unique"
	p.advance() // '{'
	n := &ast.ConstraintUnique{}
	for p.peek().Kind != token.KindRBrace && p.peek().Kind != token.KindEOF {
		n.Exprs = append(n.Exprs, p.parseRangeOrValue())
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	p.expectKind(token.KindRBrace, "'}' to close the unique list")
	p.expectSemi()
	return finishItem(p, start, n)
}

// parseConstraintForeach parses "foreach (arr[i, j]) set" (LRM 18.5.8).
func (p *parser) parseConstraintForeach(start int) ast.ConstraintItem {
	p.advance() // "foreach"
	n := &ast.ConstraintForeach{}
	if !p.expectKind(token.KindLParen, "'('") {
		n.Array = p.emptyBad()
		n.Body = p.parseConstraintSet()
		return finishItem(p, start, n)
	}
	n.Array = p.parseHierName(p.pos)
	if p.expectKind(token.KindLBrack, "'[' before the loop variables") {
		for {
			if tok := p.peek(); tok.Kind == token.KindIdent {
				n.Vars = append(n.Vars, tok.Text)
				p.advance()
			} else {
				n.Vars = append(n.Vars, "")
			}
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
		p.expectKind(token.KindRBrack, "']' after the loop variables")
	}
	p.expectKind(token.KindRParen, "')'")
	n.Body = p.parseConstraintSet()
	return finishItem(p, start, n)
}
