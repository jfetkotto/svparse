package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// dataTypeKeywords are the builtin type keywords that can start a
// variable/net declaration at the dispatch level (parseDecl) -- an
// identifier starting a declaration is ambiguous with a module
// instantiation and isn't handled until that disambiguation exists (a
// later commit).
var dataTypeKeywords = map[string]bool{
	"logic": true, "bit": true, "reg": true,
	"wire": true, "tri": true, "tri0": true, "tri1": true, "triand": true, "trior": true, "trireg": true,
	"wand": true, "wor": true, "uwire": true, "supply0": true, "supply1": true,
	"int": true, "integer": true, "byte": true, "shortint": true, "longint": true,
	"real": true, "shortreal": true, "realtime": true, "time": true,
	"string": true, "chandle": true, "event": true,
	"signed": true, "unsigned": true,
	"genvar": true, // LRM 27.4 -- a generate for-loop's index variable, declared like any other variable outside the loop header itself
}

// netTypeKeywords are the net types (LRM 6.7.1's net_type) that may
// precede a data type rather than being one: "net_port_type ::=
// [net_type] data_type_or_implicit" (LRM 23.2.2.3). Deliberately its own
// set rather than a subset check against dataTypeKeywords, which also
// holds "logic", "int", "signed" and friends -- none of those may ever be
// swallowed as a leading qualifier.
var netTypeKeywords = map[string]bool{
	"wire": true, "tri": true, "tri0": true, "tri1": true, "triand": true, "trior": true, "trireg": true,
	"wand": true, "wor": true, "uwire": true, "supply0": true, "supply1": true,
}

// nonTypeNameQualifiers are keywords that occupy the slot a data type
// would, and lex as type-name tokens, but qualify a type rather than being
// one: LRM 6.9.2's "vectored"/"scalared" net qualifiers, and the signing
// qualifiers parseTypeBase consumes on their own. A net-type keyword
// followed by one of these is still naming its OWN type ("tri1 scalared
// [15:0] a"), so the [net_type] data_type shape must not claim it.
var nonTypeNameQualifiers = map[string]bool{
	"signed": true, "unsigned": true, "vectored": true, "scalared": true,
}

func isVariableStartKeyword(text string) bool {
	return dataTypeKeywords[text]
}

// parseVariableDecl parses a net/variable declaration statement, which
// can declare several names at once sharing one type -- "logic [7:0] a,
// b, c;" becomes three ast.Variable nodes. Unlike a port or parameter
// entry, there's no implicit/explicit-type ambiguity to resolve here:
// parseDecl only calls this once it has already confirmed a builtin type
// keyword starts the statement.
func (p *parser) parseVariableDecl() ([]ast.Decl, bool) {
	return p.parseVariableDeclQualified(false, false, false)
}

// parseVariableDeclQualified is parseVariableDecl generalized with
// explicit rand/randc/static qualifiers, for a class property ("rand
// bit [7:0] payload;", "static int count;"). Unlike the plain builtin-
// type-keyword dispatch, the type here can also be an identifier (a
// class/typedef name, e.g. "rand packet_t pkt;") -- parseTypeBase
// already accepts either generically, and a qualifier keyword is itself
// an unambiguous enough signal (never a module instantiation) that this
// doesn't need to wait for the general identifier-vs-instantiation
// disambiguation a later commit adds.
func (p *parser) parseVariableDeclQualified(isRand, isRandC, isStatic bool) ([]ast.Decl, bool) {
	p.consumeNetTypeQualifier()
	typ := p.parseTypeBase()
	typ.PackedDims = p.parseDims()

	groups, closed := p.splitDeclaratorsToSemi()
	if !closed {
		p.errorf(p.peek(), "unterminated variable declaration, expected ';'")
	}

	var decls []ast.Decl
	for _, group := range groups {
		if v, ok := p.parseVariableDeclarator(group, typ); ok {
			v.IsRand = isRand
			v.IsRandC = isRandC
			v.IsStatic = isStatic
			decls = append(decls, v)
		}
	}
	// ok=true even with nothing to show for it: the statement was consumed
	// through its ';' above, so this is "parsed, produced no declarations",
	// not "didn't recognize anything here". Reporting failure instead would
	// send parseBody into recover() at a cursor that has already moved past
	// this statement, eating whatever follows -- see parseBody's own guard.
	// Every group that failed already recorded its own error via
	// parseVariableDeclarator, so nothing goes unreported.
	return decls, true
}

// parseVariableDeclarator parses one already-comma-isolated declarator:
// name {unpacked-dims} [= initial]. Shared with struct/union member
// parsing (see typedef.go), which has the identical shape.
func (p *parser) parseVariableDeclarator(group []preprocessor.Token, typ ast.Type) (*ast.Variable, bool) {
	sub := p.newSubParser(group)
	nameTok, ok := sub.expectIdent()
	if !ok {
		p.mergeErrors(sub)
		return nil, false
	}
	v := &ast.Variable{Type: typ, Name: nameTok.Text}
	v.Position = namePosition(nameTok)
	v.UnpackedDims = sub.parseDims()
	if sub.peek().Kind == token.KindAssign {
		sub.advance()
		v.Initial = sub.remainingTokens()
	} else if sub.pos < len(sub.toks) {
		// Anything left unconsumed here (not a '[', not a '=') is
		// malformed -- e.g. a stray extra identifier with no separating
		// comma. Recorded rather than silently discarded, since the
		// group-splitting model this parser uses throughout otherwise
		// has no other way to notice trailing garbage within one already-
		// isolated declarator.
		p.errorf(sub.peek(), "unexpected trailing tokens after declaration of %q", nameTok.Text)
	}
	p.mergeErrors(sub)
	return v, true
}

// parseParameterDecl parses a body-level "parameter"/"localparam"
// declaration (as opposed to a parameter port list entry, see
// parseParamPortEntry in ports.go -- a different context with a
// different sharing rule). A single statement can declare several names,
// which share ONE type resolved (implicit-vs-explicit, same ambiguity
// and rewind approach as parseTypeAndName) from the first declarator
// only -- "parameter int A = 1, B = 2;" declares both A and B as int,
// matching how a variable declaration's shared type works, not how each
// independent entry in a parameter port list has its own.
func (p *parser) parseParameterDecl() ([]ast.Decl, bool) {
	isLocal := p.peek().Text == "localparam"
	p.advance() // "parameter"/"localparam"

	typ, nameTok, ok := p.parseTypeAndName()
	if !ok {
		return nil, false
	}
	// A "type" parameter (LRM 6.20.4, "parameter type name = logic;")
	// legitimately has a bare type/user keyword as its default value --
	// parseTypeBase's isTypeNameToken accepts any keyword as a type name,
	// so the bare "type" keyword itself parses as typ.Name here, and
	// what follows "=" is a type, not an expression. collectExprUntil's
	// isDeclBoundaryKeyword check would misread that value (e.g. the
	// builtin keyword "logic") as the next declaration already starting;
	// collectUntil, with no such awareness, is the correct, deliberate
	// choice for this one case.
	collectDefault := p.collectExprUntil
	if typ.Name == "type" {
		collectDefault = p.collectUntil
	}

	makeParam := func(nameTok preprocessor.Token) *ast.Parameter {
		param := &ast.Parameter{IsLocal: isLocal, Type: typ, Name: nameTok.Text}
		param.Position = namePosition(nameTok)
		return param
	}

	first := makeParam(nameTok)
	first.UnpackedDims = p.parseDims()
	if p.peek().Kind == token.KindAssign {
		p.advance()
		first.Default = collectDefault(token.KindComma, token.KindSemi)
	}
	decls := []ast.Decl{first}

	for p.peek().Kind == token.KindComma {
		p.advance()
		nt, ok := p.expectIdent()
		if !ok {
			break
		}
		next := makeParam(nt)
		next.UnpackedDims = p.parseDims()
		if p.peek().Kind == token.KindAssign {
			p.advance()
			next.Default = collectDefault(token.KindComma, token.KindSemi)
		}
		decls = append(decls, next)
	}

	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after parameter declaration")
	}
	return decls, true
}

// parseVirtualInterfaceDecl parses a virtual interface handle declaration
// (LRM 25.9): "virtual [interface] IfaceName[.modport] name [, name ...];"
// -- a class property or module-scope variable typed as an interface,
// optionally restricted to one of its modports. The cursor is expected to
// be at "virtual", with the caller (parseDecl's "virtual" dispatch)
// already having ruled out "virtual [interface] class" and "virtual
// function/task".
//
// This can't just be parseVariableDeclQualified with a different prefix
// consumed first: an ordinary variable's type is a single name (plus
// optional pkg::-qualification, handled generically by parseTypeBase),
// but a virtual interface handle's type can carry a ".modport" suffix
// after the interface name, which parseTypeBase has no notion of --
// dot-qualification is otherwise never legal directly after a type name
// this parser recognizes.
func (p *parser) parseVirtualInterfaceDecl() ([]ast.Decl, bool) {
	p.advance() // "virtual"
	if p.peek().Text == "interface" {
		p.advance() // optional -- "virtual my_if vif;" is equally legal with it omitted
	}

	typ := p.parseTypeBase()
	if typ.Name == "" {
		return nil, false
	}
	if p.peek().Kind == token.KindDot && p.peekAt(1).Kind == token.KindIdent {
		p.advance() // '.'
		p.advance() // modport name -- not tracked on ast.Type, same "consumed, not tracked" treatment as other qualifiers this parser sees
	}
	typ.PackedDims = p.parseDims()

	groups, closed := p.splitDeclaratorsToSemi()
	if !closed {
		p.errorf(p.peek(), "unterminated variable declaration, expected ';'")
	}

	var decls []ast.Decl
	for _, group := range groups {
		if v, ok := p.parseVariableDeclarator(group, typ); ok {
			decls = append(decls, v)
		}
	}
	return decls, true // consumed through ';' -- see parseVariableDeclQualified
}
