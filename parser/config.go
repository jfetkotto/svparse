package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/token"
)

// parseConfig parses a configuration declaration (LRM 33.4), the cursor on
// "config". Design cells and rules are read as text; resolving them belongs to
// elaboration.
func (p *parser) parseConfig() (ast.Decl, bool) {
	p.advance() // "config"
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	n := &ast.Config{Name: nameTok.Text}
	n.Position = namePosition(nameTok)
	p.skipHeaderToSemiStrict()

	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected endconfig")
			return n, true
		}
		if tok.Kind == token.KindKeyword {
			switch tok.Text {
			case "endconfig":
				n.EndLine, n.EndCharacter = tok.Line, tok.Character
				p.advance()
				p.skipEndLabel()
				return n, true
			case "design":
				p.advance()
				prevDot := false
				for _, t := range p.headerTokens() {
					switch {
					case t.Kind == token.KindDot:
						prevDot = true
						continue
					case prevDot && len(n.Design) > 0:
						n.Design[len(n.Design)-1] += "." + t.Text
					default:
						n.Design = append(n.Design, t.Text)
					}
					prevDot = false
				}
				continue
			case "default", "instance", "cell":
				start := p.pos
				p.advance()
				rule := ast.ConfigRule{Kind: tok.Text}
				toks := p.headerTokens()
				rule.Text = textOf(toks)
				rule.Position = p.spanFrom(start)
				n.Rules = append(n.Rules, rule)
				continue
			case "localparam", "parameter":
				if decls, ok := p.parseDecl(); ok {
					n.Body = append(n.Body, decls...)
				}
				continue
			}
			if isEndKeyword(tok.Text) || udpBoundaryKeywords[tok.Text] {
				p.errorf(tok, "expected endconfig, found %s", tok.Text)
				return n, true
			}
		}
		before := p.pos
		p.errorf(tok, "unexpected token %q in a configuration", tok.Text)
		p.skipHeaderToSemiStrict()
		if p.pos == before {
			p.advance()
		}
	}
}
