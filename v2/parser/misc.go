package parser

import (
	"strings"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// headerTokens consumes the rest of a one-statement item and returns its
// tokens without the terminating ';'. Like skipHeaderToSemiStrict, which it
// uses, it reports a missing ';' and stops before the next declaration rather
// than eating it.
func (p *parser) headerTokens() []preprocessor.Token {
	return p.headerTokensVia(p.skipHeaderToSemiStrict)
}

// headerTokensVia is headerTokens with the skip chosen by the caller, for
// items whose header legitimately contains a declaration keyword (a DPI
// export names "function"), which the strict skip would take for the next
// declaration.
func (p *parser) headerTokensVia(skip func()) []preprocessor.Token {
	start := p.pos
	skip()
	toks := p.toks[start:p.pos]
	if n := len(toks); n > 0 && toks[n-1].Kind == token.KindSemi {
		toks = toks[:n-1]
	}
	return toks
}

// textOf joins tokens back into source-like text, keeping a single space
// wherever the source had any gap between two tokens.
func textOf(toks []preprocessor.Token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			endLine, endChar := toks[i-1].End()
			if t.Line != endLine || t.Character > endChar {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

// splitTop splits toks at each depth-zero token of the given kind.
func splitTop(toks []preprocessor.Token, sep token.Kind) [][]preprocessor.Token {
	var out [][]preprocessor.Token
	depth, from := 0, 0
	for i, tok := range toks {
		switch tok.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, toks[from:i])
				from = i + 1
			}
		}
	}
	return append(out, toks[from:])
}

// parseTimeUnit parses "timeunit v [/ v];" / "timeprecision v;" (LRM 3.14.2.3),
// the cursor on the keyword.
func (p *parser) parseTimeUnit() ([]ast.Decl, bool) {
	start := p.pos
	n := &ast.TimeUnit{Keyword: p.advance().Text}
	parts := splitTop(p.headerTokens(), token.KindQuo)
	n.Value = textOf(parts[0])
	if len(parts) > 1 {
		n.Divisor = textOf(parts[1])
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// parseDefparam parses "defparam a.b = v, ...;" (LRM 23.10.1). Errors in the
// assignments are suppressed.
func (p *parser) parseDefparam() ([]ast.Decl, bool) {
	start := p.pos
	p.advance()
	n := &ast.Defparam{}
	sub := p.newSubParser(p.headerTokens())
	n.Assigns = sub.parseAssignItems()
	p.suppressed = append(p.suppressed, sub.errs...)
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// parseLet parses "let name [(ports)] = expr;" (LRM 11.13).
func (p *parser) parseLet() ([]ast.Decl, bool) {
	p.advance()
	n := &ast.Let{}
	sub := p.newSubParser(p.headerTokens())
	nameTok := sub.peek()
	if nameTok.Kind != token.KindIdent {
		return nil, true
	}
	sub.advance()
	n.Name = nameTok.Text
	n.Ports = sub.parsePortList()
	if sub.expectKind(token.KindAssign, "'=' in a let declaration") {
		n.Body = parseSpan(sub.toks[sub.pos:], false)
	}
	p.suppressed = append(p.suppressed, sub.errs...)
	n.Position = namePosition(nameTok)
	return []ast.Decl{n}, true
}

// parseAlias parses "alias a = b = c;" (LRM 10.12).
func (p *parser) parseAlias() ([]ast.Decl, bool) {
	start := p.pos
	p.advance()
	n := &ast.Alias{}
	for _, part := range splitTop(p.headerTokens(), token.KindAssign) {
		if e := parseSpan(part, false); e != nil {
			n.Nets = append(n.Nets, e)
		}
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// parseExport parses a package export (LRM 26.6) or a DPI export (LRM 35.7),
// the cursor on "export".
func (p *parser) parseExport() ([]ast.Decl, bool) {
	start := p.pos
	p.advance()
	if p.peek().Kind == token.KindStringLiteral {
		return p.parseDPIExport(start)
	}
	var out []ast.Decl
	for _, part := range splitTop(p.headerTokens(), token.KindComma) {
		e := &ast.Export{}
		if len(part) >= 3 && part[1].Kind == token.KindColonColon {
			e.Package, e.Member = part[0].Text, part[2].Text
			e.Position = namePosition(part[0])
			out = append(out, e)
		}
	}
	return out, true
}

// parseDPIExport parses '"DPI-C" [c_name =] function|task name;', the cursor
// on the spec string.
func (p *parser) parseDPIExport(start int) ([]ast.Decl, bool) {
	n := &ast.DPIExport{Spec: strings.Trim(p.advance().Text, `"`)}
	toks := p.headerTokensVia(p.skipHeaderToSemi)
	if len(toks) >= 2 && toks[1].Kind == token.KindAssign {
		n.CName = toks[0].Text
		toks = toks[2:]
	}
	if len(toks) >= 2 {
		n.Kind, n.Name = toks[0].Text, toks[1].Text
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// parseDefaultItem parses the "default" module items: "default clocking name;",
// the clocking declaration "default clocking [name] @(e);" and "default
// disable iff expr;" (LRM 14.12, 16.15). Anything else returns false.
func (p *parser) parseDefaultItem() ([]ast.Decl, bool) {
	start := p.pos
	switch p.peekAt(1).Text {
	case "disable":
		p.advance()
		p.advance()
		toks := p.headerTokens()
		if len(toks) > 0 && toks[0].Text == "iff" {
			toks = toks[1:]
		}
		n := &ast.DefaultDisable{Cond: parseSpan(toks, false)}
		n.Position = p.spanFrom(start)
		return []ast.Decl{n}, true
	case "clocking":
		if p.peekAt(2).Kind == token.KindIdent && p.peekAt(3).Kind == token.KindSemi {
			p.advance()
			p.advance()
			n := &ast.DefaultClocking{Name: p.advance().Text}
			p.advance() // ';'
			n.Position = p.spanFrom(start)
			return []ast.Decl{n}, true
		}
		p.advance() // "default"
		return oneDecl(p.parseClocking(start, true, false))
	}
	return nil, false
}

// parseBind parses "bind target [: inst, ...] instantiation;" (LRM 23.11), the
// cursor on "bind".
func (p *parser) parseBind() ([]ast.Decl, bool) {
	start := p.pos
	p.advance()
	n := &ast.Bind{}
	var ok bool
	if n.Target, ok = p.hierNameText(); !ok {
		p.skipHeaderToSemiStrict()
		n.Position = p.spanFrom(start)
		return []ast.Decl{n}, true
	}
	if p.peek().Kind == token.KindColon {
		p.advance()
		for {
			name, ok := p.hierNameText()
			if !ok {
				break
			}
			n.Instances = append(n.Instances, name)
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	}
	if p.peek().Kind == token.KindIdent {
		decls, _ := p.parseVariableOrInstantiation()
		for _, d := range decls {
			if inst, ok := d.(*ast.Instantiation); ok {
				n.Inst = inst
			}
		}
	} else {
		p.skipHeaderToSemiStrict()
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// hierNameText consumes "a.b[3].c" and returns its text.
func (p *parser) hierNameText() (string, bool) {
	start := p.pos
	if p.peek().Kind != token.KindIdent {
		return "", false
	}
	p.advance()
	for {
		switch p.peek().Kind {
		case token.KindDot:
			if p.peekAt(1).Kind != token.KindIdent {
				return textOf(p.toks[start:p.pos]), true
			}
			p.advance()
			p.advance()
		case token.KindLBrack:
			p.advance()
			p.collectUntil(token.KindRBrack)
			if p.peek().Kind == token.KindRBrack {
				p.advance()
			}
		default:
			return textOf(p.toks[start:p.pos]), true
		}
	}
}
