package parser

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/token"
)

// parseTypedef parses a typedef: "typedef struct {...} name;", "typedef
// enum {...} name;", "typedef union {...} name;", "typedef <type>
// name;" (a plain alias), or "typedef class Name;"/"typedef Name;" (a
// forward declaration, Underlying nil).
//
// A variable of an anonymous, non-typedef'd aggregate type ("struct packed
// {...} my_var;") is not a typedef; see parseDataType.
func (p *parser) parseTypedef() (ast.Decl, bool) {
	p.advance() // "typedef"

	forwardKind := ""
	if p.peek().Text == "class" {
		p.advance() // optional in a forward declaration: "typedef class Foo;"
		forwardKind = "class"
	}

	if p.atAggregate() {
		// "typedef struct Foo;" is a forward declaration (LRM 6.18): the
		// keyword, a name, and a ';' with no body.
		if p.peekAt(1).Kind == token.KindIdent && p.peekAt(2).Kind == token.KindSemi {
			kw := p.advance()
			nameTok := p.advance()
			td := &ast.Typedef{Name: nameTok.Text, ForwardKind: kw.Text}
			td.Position = namePosition(nameTok)
			p.skipHeaderToSemiStrict()
			return td, true
		}
		return p.parseTypedefAggregate()
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
	} else {
		td.ForwardKind = forwardKind
	}
	p.skipHeaderToSemiStrict()
	return td, true
}

// parseTypedefAggregate parses "typedef struct|union|enum {...} name;". The
// body is shared with the anonymous form (parseInlineAggregate); here a name
// must follow it, and the aggregate is positioned on that name.
func (p *parser) parseTypedefAggregate() (ast.Decl, bool) {
	t, ok := p.parseInlineAggregate()
	if !ok {
		return nil, false
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}

	pos := namePosition(nameTok)
	switch d := t.Inline.(type) {
	case *ast.Struct:
		d.Position = pos
	case *ast.Union:
		d.Position = pos
	case *ast.Enum:
		d.Position = pos
	}
	td := &ast.Typedef{Name: nameTok.Text, Underlying: t.Inline}
	td.Position = pos
	p.skipHeaderToSemiStrict()
	return td, true
}

// aggregateKeywords begin an anonymous struct, union, or enum type.
var aggregateKeywords = map[string]bool{"struct": true, "union": true, "enum": true}

func (p *parser) atAggregate() bool {
	tok := p.peek()
	return tok.Kind == token.KindKeyword && aggregateKeywords[tok.Text]
}

// parseInlineAggregate parses a struct, union, or enum type from its keyword
// through its closing brace, with no name after it: the cursor is on
// "struct"/"union"/"enum". It returns a Type named for the keyword whose
// Inline holds the aggregate, positioned from the keyword to the closing
// brace. Sign, "packed", and "tagged" are recorded. ok is false if the body
// could not be parsed (the error is already recorded).
//
// A typedef (parseTypedefAggregate) and a type written in place
// ("struct packed {...} s;", "enum {A, B} state;", see parseDataType) share
// this; only what follows the closing brace differs.
func (p *parser) parseInlineAggregate() (ast.Type, bool) {
	start := p.pos
	kw := p.advance()
	t := ast.Type{Name: kw.Text}
	t.Position = namePosition(kw)

	switch kw.Text {
	case "struct":
		s := &ast.Struct{}
		if p.peek().Text == "packed" {
			p.advance()
			s.Packed = true
		}
		t.Signed, t.Unsigned = p.consumeSignedUnsigned()
		members, ok := p.parseStructUnionBody()
		if !ok {
			return t, false
		}
		s.Members = members
		s.Position = p.spanFrom(start)
		t.Inline = s

	case "union":
		u := &ast.Union{}
		if p.peek().Text == "tagged" {
			p.advance()
			u.Tagged = true
		}
		if p.peek().Text == "packed" {
			p.advance()
			u.Packed = true
		}
		t.Signed, t.Unsigned = p.consumeSignedUnsigned()
		members, ok := p.parseStructUnionBody()
		if !ok {
			return t, false
		}
		u.Members = members
		u.Position = p.spanFrom(start)
		t.Inline = u

	default: // "enum"
		e := &ast.Enum{}
		if p.peek().Kind != token.KindLBrace {
			e.BaseType = p.parseTypeBase()
			e.BaseType.PackedDims = p.parseDims()
		}
		members, ok := p.parseEnumBody()
		if !ok {
			return t, false
		}
		e.Members = members
		e.Position = p.spanFrom(start)
		t.Inline = e
	}
	return t, true
}

// structBodyStopKeywords can never begin a struct or union member, so meeting
// one where a member is expected means the body's closing '}' is missing and
// the token belongs to the enclosing scope. Without this a missing brace made
// the member loop read "endmodule" as a type name and carry on through the
// rest of the file. The end keywords (isEndKeyword) stop it too.
var structBodyStopKeywords = func() map[string]bool {
	m := map[string]bool{
		"module": true, "macromodule": true, "interface": true, "program": true,
		"class": true, "package": true, "function": true, "task": true,
		"typedef": true, "parameter": true, "localparam": true, "import": true,
		"assign": true, "initial": true, "final": true, "generate": true, "endgenerate": true,
		"always": true, "always_comb": true, "always_ff": true, "always_latch": true,
	}
	for k := range endKeywords {
		m[k] = true
	}
	return m
}()

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
		if tok := p.peek(); tok.Kind == token.KindEOF || (tok.Kind == token.KindKeyword && structBodyStopKeywords[tok.Text]) {
			// Missing '}': stop here, unconsumed, so whatever follows (the
			// rest of the module, the next module) is not read as members.
			p.errorf(tok, "unterminated struct/union body, expected '}'")
			return members, false
		}
		// An optional random qualifier (LRM 7.2): "rand bit i;".
		isRand, isRandC := false, false
		switch p.peek().Text {
		case "rand":
			p.advance()
			isRand = true
		case "randc":
			p.advance()
			isRandC = true
		}
		typ := p.parseDataType() // a member may itself be an anonymous struct/union/enum
		groups, closed := p.splitDeclaratorsToSemi()
		if !closed {
			p.errorf(p.peek(), "unterminated struct/union member, expected ';'")
			break
		}
		for _, group := range groups {
			if v, ok := p.parseVariableDeclarator(group, typ); ok {
				v.IsRand, v.IsRandC = isRand, isRandC
				members = append(members, *v)
			}
		}
	}
	if p.peek().Kind == token.KindRBrace {
		p.advance()
	}
	return members, true
}

// parseEnumBody parses an enum's "{ A, B = value, ... }" body.
func (p *parser) parseEnumBody() ([]ast.EnumMember, bool) {
	if p.peek().Kind != token.KindLBrace {
		p.errorf(p.peek(), "expected '{' to start enum body")
		return nil, false
	}
	p.advance() // '{'

	// Declaration-boundary aware, like a declarator list: a missing '}' must
	// not carry the member list through the declarations that follow.
	groups, closed := p.splitByComma(token.KindRBrace, true)
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
	sub := p.newSubParser(group)
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
