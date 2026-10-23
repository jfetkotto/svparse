package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parseClass handles "class", "virtual class", "interface class", and
// "virtual interface class", consuming whichever prefix is actually
// present, then a parameter port list, an "extends" clause (each base is a
// class reference with an optional package qualifier and parameter list, see
// parseClassTypeRef, then an optional constructor argument list kept as a
// raw token span -- see ast.Class.ExtendsArgs), and an "implements" clause.
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
		// A class extends one base; an interface class may extend several
		// (LRM 8.26.2), so this is a list either way and a stray comma after a
		// class's single base is tolerated rather than derailing the header.
		for {
			base, ok := p.parseClassTypeRef()
			if !ok {
				break
			}
			cls.ExtendsTypes = append(cls.ExtendsTypes, base)
			if len(cls.ExtendsTypes) == 1 {
				cls.Extends = base.Name
				if p.peek().Kind == token.KindLParen {
					p.advance()
					end := p.parenGroupEnd()
					cls.ExtendsArgs = p.toks[p.pos:end]
					p.pos = end
					if !p.expectKind(token.KindRParen, "')' to close the constructor arguments") {
						break // the header is broken; what follows is the class body or the next declaration
					}
				}
			}
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
	}

	if p.peek().Text == "implements" {
		p.advance()
		for {
			iface, ok := p.parseClassTypeRef()
			if !ok {
				break
			}
			cls.ImplementsTypes = append(cls.ImplementsTypes, iface)
			cls.Implements = append(cls.Implements, iface.Name)
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

// parseClassTypeRef parses a class reference in an extends or implements
// clause: "Base", "pkg::Base", "Base #(5)", or "pkg::Base #(5, .W(2))". The
// result is a Type (Name is the class, PackageQualifier its scope,
// ParamOverrides its parameter values). The override list is the same tolerant
// raw form an instantiation uses, so unusual expressions inside it never add a
// header error; the fill pass parses them.
func (p *parser) parseClassTypeRef() (ast.Type, bool) {
	nameTok, ok := p.expectIdent()
	if !ok {
		return ast.Type{}, false
	}
	t := ast.Type{Name: nameTok.Text}
	t.Position = namePosition(nameTok)
	for p.peek().Kind == token.KindColonColon {
		p.advance()
		next, ok := p.expectIdent()
		if !ok {
			return t, true
		}
		if t.PackageQualifier != "" {
			t.PackageQualifier += "::"
		}
		t.PackageQualifier += t.Name
		t.Name = next.Text
		t.Position = namePosition(next)
	}
	if p.peek().Kind == token.KindHash && p.peekAt(1).Kind == token.KindLParen {
		t.ParamOverrides = p.parseParamOverrideList()
	}
	return t, true
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
	c.EndLine, c.EndCharacter = endOfName(nameTok)

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
		case token.KindLBrace, token.KindTickLBrace:
			// "'{" opens a brace that "}" closes just like a plain "{" does
			// -- an assignment-pattern literal inside a constraint body would
			// otherwise close the block early on its own "}".
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
