package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/token"
)

// parseClocking parses a clocking block (LRM 14.3), the cursor on "clocking".
// start is the index of the first token of the construct, which is a
// preceding "default" or "global" when isDefault or isGlobal is set (the
// caller has consumed it).
//
// Items are read one ';'-terminated header at a time. Assertion declarations
// nested in the block (property, sequence, let) are skipped, since the
// verification constructs are not modeled yet.
func (p *parser) parseClocking(start int, isDefault, isGlobal bool) (ast.Decl, bool) {
	kw := p.advance() // "clocking"
	n := &ast.Clocking{Default: isDefault, Global: isGlobal}
	n.Position = namePosition(kw)
	if tok := p.peek(); tok.Kind == token.KindIdent {
		p.advance()
		n.Name = tok.Text
		n.Position = namePosition(tok)
	}
	sub := p.newSubParser(p.headerTokens())
	if sub.peek().Kind == token.KindAt {
		n.Event = sub.parseTimingControl()
	}
	p.suppressed = append(p.suppressed, sub.errs...)

	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected endclocking")
			return n, true
		}
		if tok.Kind == token.KindKeyword {
			switch tok.Text {
			case "endclocking":
				n.EndLine, n.EndCharacter = tok.Line, tok.Character
				p.advance()
				p.skipEndLabel()
				return n, true
			case "property":
				p.advance()
				p.skipToKeyword("endproperty", nil)
				p.skipEndLabel()
				continue
			case "sequence":
				p.advance()
				p.skipToKeyword("endsequence", nil)
				p.skipEndLabel()
				continue
			case "let":
				p.advance()
				p.skipHeaderToSemiStrict()
				continue
			}
			if isEndKeyword(tok.Text) {
				p.errorf(tok, "expected endclocking, found %s", tok.Text)
				return n, true
			}
		}
		itemStart := p.pos
		if item, ok := p.parseClockingItem(); ok {
			n.Items = append(n.Items, item)
		}
		if p.pos == itemStart {
			p.advance() // never stand still
		}
	}
}

// parseClockingItem parses "default input [skew] output [skew];" or
// "direction [skew] name [= expr] {, name [= expr]};" (LRM 14.3).
func (p *parser) parseClockingItem() (ast.ClockingItem, bool) {
	var item ast.ClockingItem
	first := p.peek()
	toks := p.headerTokens()
	if len(toks) == 0 {
		return item, false
	}
	item.Position = tokensSpan(toks)
	sub := p.newSubParser(toks)
	if t := sub.peek(); t.Kind == token.KindKeyword && t.Text == "default" {
		item.Default = true
		sub.advance()
	}
	dirs := 0
	for {
		t := sub.peek()
		if t.Kind != token.KindKeyword || !(t.Text == "input" || t.Text == "output" || t.Text == "inout") || dirs == 2 {
			break
		}
		sub.advance()
		dirs++
		if item.Direction != "" {
			item.Direction += " "
		}
		item.Direction += t.Text
		skew := sub.clockingSkew()
		switch t.Text {
		case "output":
			item.OutputSkew = skew
		default:
			item.InputSkew = skew
		}
	}
	if item.Direction == "" && !item.Default {
		p.suppressed = append(p.suppressed, Error{File: first.File, Line: first.Line, Character: first.Character, Message: "expected a clocking direction"})
		return item, true
	}
	if item.Default {
		item.Direction = ""
		return item, true
	}
	for _, part := range splitTop(sub.toks[sub.pos:], token.KindComma) {
		if len(part) == 0 || part[0].Kind != token.KindIdent {
			continue
		}
		sig := ast.ClockingSignal{Name: part[0].Text, Position: namePosition(part[0])}
		if len(part) > 2 && part[1].Kind == token.KindAssign {
			sig.Expr = parseSpan(part[2:], false)
		}
		item.Signals = append(item.Signals, sig)
	}
	p.suppressed = append(p.suppressed, sub.errs...)
	return item, true
}

// clockingSkew consumes an optional clocking skew, "#1", "#1step",
// "negedge", "posedge #2" (LRM 14.4), and returns its text.
func (p *parser) clockingSkew() string {
	start := p.pos
	if t := p.peek(); t.Kind == token.KindKeyword && (t.Text == "posedge" || t.Text == "negedge" || t.Text == "edge") {
		p.advance()
	}
	if p.peek().Kind == token.KindHash {
		p.advance()
		if p.peek().Kind == token.KindLParen {
			p.advance()
			p.collectUntil(token.KindRParen)
			if p.peek().Kind == token.KindRParen {
				p.advance()
			}
		} else {
			p.advance()
			if t := p.peek(); t.Kind == token.KindIdent && t.Text == "step" {
				p.advance() // "1step": the lexer splits the number from its unit
			}
		}
	}
	return textOf(p.toks[start:p.pos])
}
