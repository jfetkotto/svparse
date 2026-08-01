package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parseTypedef parses a typedef: "typedef struct {...} name;", "typedef
// enum {...} name;", "typedef union {...} name;", "typedef <type>
// name;" (a plain alias), or "typedef class Name;"/"typedef Name;" (a
// forward declaration, Underlying nil).
//
// Not handled: declaring a variable of an anonymous, non-typedef'd
// struct/union/enum type directly ("struct packed {...} my_var;" with
// no typedef at all) -- legal SV, but out of scope for this milestone;
// struct/union/enum support here is reached only through typedef.
func (p *parser) parseTypedef() (ast.Decl, bool) {
	p.advance() // "typedef"

	if p.peek().Text == "class" {
		p.advance() // optional in a forward declaration: "typedef class Foo;"
	}

	switch p.peek().Text {
	case "struct":
		return p.parseTypedefStruct()
	case "union":
		return p.parseTypedefUnion()
	case "enum":
		return p.parseTypedefEnum()
	}

	// Plain alias or forward declaration -- parseTypeAndName's rewind
	// already distinguishes "typedef Foo;" (Foo is the name, no type
	// found -- forward declaration) from "typedef logic [7:0] byte_t;"
	// (byte_t is the name, logic [7:0] is a real type).
	typ, nameTok, ok := p.parseTypeAndName()
	if !ok {
		return nil, false
	}
	td := &ast.Typedef{Name: nameTok.Text}
	td.Position = namePosition(nameTok)
	if typ.Name != "" {
		alias := &ast.TypeAlias{Type: typ}
		alias.Position = td.Position
		td.Underlying = alias
	}
	p.skipHeaderToSemi()
	return td, true
}

func (p *parser) parseTypedefStruct() (ast.Decl, bool) {
	p.advance() // "struct"
	packed := false
	if p.peek().Text == "packed" {
		p.advance()
		packed = true
	}
	p.consumeSignedUnsigned() // "struct packed signed {...}" -- consumed, Struct has no signedness field of its own (members carry their own)

	members, ok := p.parseStructUnionBody()
	if !ok {
		return nil, false
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}

	s := &ast.Struct{Packed: packed, Members: members}
	s.Position = namePosition(nameTok)
	td := &ast.Typedef{Name: nameTok.Text, Underlying: s}
	td.Position = s.Position
	p.skipHeaderToSemi()
	return td, true
}

func (p *parser) parseTypedefUnion() (ast.Decl, bool) {
	p.advance() // "union"
	if p.peek().Text == "tagged" {
		p.advance() // "union tagged {...}" -- consumed, not separately tracked
	}
	packed := false
	if p.peek().Text == "packed" {
		p.advance()
		packed = true
	}
	p.consumeSignedUnsigned()

	members, ok := p.parseStructUnionBody()
	if !ok {
		return nil, false
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}

	u := &ast.Union{Packed: packed, Members: members}
	u.Position = namePosition(nameTok)
	td := &ast.Typedef{Name: nameTok.Text, Underlying: u}
	td.Position = u.Position
	p.skipHeaderToSemi()
	return td, true
}

// parseStructUnionBody parses a struct/union's "{ member; member; ...
// }" body, the cursor expected to be at the opening '{'. Each member
// uses the exact same declarator shape a variable declaration does
// (parseVariableDeclarator), including sharing one type across a
// comma-separated group of names.
func (p *parser) parseStructUnionBody() ([]ast.Variable, bool) {
	if p.peek().Kind != token.KindLBrace {
		p.errorf(p.peek(), "expected '{' to start struct/union body")
		return nil, false
	}
	p.advance() // '{'

	var members []ast.Variable
	for p.peek().Kind != token.KindRBrace {
		if p.peek().Kind == token.KindEOF {
			p.errorf(p.peek(), "unterminated struct/union body, expected '}'")
			return members, false
		}
		typ := p.parseTypeBase()
		typ.PackedDims = p.parseDims()
		groups, closed := p.splitByCommaUntil(token.KindSemi)
		if !closed {
			p.errorf(p.peek(), "unterminated struct/union member, expected ';'")
			break
		}
		for _, group := range groups {
			if v, ok := p.parseVariableDeclarator(group, typ); ok {
				members = append(members, *v)
			}
		}
	}
	if p.peek().Kind == token.KindRBrace {
		p.advance()
	}
	return members, true
}

func (p *parser) parseTypedefEnum() (ast.Decl, bool) {
	p.advance() // "enum"
	var baseType ast.Type
	if p.peek().Kind != token.KindLBrace {
		baseType = p.parseTypeBase()
		baseType.PackedDims = p.parseDims()
	}

	members, ok := p.parseEnumBody()
	if !ok {
		return nil, false
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}

	e := &ast.Enum{BaseType: baseType, Members: members}
	e.Position = namePosition(nameTok)
	td := &ast.Typedef{Name: nameTok.Text, Underlying: e}
	td.Position = e.Position
	p.skipHeaderToSemi()
	return td, true
}

// parseEnumBody parses an enum's "{ A, B = value, ... }" body.
func (p *parser) parseEnumBody() ([]ast.EnumMember, bool) {
	if p.peek().Kind != token.KindLBrace {
		p.errorf(p.peek(), "expected '{' to start enum body")
		return nil, false
	}
	p.advance() // '{'

	groups, closed := p.splitByCommaUntil(token.KindRBrace)
	if !closed {
		p.errorf(p.peek(), "unterminated enum body, expected '}'")
	}
	var members []ast.EnumMember
	for _, group := range groups {
		if m, ok := p.parseEnumMemberGroup(group); ok {
			members = append(members, m)
		}
	}
	return members, true
}

// parseEnumMemberGroup parses one already-comma-isolated enum member:
// name [[repeat-count]] [= value]. The optional bracketed repeat-count
// (an auto-increment feature for declaring several sequential values at
// once) is consumed via parseDims' identical bracket syntax but not
// otherwise represented -- a rare feature, out of scope beyond not
// letting it confuse the parser.
func (p *parser) parseEnumMemberGroup(group []preprocessor.Token) (ast.EnumMember, bool) {
	sub := newSubParser(group)
	nameTok, ok := sub.expectIdent()
	if !ok {
		p.mergeErrors(sub)
		return ast.EnumMember{}, false
	}
	m := ast.EnumMember{Name: nameTok.Text}
	m.Position = namePosition(nameTok)
	if sub.peek().Kind == token.KindLBrack {
		sub.parseDims()
	}
	if sub.peek().Kind == token.KindAssign {
		sub.advance()
		m.Value = sub.remainingTokens()
	}
	p.mergeErrors(sub)
	return m, true
}
