package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parseClass handles "class", "virtual class", "interface class", and
// "virtual interface class", consuming whichever prefix is actually
// present, then a parameter port list, an "extends" clause (with an
// optional package/class qualifier on the base name and an optional
// constructor argument list, kept as a raw token span -- see
// ast.Class.ExtendsArgs), and an "implements" clause.
func (p *parser) parseClass() (ast.Decl, bool) {
	virtual := false
	if p.peek().Text == "virtual" {
		p.advance()
		virtual = true
	}
	isInterfaceClass := false
	if p.peek().Text == "interface" {
		p.advance()
		isInterfaceClass = true
	}
	p.advance() // "class"

	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	cls := &ast.Class{Virtual: virtual, IsInterfaceClass: isInterfaceClass, Name: nameTok.Text}
	cls.Position = namePosition(nameTok)

	cls.Params = p.parseParamPortList()

	if p.peek().Text == "extends" {
		p.advance()
		if baseTok, ok := p.expectIdent(); ok {
			cls.Extends = baseTok.Text
			if p.peek().Kind == token.KindColonColon {
				// "extends pkg::Base" -- keep just the base class name;
				// the package qualifier isn't separately tracked on
				// ast.Class, consistent with this parser's scope elsewhere
				// (e.g. Instantiation.ModuleType is also unqualified).
				p.advance()
				if realBase, ok := p.expectIdent(); ok {
					cls.Extends = realBase.Text
				}
			}
			if p.peek().Kind == token.KindLParen {
				p.advance()
				cls.ExtendsArgs = p.collectUntil(token.KindRParen)
				if p.peek().Kind == token.KindRParen {
					p.advance()
				}
			}
		}
	}

	if p.peek().Text == "implements" {
		p.advance()
		for {
			ifaceTok, ok := p.expectIdent()
			if !ok {
				break
			}
			cls.Implements = append(cls.Implements, ifaceTok.Text)
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	}

	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after class header")
	}

	body, end, _ := p.parseBody("endclass")
	cls.Body = body
	cls.EndLine, cls.EndCharacter = end.Line, end.Character
	return cls, true
}

// parseConstraint parses a class constraint block: constraint name
// { ... }, or an out-of-class-body constraint matching an extern
// prototype ("constraint Cls::name { ... }", the constraint counterpart
// to parseFunction/parseTask's own out-of-class method body handling --
// see resolveQualifiedName). The body is recognized and skipped
// (balanced braces), never parsed -- constraint-expression grammar is
// entirely out of scope for this parser. The cursor is expected to be at
// "constraint" itself -- any qualifier ("static", or "extern"/"pure" for
// a prototype, see parseConstraintPrototype) is the caller's
// responsibility to consume first, mirroring how parseFunction/parseTask
// expect to start at "function"/"task".
func (p *parser) parseConstraint() (ast.Decl, bool) {
	p.advance() // "constraint"

	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	nameTok = p.resolveQualifiedName(nameTok)
	c := &ast.Constraint{Name: nameTok.Text}
	c.Position = namePosition(nameTok)

	end := p.skipBracedBlock()
	c.EndLine, c.EndCharacter = end.Line, end.Character
	return c, true
}

// parseConstraintPrototype parses an "extern constraint name;" / "pure
// constraint name;" prototype (LRM 18.5.1/18.5.2) -- a name and a
// terminating ';', no braced body at all, mirroring parseFunction/
// parseTask's own isPrototype=true shape. The cursor is expected to be
// at "constraint" -- the caller has already consumed "extern"/"pure" and
// any qualifiers between it and "constraint".
func (p *parser) parseConstraintPrototype() (ast.Decl, bool) {
	p.advance() // "constraint"

	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	c := &ast.Constraint{Name: nameTok.Text, Prototype: true}
	c.Position = namePosition(nameTok)
	c.EndLine, c.EndCharacter = c.Line, c.Character

	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after constraint prototype")
	}
	return c, true
}

// skipBracedBlock consumes a balanced "{ ... }" block, the cursor
// expected to be at the opening '{', and returns the position of the
// closing '}' (or wherever scanning stopped, if unterminated).
func (p *parser) skipBracedBlock() (closeTok preprocessor.Token) {
	if p.peek().Kind != token.KindLBrace {
		p.errorf(p.peek(), "expected '{' to start block")
		return p.peek()
	}
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unterminated block, expected '}'")
			return tok
		}
		switch tok.Kind {
		case token.KindLBrace:
			depth++
		case token.KindRBrace:
			depth--
			if depth == 0 {
				closeTok = tok
				p.advance()
				return closeTok
			}
		}
		p.advance()
	}
}
