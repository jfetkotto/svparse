package parser

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/token"
)

// maxGenDepth bounds generate nesting so hostile input cannot exhaust the
// stack. Beyond it a generate keyword is reported and skipped one token at a
// time, the way any unrecognised token is.
const maxGenDepth = 256

// parseGenerateRegion parses "generate ... endgenerate" (LRM 27.3), the cursor
// on "generate".
func (p *parser) parseGenerateRegion() ([]ast.Decl, bool) {
	start := p.pos
	if !p.enterGen() {
		return nil, false
	}
	defer p.leaveGen()
	p.advance()
	n := &ast.GenerateRegion{}
	n.Body, _, _ = p.parseBody("endgenerate")
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

func (p *parser) enterGen() bool {
	if p.genDepth >= maxGenDepth {
		p.errorf(p.peek(), "generate constructs nested more than %d deep", maxGenDepth)
		return false
	}
	p.genDepth++
	return true
}

func (p *parser) leaveGen() { p.genDepth-- }

// parseGenerateBlock parses "begin [: label] items end [: label]" (LRM 27.3),
// the cursor on "begin".
func (p *parser) parseGenerateBlock() *ast.GenerateBlock {
	start := p.pos
	n := &ast.GenerateBlock{}
	if !p.enterGen() {
		p.advance() // never stand still
		n.Position = p.spanFrom(start)
		return n
	}
	defer p.leaveGen()
	p.advance() // "begin"
	if p.peek().Kind == token.KindColon && p.peekAt(1).Kind == token.KindIdent {
		p.advance()
		n.Label = p.advance().Text
	}
	var matched bool
	n.Body, _, matched = p.parseBody("end")
	// parseBody consumed an "end : name" label without keeping it.
	if matched && n.Label == "" && p.pos >= 3 &&
		p.toks[p.pos-3].Text == "end" && p.toks[p.pos-2].Kind == token.KindColon && p.toks[p.pos-1].Kind == token.KindIdent {
		n.Label = p.toks[p.pos-1].Text
	}
	n.Position = p.spanFrom(start)
	return n
}

// parseGenerateBranch parses what follows a generate header: a begin/end
// block, or the single bare item LRM 27.5 allows.
func (p *parser) parseGenerateBranch() *ast.GenerateBlock {
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "begin" {
		return p.parseGenerateBlock()
	}
	start := p.pos
	n := &ast.GenerateBlock{Bare: true}
	if !p.enterGen() {
		n.Position = p.spanFrom(start)
		return n
	}
	defer p.leaveGen()
	body, ok := p.parseDecl()
	if !ok {
		p.errorf(p.peek(), "expected a generate item, found %s", describeTok(p.peek()))
	}
	n.Body = body
	n.Position = p.spanFrom(start)
	return n
}

// parenGroup consumes "( ... )" and returns the tokens between the
// parentheses. A missing parenthesis is reported and yields what was seen.
func (p *parser) parenGroup() []preprocessor.Token {
	if !p.expectKind(token.KindLParen, "'('") {
		return nil
	}
	open := p.pos
	p.collectUntil(token.KindRParen)
	end := p.pos
	p.expectKind(token.KindRParen, "')'")
	return p.toks[open:end]
}

// parseGenerateIf parses "if (cond) item [else item]" (LRM 27.5), the cursor
// on "if". The condition's errors are suppressed like any expression's.
func (p *parser) parseGenerateIf() ([]ast.Decl, bool) {
	start := p.pos
	if !p.enterGen() {
		return nil, false
	}
	defer p.leaveGen()
	p.advance() // "if"
	n := &ast.GenerateIf{}
	n.Cond = parseSpan(p.parenGroup(), false)
	n.Then = p.parseGenerateBranch()
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "else" {
		p.advance()
		if nxt := p.peek(); nxt.Kind == token.KindKeyword && nxt.Text == "if" {
			if d, ok := p.parseGenerateIf(); ok {
				n.Else = d[0]
			}
		} else {
			n.Else = p.parseGenerateBranch()
		}
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// parseGenerateFor parses "for (genvar i = a; cond; step) item" (LRM 27.4),
// the cursor on "for". The header is parsed by a bounded sub-parser whose
// errors are suppressed.
func (p *parser) parseGenerateFor() ([]ast.Decl, bool) {
	start := p.pos
	if !p.enterGen() {
		return nil, false
	}
	defer p.leaveGen()
	p.advance() // "for"
	n := &ast.GenerateFor{}
	hdr := p.newSubParser(p.parenGroup())
	if tok := hdr.peek(); tok.Kind == token.KindKeyword && tok.Text == "genvar" {
		n.Genvar = true
		hdr.advance()
	} else if tok.Kind == token.KindKeyword && dataTypeKeywords[tok.Text] {
		hdr.advance() // "int i = 0" is accepted without a genvar
	}
	if tok := hdr.peek(); tok.Kind == token.KindIdent {
		n.Var = tok.Text
		hdr.advance()
	} else {
		hdr.errorf(tok, "expected the loop variable")
	}
	if hdr.expectKind(token.KindAssign, "'='") {
		n.Init = hdr.parseExpr()
	}
	hdr.expectKind(token.KindSemi, "';'")
	n.Cond = hdr.parseExpr()
	hdr.expectKind(token.KindSemi, "';'")
	if steps := hdr.parseAssignList(token.KindEOF); len(steps) > 0 {
		n.Step = steps[0]
	}
	p.suppressed = append(p.suppressed, hdr.errs...)
	n.Body = p.parseGenerateBranch()
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// parseGenerateCase parses "case (expr) item: ... default: ... endcase"
// (LRM 27.5), the cursor on "case".
func (p *parser) parseGenerateCase() ([]ast.Decl, bool) {
	start := p.pos
	if !p.enterGen() {
		return nil, false
	}
	defer p.leaveGen()
	p.advance() // "case"
	n := &ast.GenerateCase{}
	n.Expr = parseSpan(p.parenGroup(), false)
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected endcase")
			break
		}
		if tok.Kind == token.KindKeyword && tok.Text == "endcase" {
			p.advance()
			break
		}
		if tok.Kind == token.KindKeyword && isEndKeyword(tok.Text) {
			p.errorf(tok, "expected endcase, found %s", tok.Text)
			break
		}
		itemStart := p.pos
		item := ast.GenerateCaseItem{}
		if tok.Kind == token.KindKeyword && tok.Text == "default" {
			item.Default = true
			p.advance()
			if p.peek().Kind == token.KindColon {
				p.advance()
			}
		} else if toks, ok := p.caseItemValues(); ok {
			item.Exprs = splitExprs(toks)
			p.advance() // ':'
		} else {
			p.errorf(tok, "expected a case item, found %s", describeTok(tok))
			p.advance() // never stand still
			continue
		}
		item.Body = p.parseGenerateBranch()
		item.Position = p.spanFrom(itemStart)
		n.Items = append(n.Items, item)
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// caseItemValues collects the match values up to the ':' that ends them,
// leaving the cursor on that colon. A '?' in a value claims the next ':'.
func (p *parser) caseItemValues() ([]preprocessor.Token, bool) {
	depth, quest := 0, 0
	for i := p.pos; i < len(p.toks); i++ {
		switch tok := p.toks[i]; tok.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindQuest:
			quest++
		case token.KindColon:
			if depth == 0 {
				if quest > 0 {
					quest--
					continue
				}
				if i == p.pos {
					return nil, false
				}
				toks := p.toks[p.pos:i]
				p.pos = i
				return toks, true
			}
		case token.KindSemi:
			if depth == 0 {
				return nil, false
			}
		case token.KindKeyword:
			if depth == 0 && (isEndKeyword(tok.Text) || tok.Text == "endcase") {
				return nil, false
			}
		}
	}
	return nil, false
}

// splitExprs parses a comma-separated list of expression spans.
func splitExprs(toks []preprocessor.Token) []ast.Expr {
	var out []ast.Expr
	depth, from := 0, 0
	flush := func(to int) {
		if e := parseSpan(toks[from:to], false); e != nil {
			out = append(out, e)
		}
	}
	for i, tok := range toks {
		switch tok.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindComma:
			if depth == 0 {
				flush(i)
				from = i + 1
			}
		}
	}
	flush(len(toks))
	return out
}
