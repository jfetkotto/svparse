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
	return f, p.errs
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
// somewhere sensible), or the zero position if toks is empty.
func (p *parser) eofToken() preprocessor.Token {
	if len(p.toks) == 0 {
		return preprocessor.Token{Token: token.Token{Kind: token.KindEOF}}
	}
	last := p.toks[len(p.toks)-1]
	return preprocessor.Token{
		Token: token.Token{Kind: token.KindEOF, Line: last.Line, Character: last.Character + token.UTF16Len(last.Text)},
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
// should start with, given the token that names it.
func namePosition(nameTok preprocessor.Token) ast.Position {
	return ast.Position{File: nameTok.File, Line: nameTok.Line, Character: nameTok.Character}
}

// endOfName returns the End position for a declaration that has no body to
// span -- a prototype (extern / pure virtual / DPI import function or task,
// an extern or pure constraint): just past the last character of its own
// name.
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
