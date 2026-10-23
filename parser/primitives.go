package parser

import (
	"strings"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parseGateInstantiation parses a built-in gate or switch instantiation (LRM
// 28.3), the cursor on the gate keyword:
//
//	gate [(strength)] [#delay] [name] [range] (terminals) {, ...} ;
//
// LRM 28.3 writes the strength before the delay; the two are accepted in
// either order. The strength is told apart from an unnamed instance's
// terminal list by its first token, which must be a strength keyword.
func (p *parser) parseGateInstantiation() ([]ast.Decl, bool) {
	start := p.pos
	n := &ast.GateInstantiation{Gate: p.advance().Text}
	for i := 0; i < 2; i++ {
		switch tok := p.peek(); {
		case tok.Kind == token.KindLParen && n.DriveStrength == nil && p.atStrength():
			n.DriveStrength = p.parseStrength()
		case tok.Kind == token.KindHash && n.Delay == nil:
			n.Delay = p.parseDelay3()
		}
	}
	for {
		inst, ok := p.parseGateInstance()
		if !ok {
			p.skipHeaderToSemiStrict()
			n.Position = p.spanFrom(start)
			return []ast.Decl{n}, true
		}
		n.Instances = append(n.Instances, inst)
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.skipHeaderToSemiStrict() // reports the missing ';' and stops at the next declaration
	}
	n.Position = p.spanFrom(start)
	return []ast.Decl{n}, true
}

// atStrength reports whether the "(" under the cursor opens a drive or charge
// strength rather than a terminal list.
func (p *parser) atStrength() bool {
	next := p.peekAt(1)
	return next.Kind == token.KindKeyword && strengthKeywords[next.Text]
}

// parseGateInstance parses "[name [range]] ( terminals )".
func (p *parser) parseGateInstance() (ast.GateInstance, bool) {
	var inst ast.GateInstance
	tok := p.peek()
	if tok.Kind == token.KindIdent {
		p.advance()
		inst.Name = tok.Text
		inst.UnpackedDims = p.parseDims()
	}
	open := p.peek()
	inst.Position = namePosition(tok)
	if inst.Name == "" {
		inst.Position = namePosition(open)
	}
	if open.Kind != token.KindLParen {
		p.errorf(open, "expected '(' to start the gate terminals, found %s", describeTok(open))
		return inst, false
	}
	inst.Terminals = splitExprs(p.parenGroup())
	return inst, true
}

// udpBoundaryKeywords start a new top-level unit, so an unterminated UDP ends
// before them rather than swallowing the unit.
var udpBoundaryKeywords = map[string]bool{
	"module": true, "macromodule": true, "interface": true, "program": true,
	"package": true, "class": true, "primitive": true,
}

// parseUDP parses a user-defined primitive (LRM 29.3), the cursor on
// "primitive":
//
//	primitive name (ports); {port and reg declarations} [initial ...] table ... endtable endprimitive
func (p *parser) parseUDP() (ast.Decl, bool) {
	p.advance() // "primitive"
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	u := &ast.UDP{Name: nameTok.Text}
	u.Position = namePosition(nameTok)
	u.Ports = p.parsePortList()
	p.skipHeaderToSemiStrict()

	var body []ast.Decl
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected endprimitive")
			break
		}
		if tok.Kind == token.KindKeyword {
			if tok.Text == "endprimitive" {
				u.EndLine, u.EndCharacter = tok.Line, tok.Character
				p.advance()
				p.skipEndLabel()
				break
			}
			if tok.Text == "table" {
				p.parseUDPTable(u)
				continue
			}
			if isEndKeyword(tok.Text) || udpBoundaryKeywords[tok.Text] {
				p.errorf(tok, "expected endprimitive, found %s", tok.Text)
				break
			}
		}
		before := p.pos
		decls, ok := p.parseDecl()
		if ok {
			body = append(body, decls...)
			continue
		}
		p.errorf(tok, "unexpected token %q, skipping to the next declaration", tok.Text)
		if p.pos == before {
			p.recover()
		}
	}
	u.Body = p.mergeNonANSIPortDecls(u.Ports, body)
	return u, true
}

// parseUDPTable parses "table row; ... endtable" into u.Rows, the cursor on
// "table". A row that is not "inputs : [current :] next" is reported and
// dropped.
func (p *parser) parseUDPTable(u *ast.UDP) {
	p.advance() // "table"
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected endtable")
			return
		}
		if tok.Kind == token.KindKeyword {
			if tok.Text == "endtable" {
				p.advance()
				return
			}
			if isEndKeyword(tok.Text) {
				p.errorf(tok, "expected endtable, found %s", tok.Text)
				return
			}
		}
		start := p.pos
		for k := p.peek().Kind; k != token.KindSemi && k != token.KindEOF; k = p.peek().Kind {
			if t := p.peek(); t.Kind == token.KindKeyword && (t.Text == "endtable" || isEndKeyword(t.Text)) {
				break
			}
			p.advance()
		}
		row, ok := p.udpRow(p.toks[start:p.pos])
		if ok {
			u.Rows = append(u.Rows, row)
			if row.Current != "" {
				u.Sequential = true
			}
		} else {
			p.errorf(tok, "expected a table row \"inputs : [current :] next\"")
		}
		if p.peek().Kind == token.KindSemi {
			p.advance()
		}
		if p.pos == start {
			p.advance() // never stand still
		}
	}
}

// udpRow reads one table row from its tokens (without the ';'). Symbols are
// single characters or a parenthesised edge, and the lexer may have joined
// adjacent ones ("01", "?0"), so the fields are read from the token text.
func (p *parser) udpRow(toks []preprocessor.Token) (ast.UDPRow, bool) {
	var row ast.UDPRow
	if len(toks) == 0 {
		return row, false
	}
	row.Position = tokensSpan(toks)
	var fields []string
	var cur strings.Builder
	for _, t := range toks {
		if t.Kind == token.KindColon {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteString(t.Text)
	}
	fields = append(fields, cur.String())
	if len(fields) < 2 || len(fields) > 3 || fields[0] == "" {
		return row, false
	}
	row.Inputs = udpSymbols(fields[0])
	row.Next = fields[len(fields)-1]
	if len(fields) == 3 {
		row.Current = fields[1]
	}
	return row, row.Next != ""
}

// udpSymbols splits "01?(01)" into ["0" "1" "?" "(01)"].
func udpSymbols(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		if s[i] == '(' {
			j := strings.IndexByte(s[i:], ')')
			if j < 0 {
				j = len(s) - i - 1
			}
			out = append(out, s[i:i+j+1])
			i += j + 1
			continue
		}
		out = append(out, s[i:i+1])
		i++
	}
	return out
}
