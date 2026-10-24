// Package parser implements an error-tolerant, declaration-grade
// recursive-descent parser for SystemVerilog, producing an svparse/ast
// tree from a token stream. Error-tolerant means what it does throughout
// svparse: a malformed declaration is recorded and skipped (see
// recover.go), never fatal -- a language server has to make sense of
// source that's broken mid-edit far more often than a batch tool does.
//
// Declaration-grade means ports, nets/variables, parameters, typedef/
// struct/enum, function/task signatures, class members, package
// imports, containers, and instantiations are parsed for real;
// procedural block bodies (inside always, function/task bodies,
// constraint blocks) are recognized and skipped via balanced-token
// matching, never parsed -- expression and statement grammar is
// entirely out of scope. See each unparsed field's doc comment in
// package ast for exactly what's kept as a raw token span instead.
package parser

import (
	"fmt"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// Error is a non-fatal problem noticed while parsing. Parse never stops
// or panics because of one.
type Error struct {
	File      string
	Line      int
	Character int
	Message   string
}

type parser struct {
	toks []preprocessor.Token
	pos  int
	errs []Error

	// fallback positions eofToken when toks is empty -- see newSubParser.
	fallback preprocessor.Token

	// exprDepth is the current expression-recursion depth, bounded by
	// maxExprDepth -- see expr.go.
	exprDepth int

	// stmtDepth is the current statement nesting depth, bounded by
	// maxStmtDepth -- see stmt.go.
	stmtDepth int

	// genDepth is the current generate nesting depth, bounded by
	// maxGenDepth -- see generate.go.
	genDepth int

	// inProp is positive while a property or sequence expression is being
	// parsed, which changes how a parenthesised group is read -- see sva.go.
	inProp int

	// suppressed holds errors found while parsing a construct for its
	// structure only: the body of a function or task, a procedural item,
	// a generate header and so on, whose extent the declaration parser
	// finds without looking inside. They are kept apart so that scan is
	// unchanged, and Parse reports them after the declaration errors
	// (see reported).
	suppressed []Error

	// zeroWidth* remember the most recent zero-width expression node (a
	// placeholder for something missing) so enclosing spans can widen to
	// contain it -- see spanFrom.
	zeroWidthSet                bool
	zeroWidthAt                 int // token index it was created at
	zeroWidthLine, zeroWidthCol int
}

// Parse parses toks. preprocessor.Token is the canonical input -- not
// token.Token -- so AST node positions can carry real file attribution
// (an included declaration's true file) without the parser needing to
// know anything about how the tokens got there.
//
// toks need not end with a token.KindEOF sentinel: lexer.Lex's own
// output always does, but preprocessor.Preprocess's does not (its EOF
// sentinels are internal boundary markers between pushed sources --
// files, macro expansions, includes -- stripped from the merged output
// by design, not a producer bug). peekAt synthesizes an EOF token past
// the end of whatever toks it's given rather than trusting an invariant
// that only sometimes holds -- past a genuine end of input should
// always look the same to the parser regardless of which producer built
// toks.
func Parse(path string, toks []preprocessor.Token) (*ast.File, []Error) {
	p := &parser{toks: toks}
	f := &ast.File{Path: path}
	f.Decls, _, _ = p.parseBody("")
	ownTypes(f)
	fillExprs(f)
	return f, p.reported()
}

// reported returns the parse errors: the declaration errors, then those found inside bodies, procedural items and other constructs parsed
// for their structure (LRM Annex A.6, A.8). A body parsed twice, or an error
// seen by both the extent scan and the body parser, is reported once.
func (p *parser) reported() []Error {
	if len(p.suppressed) == 0 {
		return p.errs
	}
	all := make([]Error, 0, len(p.errs)+len(p.suppressed))
	seen := make(map[Error]bool, cap(all))
	for _, e := range append(append(all, p.errs...), p.suppressed...) {
		if !seen[e] {
			seen[e] = true
			all = append(all, e)
		}
	}
	return all
}

// ParseTokens is a convenience entry point for callers that skip
// preprocessing entirely: it wraps each token with File set and no
// macro attribution, then delegates to Parse.
func ParseTokens(path string, toks []token.Token) (*ast.File, []Error) {
	pptoks := make([]preprocessor.Token, len(toks))
	for i, t := range toks {
		pptoks[i] = preprocessor.Token{Token: t, File: path}
	}
	return Parse(path, pptoks)
}

// peek returns the current token without consuming it.
func (p *parser) peek() preprocessor.Token {
	return p.peekAt(0)
}

// peekAt returns the token offset positions ahead of the cursor without
// consuming anything. Past the end of toks (however that end is
// reached -- toks may or may not itself end with a KindEOF token, see
// Parse's doc comment), it always returns a synthetic KindEOF rather
// than clamping to whatever the last real token happens to be: clamping
// would let a caller mistake "ran off the end" for "found another real
// token here," which previously caused an infinite loop when
// preprocessor.Preprocess's EOF-less output left the cursor parked on a
// real keyword token forever instead of ever reaching a recognizable
// end of input.
func (p *parser) peekAt(offset int) preprocessor.Token {
	idx := p.pos + offset
	if idx < 0 || idx >= len(p.toks) {
		return p.eofToken()
	}
	return p.toks[idx]
}

// eofToken synthesizes an end-of-input marker positioned just after the
// last real token in toks (so an "unexpected end of input" error points
// somewhere sensible), falling back to the position a sub-parser was
// created at when toks is empty -- see newSubParser.
func (p *parser) eofToken() preprocessor.Token {
	if len(p.toks) == 0 {
		return preprocessor.Token{
			Token: token.Token{Kind: token.KindEOF, Line: p.fallback.Line, Character: p.fallback.Character},
			File:  p.fallback.File,
		}
	}
	last := p.toks[len(p.toks)-1]
	endLine, endChar := last.End()
	return preprocessor.Token{
		Token: token.Token{Kind: token.KindEOF, Line: endLine, Character: endChar},
		File:  last.File,
	}
}

// advance consumes and returns the current token. It never advances past
// EOF.
func (p *parser) advance() preprocessor.Token {
	tok := p.peek()
	if tok.Kind != token.KindEOF {
		p.pos++
	}
	return tok
}

// expectIdent consumes the current token if it's an identifier, else
// records an error and leaves the cursor where it is (so the caller's
// error-recovery path can decide what to skip).
func (p *parser) expectIdent() (preprocessor.Token, bool) {
	tok := p.peek()
	if tok.Kind != token.KindIdent {
		p.errorf(tok, "expected an identifier, found %q", tok.Text)
		return tok, false
	}
	p.advance()
	return tok, true
}

func (p *parser) errorf(tok preprocessor.Token, format string, args ...any) {
	p.errs = append(p.errs, Error{File: tok.File, Line: tok.Line, Character: tok.Character, Message: fmt.Sprintf(format, args...)})
}

// namePosition returns the ast.Position a declaration's Position field
// should start with, given the token that names it -- spanning the name
// itself, so a declaration with no body to span still has a usable range.
//
// The End half is set here rather than at each call site because leaving
// it to the caller did not work: only the seven declarations that go on to
// span a real body (container, package, class, function, task, constraint)
// ever assigned one, so every OTHER declaration -- variable, port,
// parameter, typedef, enum member, import, instantiation -- carried
// End{0,0} against a nonzero start, i.e. a BACKWARDS range rather than
// merely an empty one. Every half-open containment test a consumer builds
// on Position/End fails for those, and an LSP documentSymbol whose
// range.end precedes its range.start is protocol-invalid. Declarations
// that do span a body simply overwrite what this sets.
func namePosition(nameTok preprocessor.Token) ast.Position {
	endLine, endCharacter := endOfName(nameTok)
	return ast.Position{
		File: nameTok.File, Line: nameTok.Line, Character: nameTok.Character,
		EndLine: endLine, EndCharacter: endCharacter,
	}
}

// parenGroupEnd returns the index of the ')' that closes the parenthesized
// group the cursor is inside (the cursor is just past its '('), or, if that
// ')' is missing, the index of the first ';' or the end of input.
//
// In a declaration-level list (a parameter value list, a constructor argument
// list) a ';' can never appear inside the parentheses, so meeting one means the
// ')' is missing, and the list must stop there instead of running on through
// the declarations that follow. The scan only bounds; it consumes nothing.
func (p *parser) parenGroupEnd() int {
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		switch p.toks[i].Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindRParen:
			if depth == 0 {
				return i
			}
			depth--
		case token.KindSemi:
			return i
		}
	}
	return len(p.toks)
}

// parenGroupLimit is the exclusive upper bound of the tokens a parenthesized
// list may use: through its closing ')', or up to but NOT including the ';' or
// end of input that stands in for a missing one, so the terminator is left
// for the enclosing construct to consume.
func (p *parser) parenGroupLimit() int {
	end := p.parenGroupEnd()
	if end < len(p.toks) && p.toks[end].Kind == token.KindRParen {
		return end + 1
	}
	return end
}

// tokensSpan is the span from the start of toks[0] through the end of the last
// token, for a node that covers a run of tokens rather than a single name (a
// positional override or connection, a wildcard). End is clamped so it never
// precedes Start (tokens from a macro expansion can be positioned out of order
// relative to their invocation). toks must not be empty.
func tokensSpan(toks []preprocessor.Token) ast.Position {
	first := toks[0]
	pos := ast.Position{File: first.File, Line: first.Line, Character: first.Character}
	pos.EndLine, pos.EndCharacter = toks[len(toks)-1].End()
	if pos.EndLine < pos.Line || (pos.EndLine == pos.Line && pos.EndCharacter < pos.Character) {
		pos.EndLine, pos.EndCharacter = pos.Line, pos.Character
	}
	return pos
}

// endOfName returns the End position for a declaration that has no body to
// span: just past the last character of its own name. Applied to every
// declaration by namePosition, and re-applied explicitly by a prototype
// (extern / pure virtual / DPI import function or task, an extern or pure
// constraint), which takes the body-spanning path but has no body.
//
// Deliberately name-WIDTH, not zero-width. A zero-width End makes the
// declaration's span contain nothing at all, including the name's own
// first column, which breaks every half-open "is this position inside that
// declaration" test a consumer builds on Position/End -- sigils' hover and
// goto-definition on a file-scope DPI import used to find nothing for
// exactly this reason. It also makes an LSP documentSymbol's selectionRange
// (the name) fall outside its range (the declaration), which the protocol
// forbids. Matching the convention every other name-only declaration here
// already follows (a typedef, an enum member, a variable) costs nothing and
// avoids both.
func endOfName(nameTok preprocessor.Token) (endLine, endCharacter int) {
	return nameTok.Line, nameTok.Character + token.UTF16Len(nameTok.Text)
}
