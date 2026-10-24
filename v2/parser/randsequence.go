package parser

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/token"
)

// parseRandSequence parses a randsequence statement (LRM 18.17, grammar in
// Annex A.6.12) with the cursor on "randsequence". A sequence it cannot read
// stays a *ast.RawStmt, as it was before this existed: nothing is lost and no
// error is reported, since a rule it does not know is a gap here, not a
// mistake in the source.
func (p *parser) parseRandSequence(start int) ast.Stmt {
	errs := len(p.errs)
	n := p.readRandSequence(start)
	if n == nil || len(p.errs) > errs {
		p.errs = p.errs[:errs]
		return p.rawStatement(start)
	}
	return n
}

func (p *parser) readRandSequence(start int) *ast.RandSequence {
	p.advance() // "randsequence"
	n := &ast.RandSequence{}
	if !p.expectKind(token.KindLParen, "'(' after randsequence") {
		return nil
	}
	if p.peek().Kind == token.KindIdent {
		n.Start = p.advance().Text
	}
	if !p.expectKind(token.KindRParen, "')' after the randsequence start") {
		return nil
	}
	for {
		tok := p.peek()
		switch {
		case tok.Kind == token.KindKeyword && tok.Text == "endsequence":
			p.advance()
			if len(n.Productions) == 0 {
				return nil
			}
			return finishStmt(p, start, n)
		case tok.Kind == token.KindEOF:
			return nil
		}
		prod := p.readRandProduction()
		if prod == nil {
			return nil
		}
		n.Productions = append(n.Productions, *prod)
	}
}

// readRandProduction reads "[type] name [(args)] : rule { | rule } ;".
func (p *parser) readRandProduction() *ast.RandProduction {
	start := p.pos
	prod := &ast.RandProduction{}
	if next := p.peekAt(1).Kind; p.peek().Kind != token.KindIdent || (next != token.KindColon && next != token.KindLParen) {
		prod.ReturnType = p.parseDataType()
	}
	name, ok := p.expectIdent()
	if !ok {
		return nil
	}
	prod.Name = name.Text
	if p.peek().Kind == token.KindLParen {
		prod.Args = p.parseArgList()
	}
	if !p.expectKind(token.KindColon, "':' after the production name") {
		return nil
	}
	for {
		rule := p.readRandRule()
		if rule == nil {
			return nil
		}
		prod.Rules = append(prod.Rules, *rule)
		switch p.peek().Kind {
		case token.KindPipe:
			p.advance()
		case token.KindSemi:
			p.advance()
			return finishStmt(p, start, prod)
		default:
			return nil
		}
	}
}

// readRandRule reads one alternative: its elements, then an optional
// ":= weight [code]".
func (p *parser) readRandRule() *ast.RandRule {
	start := p.pos
	rule := &ast.RandRule{}
	for {
		tok := p.peek()
		var elem ast.Stmt
		switch {
		case tok.Kind == token.KindPipe || tok.Kind == token.KindSemi || tok.Kind == token.KindEOF:
			if len(rule.Elems) == 0 {
				return nil
			}
			return finishStmt(p, start, rule)
		case tok.Kind == token.KindColonAssign:
			return p.readRandWeight(start, rule)
		case tok.Kind == token.KindLBrace:
			if code := p.readRandCode(); code != nil {
				elem = code
			}
		case tok.Kind == token.KindIdent:
			if item := p.readRandItem(); item != nil {
				elem = item
			}
		case tok.Kind == token.KindKeyword && tok.Text == "if":
			elem = p.readRandIf()
		case tok.Kind == token.KindKeyword && tok.Text == "repeat":
			elem = p.readRandRepeat()
		case tok.Kind == token.KindKeyword && tok.Text == "case":
			elem = p.readRandCase()
		case tok.Kind == token.KindKeyword && tok.Text == "rand":
			elem = p.readRandJoin()
		default:
			return nil
		}
		if isNilStmt(elem) {
			return nil
		}
		rule.Elems = append(rule.Elems, elem)
	}
}

// isNilStmt reports whether s is nil or holds a nil pointer.
func isNilStmt(s ast.Stmt) bool {
	switch n := s.(type) {
	case nil:
		return true
	case *ast.RandItem:
		return n == nil
	case *ast.RandCode:
		return n == nil
	case *ast.RandIf:
		return n == nil
	case *ast.RandRepeat:
		return n == nil
	case *ast.RandSeqCase:
		return n == nil
	case *ast.RandJoin:
		return n == nil
	}
	return false
}

// readRandWeight reads ":= weight [code block]" and ends the rule.
func (p *parser) readRandWeight(start int, rule *ast.RandRule) *ast.RandRule {
	if len(rule.Elems) == 0 {
		return nil
	}
	p.advance() // ":="
	rule.Weight = p.parseUnary()
	if p.peek().Kind == token.KindLBrace {
		if rule.WeightCode = p.readRandCode(); rule.WeightCode == nil {
			return nil
		}
	}
	return finishStmt(p, start, rule)
}

// readRandItem reads "name" or "name(args)".
func (p *parser) readRandItem() *ast.RandItem {
	start := p.pos
	name, ok := p.expectIdent()
	if !ok {
		return nil
	}
	item := &ast.RandItem{Name: name.Text}
	if p.peek().Kind == token.KindLParen {
		p.advance()
		item.Args = p.parseCallArgs(false)
	}
	return finishStmt(p, start, item)
}

// readRandCode reads "{ declarations statements }" by cutting out the tokens
// between the braces and reading them as a subroutine body.
func (p *parser) readRandCode() *ast.RandCode {
	start := p.pos
	depth := 0
	end := -1
scan:
	for i := p.pos; i < len(p.toks); i++ {
		switch p.toks[i].Kind {
		case token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRBrace:
			depth--
			if depth == 0 {
				end = i
				break scan
			}
		case token.KindEOF:
			break scan
		}
	}
	if end < 0 {
		return nil
	}
	code := &ast.RandCode{}
	code.Decls, code.Stmts, _, _ = p.parseSubroutineBody(p.toks[p.pos+1 : end])
	p.pos = end + 1
	return finishStmt(p, start, code)
}

func (p *parser) readParenExpr() (ast.Expr, bool) {
	if !p.expectKind(token.KindLParen, "'('") {
		return nil, false
	}
	e := p.parseExpr()
	return e, p.expectKind(token.KindRParen, "')'")
}

func (p *parser) readRandIf() *ast.RandIf {
	start := p.pos
	p.advance() // "if"
	n := &ast.RandIf{}
	var ok bool
	if n.Cond, ok = p.readParenExpr(); !ok {
		return nil
	}
	if n.Then = p.readRandItem(); n.Then == nil {
		return nil
	}
	if p.peek().Kind == token.KindKeyword && p.peek().Text == "else" {
		p.advance()
		if n.Else = p.readRandItem(); n.Else == nil {
			return nil
		}
	}
	return finishStmt(p, start, n)
}

func (p *parser) readRandRepeat() *ast.RandRepeat {
	start := p.pos
	p.advance() // "repeat"
	n := &ast.RandRepeat{}
	var ok bool
	if n.Count, ok = p.readParenExpr(); !ok {
		return nil
	}
	if n.Item = p.readRandItem(); n.Item == nil {
		return nil
	}
	return finishStmt(p, start, n)
}

func (p *parser) readRandCase() *ast.RandSeqCase {
	start := p.pos
	p.advance() // "case"
	n := &ast.RandSeqCase{}
	var ok bool
	if n.X, ok = p.readParenExpr(); !ok {
		return nil
	}
	for {
		tok := p.peek()
		if tok.Kind == token.KindKeyword && tok.Text == "endcase" {
			p.advance()
			if len(n.Items) == 0 {
				return nil
			}
			return finishStmt(p, start, n)
		}
		itemStart := p.pos
		item := &ast.RandSeqCaseItem{}
		if tok.Kind == token.KindKeyword && tok.Text == "default" {
			p.advance()
			item.Default = true
			if p.peek().Kind == token.KindColon {
				p.advance()
			}
		} else {
			for {
				item.Exprs = append(item.Exprs, p.parseExpr())
				if p.peek().Kind != token.KindComma {
					break
				}
				p.advance()
			}
			if !p.expectKind(token.KindColon, "':' in the case item") {
				return nil
			}
		}
		if item.Item = p.readRandItem(); item.Item == nil || !p.expectKind(token.KindSemi, "';' after the case item") {
			return nil
		}
		n.Items = append(n.Items, *finishStmt(p, itemStart, item))
	}
}

func (p *parser) readRandJoin() *ast.RandJoin {
	start := p.pos
	p.advance() // "rand"
	if t := p.peek(); t.Kind != token.KindKeyword || t.Text != "join" {
		return nil
	}
	p.advance()
	n := &ast.RandJoin{}
	if p.peek().Kind == token.KindLParen {
		var ok bool
		if n.Count, ok = p.readParenExpr(); !ok {
			return nil
		}
	}
	for p.peek().Kind == token.KindIdent {
		item := p.readRandItem()
		if item == nil {
			return nil
		}
		n.Items = append(n.Items, *item)
	}
	if len(n.Items) < 2 {
		return nil
	}
	return finishStmt(p, start, n)
}
