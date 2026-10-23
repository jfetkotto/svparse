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
	"interconnect": true, // LRM 6.6.8 -- a net with no data type, only for connecting ports
	"signed":       true, "unsigned": true,
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
	return dataTypeKeywords[text] || aggregateKeywords[text]
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
	isNet := p.consumeNetTypeQualifier()
	typ := p.parseDataType()
	isNet = isNet || netTypeKeywords[typ.Name] || typ.Name == "interconnect"
	qualifier, strength, delay := p.parseNetSuffix(&typ, isNet)

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
			v.NetQualifier = qualifier
			v.DriveStrength = strength
			// One delay serves every name in the statement, but each Variable
			// owns its copy so the tree has no shared nodes.
			v.Delay = delay
			if len(decls) > 0 {
				v.Delay = cloneExprs(delay)
			}
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

// strengthKeywords are the drive strengths (LRM 28.11) and charge strengths
// (LRM 28.16) that can appear in a net declaration's "( ... )".
var strengthKeywords = map[string]bool{
	"supply0": true, "strong0": true, "pull0": true, "weak0": true, "highz0": true,
	"supply1": true, "strong1": true, "pull1": true, "weak1": true, "highz1": true,
	"small": true, "medium": true, "large": true,
}

// parseNetSuffix parses what may follow a net declaration's type name, in
// any order (LRM 6.7): a drive or charge strength "(strong0, pull1)", the
// "vectored" or "scalared" qualifier, packed dimensions that come after one of
// those ("tri1 scalared [15:0] a"), and a delay "#10" or "#(1, 2, 3)".
// Strength and delay only follow a net type (isNet); the qualifier is accepted
// anywhere, since whether it suits the type is a semantic question.
func (p *parser) parseNetSuffix(typ *ast.Type, isNet bool) (qualifier string, strength []string, delay []ast.Expr) {
	for {
		tok := p.peek()
		switch {
		case isNet && tok.Kind == token.KindLParen && p.peekAt(1).Kind == token.KindKeyword && strengthKeywords[p.peekAt(1).Text]:
			strength = p.parseStrength()
		case tok.Kind == token.KindKeyword && (tok.Text == "vectored" || tok.Text == "scalared"):
			qualifier = tok.Text
			p.advance()
		case tok.Kind == token.KindLBrack && len(typ.PackedDims) == 0:
			typ.PackedDims = p.parseDims()
		case isNet && tok.Kind == token.KindHash:
			delay = p.parseDelay3()
		default:
			return qualifier, strength, delay
		}
	}
}

// parseStrength parses "(strong0, pull1)" or "(small)", the cursor on '('.
func (p *parser) parseStrength() []string {
	p.advance() // '('
	var out []string
	afterComma := false
	for {
		tok := p.peek()
		if tok.Kind != token.KindKeyword || !strengthKeywords[tok.Text] {
			if afterComma {
				p.errorf(tok, "expected a strength after ',', found %s", describeTok(tok))
			}
			break
		}
		out = append(out, tok.Text)
		p.advance()
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
		afterComma = true
	}
	p.expectKind(token.KindRParen, "')' to close the drive strength")
	return out
}

// parseDelay3 parses "#10", "#d", "#1.5ns", or "#(rise, fall, turnoff)" (LRM
// 28.16), the cursor on '#'. A parenthesised value may be a min:typ:max triple.
func (p *parser) parseDelay3() []ast.Expr {
	p.advance() // '#'
	if p.peek().Kind != token.KindLParen {
		return []ast.Expr{p.parsePrimary()} // a plain delay value, not an expression: "#10 w" must stop before w
	}
	p.advance() // '('
	var out []ast.Expr
	for {
		start := p.pos
		e := p.parseExpr()
		if p.peek().Kind == token.KindColon {
			p.advance()
			typ := p.parseExpr()
			var max ast.Expr
			if p.expectKind(token.KindColon, "':' in a min:typ:max delay") {
				max = p.parseExpr()
			} else {
				max = p.emptyBad()
			}
			e = finish(p, start, &ast.MinTypMax{Min: e, Typ: typ, Max: max})
		}
		out = append(out, e)
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	p.expectKind(token.KindRParen, "')' to close the delay")
	return out
}

// parseNettype parses a user-defined net type declaration (LRM 6.6.7):
// "nettype data_type name [with resolve_function];", the cursor on "nettype".
func (p *parser) parseNettype() (ast.Decl, bool) {
	p.advance() // "nettype"
	typ, nameTok, ok := p.parseTypeAndName()
	if !ok {
		return nil, false
	}
	if typ.Name == "" && typ.Inline == nil && typ.TypeRef == nil {
		p.errorf(nameTok, "expected a data type before the net type name %q", nameTok.Text)
	}
	nt := &ast.Nettype{Name: nameTok.Text, Type: typ}
	nt.Position = namePosition(nameTok)

	if p.peek().Kind == token.KindKeyword && p.peek().Text == "with" {
		p.advance()
		// The resolution function may be package-qualified: pkg::resolve.
		for {
			fn, ok := p.expectIdent()
			if !ok {
				break
			}
			nt.ResolveFunc += fn.Text
			if p.peek().Kind != token.KindColonColon {
				break
			}
			p.advance()
			nt.ResolveFunc += "::"
		}
	}
	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after net type declaration")
	}
	return nt, true
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
	isSpec := p.peek().Text == "specparam"
	p.advance() // "parameter"/"localparam"/"specparam"

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
		param := &ast.Parameter{IsLocal: isLocal, IsSpecparam: isSpec, Type: typ, Name: nameTok.Text}
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
		modportTok := p.peek()
		p.advance() // modport name
		typ.Modport = modportTok.Text
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
