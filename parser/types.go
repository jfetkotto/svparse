package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
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
	save := p.pos
	t := p.parseTypeBase()
	t.PackedDims = p.parseDims()

	if nameTok := p.peek(); nameTok.Kind == token.KindIdent {
		p.advance()
		return t, nameTok, true
	}

	p.pos = save
	nameTok, ok := p.expectIdent()
	return ast.Type{}, nameTok, ok
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
func newSubParser(toks []preprocessor.Token) *parser {
	return &parser{toks: toks}
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
