package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parsePortList parses a module/interface/program's ANSI port list. Only
// ANSI-style port lists are supported -- the same scope limit sigils's own
// scanPortList already has (see internal/sv/scan.go there) -- carried
// forward deliberately, not a new limitation.
func (p *parser) parsePortList() []ast.Port {
	if p.peek().Kind != token.KindLParen {
		return nil
	}
	p.advance() // '('

	groups, newPos, closed := preprocessor.SplitBalancedArgs(p.toks, p.pos)
	p.pos = newPos
	if !closed {
		p.errorf(p.peek(), "unterminated port list")
	}
	if isEmptyGroups(groups) {
		return nil
	}

	var ports []ast.Port
	for _, group := range groups {
		if port, ok := p.parsePortEntry(group); ok {
			ports = append(ports, port)
		}
	}
	return ports
}

// parsePortEntry parses one already-comma-isolated port list entry:
// [direction ["var"]] [type] name {unpacked-dims} [= default]. A port
// that doesn't specify its own direction gets ast.DirUnspecified, not a
// resolved/inherited one -- see ast.Direction's doc comment for why the
// parser doesn't resolve LRM direction inheritance itself.
func (p *parser) parsePortEntry(group []preprocessor.Token) (ast.Port, bool) {
	sub := p.newSubParser(group)
	dir := sub.consumeDirection()

	typ, nameTok, ok := sub.parseInterfacePortHeader()
	if !ok {
		typ, nameTok, ok = sub.parseTypeAndName()
	}
	if !ok {
		p.mergeErrors(sub)
		return ast.Port{}, false
	}

	port := ast.Port{Direction: dir, Type: typ, Name: nameTok.Text}
	port.Position = namePosition(nameTok)
	port.UnpackedDims = sub.parseDims()
	if sub.peek().Kind == token.KindAssign {
		sub.advance()
		port.Default = sub.remainingTokens()
	} else if sub.pos < len(sub.toks) {
		// Anything left unconsumed here (not a '[', not a '=') is
		// malformed -- e.g. stray trailing tokens from a shape this parser
		// doesn't recognize (see parseInterfacePortHeader, added
		// specifically because this check used to be entirely absent: an
		// interface port like "simple_bus.slave sb" silently discarded
		// ".slave sb" and misparsed "simple_bus" itself as the port name,
		// with no error to notice it by). Recorded rather than silently
		// discarded, same reasoning as parseVariableDeclarator's identical
		// check.
		p.errorf(sub.peek(), "unexpected trailing tokens after port %q", nameTok.Text)
	}
	p.mergeErrors(sub)
	return port, true
}

// parseInterfacePortHeader recognizes an interface port's type header
// (LRM 25.3's interface_port_header): either "IfaceName[.modport]" (a
// specific interface type, disambiguated from an ordinary "[type] name"
// port entry by the immediately-following '.') or the generic "interface
// [.modport]" keyword form (any interface type). Both are followed by the
// port's own name -- unlike parseTypeAndName's "[type] name" shape, there
// is no implicit/explicit-type ambiguity to resolve here, since neither
// shape is legal syntax for a plain data-typed port at all.
//
// Reports ok=false, with sub's cursor left untouched, if the group
// doesn't start with either shape -- the caller falls back to
// parseTypeAndName for an ordinary port (including the bare
// "IfaceName name" form with no ".modport", which parseTypeAndName
// already handles correctly: it's lexically identical to any other
// "[type] name" port).
func (sub *parser) parseInterfacePortHeader() (ast.Type, preprocessor.Token, bool) {
	save := sub.pos
	typeTok := sub.peek()

	var typeName string
	switch {
	case typeTok.Kind == token.KindKeyword && typeTok.Text == "interface":
		typeName = "interface"
		sub.advance()
	case typeTok.Kind == token.KindIdent && sub.peekAt(1).Kind == token.KindDot:
		typeName = typeTok.Text
		sub.advance()
	default:
		return ast.Type{}, preprocessor.Token{}, false
	}
	typ := ast.Type{Position: namePosition(typeTok), Name: typeName}

	if sub.peek().Kind == token.KindDot && sub.peekAt(1).Kind == token.KindIdent {
		sub.advance() // '.'
		sub.advance() // modport name -- not tracked on ast.Type, same "consumed, not tracked" treatment as other qualifiers this parser sees
	}

	// A plain Kind check, not expectIdent -- this is a shape probe that
	// can legitimately not match (falling back to parseTypeAndName), and
	// expectIdent would record a spurious "expected an identifier" error
	// into sub.errs that the fallback attempt has no way to un-record.
	nameTok := sub.peek()
	if nameTok.Kind != token.KindIdent {
		sub.pos = save
		return ast.Type{}, preprocessor.Token{}, false
	}
	sub.advance()
	return typ, nameTok, true
}

func (p *parser) consumeDirection() ast.Direction {
	var dir ast.Direction
	switch p.peek().Text {
	case "input":
		dir = ast.DirInput
	case "output":
		dir = ast.DirOutput
	case "inout":
		dir = ast.DirInout
	case "ref":
		dir = ast.DirRef
	default:
		return ast.DirUnspecified
	}
	p.advance()
	if p.peek().Text == "var" { // "ref var" / an explicit variable-port modifier -- consumed, not separately tracked
		p.advance()
	}
	return dir
}

// parseParamPortList parses a container or class's "#( ... )" parameter
// port list.
func (p *parser) parseParamPortList() []ast.Parameter {
	if p.peek().Kind != token.KindHash {
		return nil
	}
	p.advance() // '#'
	if p.peek().Kind != token.KindLParen {
		p.errorf(p.peek(), "expected '(' after '#'")
		return nil
	}
	p.advance() // '('

	groups, newPos, closed := preprocessor.SplitBalancedArgs(p.toks, p.pos)
	p.pos = newPos
	if !closed {
		p.errorf(p.peek(), "unterminated parameter port list")
	}
	if isEmptyGroups(groups) {
		return nil
	}

	var params []ast.Parameter
	for _, group := range groups {
		param, ok := p.parseParamPortEntry(group)
		if ok {
			params = append(params, param)
		}
	}
	return params
}

// parseParamPortEntry parses one already-comma-isolated parameter port
// list entry: [parameter|localparam] [type] name [= default]. The
// leading "parameter"/"localparam" keyword is only required on the
// first entry per the LRM (later entries inherit it); this just consumes
// it if present on any entry, uniformly.
func (p *parser) parseParamPortEntry(group []preprocessor.Token) (ast.Parameter, bool) {
	sub := p.newSubParser(group)
	isLocal := false
	switch sub.peek().Text {
	case "parameter":
		sub.advance()
	case "localparam":
		sub.advance()
		isLocal = true
	}

	typ, nameTok, ok := sub.parseTypeAndName()
	if !ok {
		p.mergeErrors(sub)
		return ast.Parameter{}, false
	}

	param := ast.Parameter{IsLocal: isLocal, Type: typ, Name: nameTok.Text}
	param.Position = namePosition(nameTok)
	param.UnpackedDims = sub.parseDims()
	if sub.peek().Kind == token.KindAssign {
		sub.advance()
		param.Default = sub.remainingTokens()
	}
	p.mergeErrors(sub)
	return param, true
}

// isEmptyGroups reports whether groups is exactly what SplitBalancedArgs
// returns for a bare "()" -- one single empty group, meaning zero
// entries were actually written, not one blank one.
func isEmptyGroups(groups [][]preprocessor.Token) bool {
	return len(groups) == 1 && len(groups[0]) == 0
}
