package parser

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/token"
)

// parseTypeAndName parses the shared "[type] name" shape used by ports,
// parameters, and (in a later commit) variable declarations. It's
// genuinely ambiguous which of "input clk" (implicit type, clk is the
// name) and "input logic clk" (explicit type, logic is the type) applies
// without seeing what comes after the first identifier-like token -- so
// this parses greedily assuming an explicit type is present, and if what
// follows isn't actually a name (an identifier), rewinds and
// reinterprets the whole thing as an implicit-typed bare name instead.
func (p *parser) parseTypeAndName() (ast.Type, preprocessor.Token, bool) {
	if t, nameTok, ok := p.netTypeAndName(); ok {
		return t, nameTok, true
	}

	save, saveErrs := p.pos, len(p.errs)
	t := p.parseDataType()

	if nameTok := p.peek(); nameTok.Kind == token.KindIdent {
		p.advance()
		return t, nameTok, true
	}

	// Rewind, and forget any error the speculative parse recorded (an
	// aggregate body can report some; the implicit-type reading will not
	// re-parse it).
	p.pos, p.errs = save, p.errs[:saveErrs]
	nameTok, ok := p.expectIdent()
	return ast.Type{}, nameTok, ok
}

// netTypeAndName parses the "[net_type] data_type name" shape -- LRM
// 23.2.2.3's net_port_type, e.g. "input wire logic [3:0] req". The net
// type itself is consumed and not tracked, the same treatment
// consumeDirection already gives "var" in the identical grammar slot:
// ast.Type describes the data type, and "wire logic x" declares the very
// same data type "logic x" does.
//
// Because a net-type keyword is equally valid AS the type ("wire x",
// "uwire w", "wire [3:0] clk"), this is a probe, not a decision: it
// commits only when swallowing the keyword still leaves a NAMED type
// followed by an identifier (a real type name, not one of
// nonTypeNameQualifiers), and otherwise rewinds both the cursor and the
// error list, leaving parseTypeAndName's own greedy path to parse the
// entry exactly as it did before this existed.
func (p *parser) netTypeAndName() (ast.Type, preprocessor.Token, bool) {
	if !netTypeKeywords[p.peek().Text] {
		return ast.Type{}, preprocessor.Token{}, false
	}
	savePos, saveErrs := p.pos, len(p.errs)
	p.advance() // the net-type keyword

	t := p.parseDataType()
	if t.Name != "" && !nonTypeNameQualifiers[t.Name] {
		if nameTok := p.peek(); nameTok.Kind == token.KindIdent {
			p.advance()
			return t, nameTok, true
		}
	}

	p.pos = savePos
	p.errs = p.errs[:saveErrs]
	return ast.Type{}, preprocessor.Token{}, false
}

// consumeNetTypeQualifier (which reports whether it swallowed the keyword) is netTypeAndName's counterpart for a net
// declaration written as a statement ("wire logic foo;"), where the same
// [net_type] data_type shape is legal. It swallows the net-type keyword
// only when the keyword is qualifying a following data type, not serving
// as the declaration's own type ("uwire w;", "wire [3:0] x;").
//
// Decided by lookahead rather than netTypeAndName's rewind because this
// caller splits declarators only after the type is parsed, so there is no
// single point at which a failed attempt could be backed out: the keyword
// is qualifying something else only if a type-name token follows it and
// that token is itself followed by a declarator's start -- a name, a
// packed dimension, or a "::" package qualifier. nonTypeNameQualifiers are
// excluded despite lexing as type-name tokens, so "wire signed [3:0] x"
// and "tri1 scalared [15:0] a" both keep naming the net type as their
// type, as before.
func (p *parser) consumeNetTypeQualifier() bool {
	if !netTypeKeywords[p.peek().Text] {
		return false
	}
	next := p.peekAt(1)
	if !isTypeNameToken(next) || nonTypeNameQualifiers[next.Text] {
		return false
	}
	switch p.peekAt(2).Kind {
	case token.KindIdent, token.KindLBrack, token.KindColonColon:
		p.advance()
		return true
	}
	return false
}

// parseDataType parses a data type with its packed dimensions: an anonymous
// struct, union, or enum written in place (parseInlineAggregate), or else a
// named type (parseTypeBase). Every site that reads "type [dims]" before a
// declared name uses this, so an inline aggregate works in a variable, port,
// parameter, argument, return type, and struct member alike.
func (p *parser) parseDataType() ast.Type {
	var t ast.Type
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "type" && p.peekAt(1).Kind == token.KindLParen {
		// The type reference operator, "type(a + b)" (LRM 6.23).
		t = ast.Type{Name: "type"}
		t.Position = namePosition(tok)
		t.TypeRef, _ = p.parseTypeRef().(*ast.TypeRef)
	} else if p.atAggregate() {
		t, _ = p.parseInlineAggregate() // on failure the error is recorded and t still names the keyword
	} else {
		t = p.parseTypeBase()
		p.parseClassTypeSuffix(&t)
	}
	t.PackedDims = p.parseDims()
	return t
}

// parseClassTypeSuffix parses the "#( ... )" that follows a user-defined
// class type's name, and the "::name" that may follow that: "cls #(8)" is a
// parameterized class, and "cls #(8)::inner_t" is a type inside one. The
// cursor is after the type name parseTypeBase consumed.
//
// Only an identifier can be a class name. After a keyword type a '#' is
// something else ("wire #(5) w" is a net delay, not a parameter list), so it
// is left for the declaration to deal with as before.
func (p *parser) parseClassTypeSuffix(t *ast.Type) {
	if t.Name == "" || token.Keywords[t.Name] || p.peek().Kind != token.KindHash || p.peekAt(1).Kind != token.KindLParen {
		return
	}
	overrides := p.parseParamOverrideList()
	if !p.absorbClassScope(t, overrides) {
		t.ParamOverrides = overrides
	}
}

// absorbClassScope handles "::name" after a parameterized class's overrides,
// the cursor on the "::". The class becomes the type's qualifier (keeping its
// overrides as QualifierOverrides) and name becomes the type's Name. It
// reports whether a "::" was there to absorb.
func (p *parser) absorbClassScope(t *ast.Type, overrides []ast.ParamOverride) bool {
	if p.peek().Kind != token.KindColonColon {
		return false
	}
	p.advance() // "::"
	nameTok, ok := p.expectIdent()
	if !ok {
		t.ParamOverrides = overrides // keep what was parsed; the error is recorded
		return true
	}
	qualifier := t.Name
	if t.PackageQualifier != "" {
		qualifier = t.PackageQualifier + "::" + t.Name
	}
	t.PackageQualifier = qualifier
	t.QualifierOverrides = overrides
	t.Name = nameTok.Text
	t.Position = namePosition(nameTok)
	return true
}

// parseTypeBase greedily consumes signed/unsigned and one type name
// (builtin keyword or identifier, optionally package/class-qualified via
// "::"), without deciding whether that name turns out to actually be the
// type or (see parseTypeAndName) the declared name itself.
func (p *parser) parseTypeBase() ast.Type {
	t := ast.Type{Position: namePosition(p.peek())}
	t.Signed, t.Unsigned = p.consumeSignedUnsigned()

	nameTok := p.peek()
	if isTypeNameToken(nameTok) {
		p.advance()
		t.Name = nameTok.Text
		if p.peek().Kind == token.KindColonColon {
			p.advance()
			qual := p.peek()
			if isTypeNameToken(qual) {
				p.advance()
				t.PackageQualifier = nameTok.Text
				t.Name = qual.Text
			}
		}
	}

	if s, u := p.consumeSignedUnsigned(); s || u {
		t.Signed, t.Unsigned = s, u
	}
	return t
}

func isTypeNameToken(tok preprocessor.Token) bool {
	return tok.Kind == token.KindIdent || tok.Kind == token.KindKeyword
}

func (p *parser) consumeSignedUnsigned() (signed, unsigned bool) {
	switch p.peek().Text {
	case "signed":
		p.advance()
		return true, false
	case "unsigned":
		p.advance()
		return false, true
	}
	return false, false
}

// parseDims consumes zero or more array/bit-range dimensions: [expr] or
// [expr:expr], repeated. Used for both a type's packed dimensions and a
// declarator's unpacked dimensions -- identical bracket syntax either
// way.
func (p *parser) parseDims() []ast.Dim {
	var dims []ast.Dim
	for p.peek().Kind == token.KindLBrack {
		p.advance() // '['
		left := p.collectUntil(token.KindColon, token.KindRBrack)
		var right []preprocessor.Token
		if p.peek().Kind == token.KindColon {
			p.advance()
			right = p.collectUntil(token.KindRBrack)
		}
		if p.peek().Kind == token.KindRBrack {
			p.advance()
		}
		dims = append(dims, ast.Dim{Left: left, Right: right})
	}
	return dims
}

// collectUntil consumes and returns tokens up to (not including) the
// first one matching any of stopKinds at bracket/paren/brace depth zero,
// or until end of input. Depth-aware so a nested call or index inside an
// expression (e.g. the $clog2(WIDTH) in "[$clog2(WIDTH)-1:0]") doesn't
// trip the stop condition early.
func (p *parser) collectUntil(stopKinds ...token.Kind) []preprocessor.Token {
	depth := 0
	var out []preprocessor.Token
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			return out
		}
		if depth == 0 {
			for _, k := range stopKinds {
				if tok.Kind == k {
					return out
				}
			}
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		}
		out = append(out, tok)
		p.advance()
	}
}

// collectExprUntil is collectUntil, generalized with awareness of
// isDeclBoundaryKeyword -- used only where the tokens being collected are
// a specific declaration's own trailing content (currently just a
// parameter/localparam's default value expression, see
// parseParameterDecl) rather than a bracket-bounded group like
// collectUntil's other callers (ExtendsArgs in classes.go, skipParenGroup,
// a dimension's range here in types.go -- all bounded by a bracket the
// SAME construct just opened, a structurally different and lower-risk
// situation: nothing downstream of that bracket has a plausible read as
// the start of an unrelated declaration). If the terminator collectUntil
// is looking for (comma/';') is missing, it has no way to notice and just
// keeps collecting straight through the next declaration's own tokens,
// silently stealing them; collectExprUntil instead also stops (without
// consuming) at a keyword that plausibly starts a new declaration at
// depth zero, so the caller's own existing "did I actually find my
// terminator" check fires and reports the missing ';' instead of the
// theft going unnoticed. A builtin type keyword immediately followed by
// "'" is excluded (isTypeCastKeyword) -- "int'(x)"-style casts (LRM
// 6.24.1) are ordinary, legal content of a default value expression, not
// a sign the next declaration has begun.
func (p *parser) collectExprUntil(stopKinds ...token.Kind) []preprocessor.Token {
	depth := 0
	var out []preprocessor.Token
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			return out
		}
		if depth == 0 {
			for _, k := range stopKinds {
				if tok.Kind == k {
					return out
				}
			}
			if tok.Kind == token.KindKeyword && isDeclBoundaryKeyword(tok.Text) && !isTypeCastKeyword(tok, p.peekAt(1)) {
				return out
			}
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		}
		out = append(out, tok)
		p.advance()
	}
}

// splitByCommaUntil splits tokens from the cursor into comma-separated
// groups (depth-aware across ( { [ '{ , same as collectUntil) until a
// top-level token of stopKind is consumed, or input runs out. The
// counterpart to preprocessor.SplitBalancedArgs for lists that aren't
// terminated by an unmatched ')' -- a multi-name variable declaration
// ("logic a, b, c;", stopKind Semi) or an enum body ("{A, B, C}",
// stopKind RBrace).
func (p *parser) splitByCommaUntil(stopKind token.Kind) (groups [][]preprocessor.Token, closed bool) {
	return p.splitByComma(stopKind, false)
}

// splitDeclaratorsToSemi is splitByCommaUntil(Semi) with the same
// isDeclBoundaryKeyword awareness collectExprUntil has, for the callers
// whose list is terminated by the declaration's own ';' rather than by a
// bracket the same construct just opened -- a variable/net declaration, a
// virtual interface handle, the declarator half of the
// variable-vs-instantiation split, and a struct/union member.
//
// Without it, a declaration missing its ';' scans straight through the
// NEXT declaration's tokens and swallows them into its own last group:
// "logic" alone on a line above "module top;" consumed "module top ;",
// failed to parse it as a declarator, and deleted the module from the
// result entirely. Stopping (unconsumed) at a decl-boundary keyword makes
// the caller's existing "did I actually find my terminator" check fire and
// report the missing ';' instead, leaving the next declaration for
// parseBody to dispatch on normally. A builtin type keyword immediately
// followed by "'" is exempt (isTypeCastKeyword), same as in
// collectExprUntil: an "int'(x)" cast is ordinary content of an
// initializer, not the next declaration starting.
func (p *parser) splitDeclaratorsToSemi() (groups [][]preprocessor.Token, closed bool) {
	return p.splitByComma(token.KindSemi, true)
}

func (p *parser) splitByComma(stopKind token.Kind, stopAtDeclBoundary bool) (groups [][]preprocessor.Token, closed bool) {
	depth := 0
	var current []preprocessor.Token
	flush := func() { groups = append(groups, current); current = nil }
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			flush()
			return groups, false
		}
		if depth == 0 && tok.Kind == stopKind {
			flush()
			p.advance()
			return groups, true
		}
		if stopAtDeclBoundary && depth == 0 && tok.Kind == token.KindKeyword &&
			isDeclBoundaryKeyword(tok.Text) && !isTypeCastKeyword(tok, p.peekAt(1)) {
			flush()
			return groups, false
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		case token.KindComma:
			if depth == 0 {
				flush()
				p.advance()
				continue
			}
		}
		current = append(current, tok)
		p.advance()
	}
}

// newSubParser returns a parser scoped to toks alone, sharing nothing
// with p -- used to parse an already-comma-isolated group from
// preprocessor.SplitBalancedArgs (a single port, parameter override, or
// port connection) without that group's own bounds/EOF handling
// interacting with the outer token stream. Its errors accumulate
// separately and must be merged back via mergeErrors.
// newSubParser builds a parser over one already-comma-isolated group,
// carrying the parent's current position as a fallback for eofToken.
//
// That fallback is what keeps an EMPTY group attributable. A stray comma
// ("module top(input logic clk, , output logic rst);") produces a group
// with no tokens at all, and a sub-parser over it has nothing of its own
// to point an error at -- so "expected an identifier" used to be recorded
// at File "" line 0 character 0, landing a diagnostic on line 1 of no
// file. In a multi-file session with include attribution that is
// unattributable noise.
func (p *parser) newSubParser(toks []preprocessor.Token) *parser {
	return &parser{toks: toks, fallback: p.peek()}
}

func (p *parser) mergeErrors(sub *parser) {
	p.errs = append(p.errs, sub.errs...)
}

// remainingTokens returns every token left in p (from the cursor to the
// end) and advances the cursor to the end. Meant for a sub-parser scoped
// to one isolated group (see newSubParser): once a fixed prefix like
// "= " has been consumed, whatever's left in the group is unambiguously
// a raw expression span with no further delimiter to look for.
func (p *parser) remainingTokens() []preprocessor.Token {
	rest := p.toks[p.pos:]
	p.pos = len(p.toks)
	return rest
}
