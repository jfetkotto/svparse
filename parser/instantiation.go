package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parseVariableOrInstantiation handles the one genuinely ambiguous top-
// level shape this parser has to resolve: "Foo bar ..." starting with a
// plain identifier could be a variable declaration using a typedef'd
// type ("Foo bar;") or a module/interface/program instantiation
// ("Foo bar(...);"). Resolved with bounded lookahead, no backtracking:
// parse the type-like prefix and an optional "#(...)" parameter override
// list (only legal for an instantiation -- its presence alone settles
// the question), split the remaining comma-separated entries the same
// way every other list in this parser is split, and decide instance-vs-
// declarator from the shape of the FIRST entry alone -- SV doesn't allow
// mixing the two within one statement, so every entry gets treated the
// same way once the first settles it.
func (p *parser) parseVariableOrInstantiation() ([]ast.Decl, bool) {
	typeNameTok := p.peek()
	typ := p.parseTypeBase()
	if typ.Name == "" {
		return nil, false // dispatch already confirmed a KindIdent; defensive
	}

	var paramOverrides []ast.ParamOverride
	hasParamOverrides := p.peek().Kind == token.KindHash
	if hasParamOverrides {
		paramOverrides = p.parseParamOverrideList()
	} else {
		// Packed dims only apply to a variable's type -- an instantiation
		// has no such position for them, and a '[' right after a
		// confirmed '#(...)' would actually belong to an instance array
		// dimension on a specific instance instead, parsed per-entry
		// below.
		typ.PackedDims = p.parseDims()
	}

	groups, closed := p.splitByCommaUntil(token.KindSemi)
	if !closed {
		p.errorf(p.peek(), "unterminated declaration, expected ';'")
	}
	if len(groups) == 0 {
		return nil, false
	}

	if hasParamOverrides || firstGroupLooksLikeInstance(groups[0]) {
		inst := &ast.Instantiation{ModuleType: typ.Name, ParamOverrides: paramOverrides}
		inst.Position = namePosition(typeNameTok)
		for _, g := range groups {
			if instance, ok := p.parseInstanceGroup(g); ok {
				inst.Instances = append(inst.Instances, instance)
			}
		}
		if len(inst.Instances) == 0 {
			return nil, false
		}
		return []ast.Decl{inst}, true
	}

	var decls []ast.Decl
	for _, g := range groups {
		if v, ok := p.parseVariableDeclarator(g, typ); ok {
			decls = append(decls, v)
		}
	}
	if len(decls) == 0 {
		return nil, false
	}
	return decls, true
}

// firstGroupLooksLikeInstance reports whether an already-comma-isolated
// group has the shape "name [dims] ( connections )" (an instantiation
// instance) rather than "name [dims] [= initial]" (a variable
// declarator) -- distinguished by whether a '(' follows the name and any
// array dimensions.
func firstGroupLooksLikeInstance(group []preprocessor.Token) bool {
	i := 0
	if i >= len(group) || group[i].Kind != token.KindIdent {
		return false
	}
	i++
	for i < len(group) && group[i].Kind == token.KindLBrack {
		depth := 0
		for i < len(group) {
			switch group[i].Kind {
			case token.KindLBrack:
				depth++
			case token.KindRBrack:
				depth--
			}
			i++
			if depth == 0 {
				break
			}
		}
	}
	return i < len(group) && group[i].Kind == token.KindLParen
}

// parseParamOverrideList parses an instantiation's "#( ... )" parameter
// override list, the cursor expected to be at '#'.
func (p *parser) parseParamOverrideList() []ast.ParamOverride {
	p.advance() // '#'
	if p.peek().Kind != token.KindLParen {
		p.errorf(p.peek(), "expected '(' after '#'")
		return nil
	}
	p.advance() // '('

	groups, newPos, closed := preprocessor.SplitBalancedArgs(p.toks, p.pos)
	p.pos = newPos
	if !closed {
		p.errorf(p.peek(), "unterminated parameter override list")
	}
	if isEmptyGroups(groups) {
		return nil
	}
	overrides := make([]ast.ParamOverride, 0, len(groups))
	for _, g := range groups {
		overrides = append(overrides, parseParamOverrideGroup(g))
	}
	return overrides
}

// parseParamOverrideGroup parses one already-isolated "#(...)" entry:
// ".name(value)" (a named override) or a bare "value" (a positional
// override, Name == ""). Position is the name token itself for a named
// override, not the preceding "." -- a consumer needs the name's own
// position to match a query or compute a correct rename edit range (see
// parsePortConnectionGroup, which follows the identical convention).
func parseParamOverrideGroup(group []preprocessor.Token) ast.ParamOverride {
	if len(group) == 0 {
		return ast.ParamOverride{}
	}
	pos := ast.Position{File: group[0].File, Line: group[0].Line, Character: group[0].Character}
	if len(group) >= 2 && group[0].Kind == token.KindDot && group[1].Kind == token.KindIdent {
		name := group[1].Text
		namePos := ast.Position{File: group[1].File, Line: group[1].Line, Character: group[1].Character}
		if len(group) >= 4 && group[2].Kind == token.KindLParen && group[len(group)-1].Kind == token.KindRParen {
			return ast.ParamOverride{Position: namePos, Name: name, Value: group[3 : len(group)-1]}
		}
		return ast.ParamOverride{Position: namePos, Name: name}
	}
	return ast.ParamOverride{Position: pos, Value: group}
}

// parseInstanceGroup parses one already-comma-isolated instance:
// name {instance-array-dims} ( connections ).
func (p *parser) parseInstanceGroup(group []preprocessor.Token) (ast.Instance, bool) {
	sub := newSubParser(group)
	nameTok, ok := sub.expectIdent()
	if !ok {
		p.mergeErrors(sub)
		return ast.Instance{}, false
	}
	inst := ast.Instance{Name: nameTok.Text, Position: namePosition(nameTok)}
	inst.UnpackedDims = sub.parseDims() // instance array, e.g. "u_leaf[3]"

	if sub.peek().Kind != token.KindLParen {
		p.errorf(nameTok, "expected '(' to start port connections for instance %q", nameTok.Text)
		p.mergeErrors(sub)
		return inst, true // tolerant -- return what was parsed so far
	}
	sub.advance() // '('
	connGroups, newPos, closed := preprocessor.SplitBalancedArgs(sub.toks, sub.pos)
	sub.pos = newPos
	if !closed {
		p.errorf(nameTok, "unterminated port connection list for instance %q", nameTok.Text)
	}
	if !isEmptyGroups(connGroups) {
		inst.Connections = make([]ast.PortConnection, 0, len(connGroups))
		for _, cg := range connGroups {
			inst.Connections = append(inst.Connections, parsePortConnectionGroup(cg))
		}
	}
	p.mergeErrors(sub)
	return inst, true
}

// parsePortConnectionGroup parses one already-isolated port connection:
// ".*" (wildcard), ".name" (implicit shorthand), ".name(expr)" (explicit,
// possibly empty for an intentionally unconnected port), or a bare
// expression (positional, Name == ""). Position is the name token itself
// for a named connection, not the preceding "." -- a consumer needs the
// name's own position to match a query or compute a correct rename edit
// range.
func parsePortConnectionGroup(group []preprocessor.Token) ast.PortConnection {
	if len(group) == 0 {
		return ast.PortConnection{}
	}
	pos := ast.Position{File: group[0].File, Line: group[0].Line, Character: group[0].Character}

	if group[0].Kind == token.KindDotStar {
		return ast.PortConnection{Position: pos, Wildcard: true}
	}
	if group[0].Kind == token.KindDot && len(group) >= 2 && group[1].Kind == token.KindIdent {
		name := group[1].Text
		namePos := ast.Position{File: group[1].File, Line: group[1].Line, Character: group[1].Character}
		if len(group) >= 4 && group[2].Kind == token.KindLParen && group[len(group)-1].Kind == token.KindRParen {
			return ast.PortConnection{Position: namePos, Name: name, Expr: group[3 : len(group)-1]}
		}
		if len(group) >= 3 && group[2].Kind == token.KindLParen {
			// A '(' with no clean matching trailing ')' detected (malformed) --
			// treat everything after '(' as the expression anyway, tolerant.
			return ast.PortConnection{Position: namePos, Name: name, Expr: group[3:]}
		}
		return ast.PortConnection{Position: namePos, Name: name, Implicit: true}
	}
	return ast.PortConnection{Position: pos, Expr: group}
}
