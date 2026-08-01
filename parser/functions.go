package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// parseFunction parses a function declaration: [lifetime] [return-type]
// name(args); ... endfunction. isPrototype tells it not to expect a body
// at all -- extern and DPI-import functions are grammatically distinct
// declarations with no possible body, not detected by "is there a ';'
// right after the argument list", which is present either way (an
// ordinary function's ANSI-style header ends in ';' too, before its
// real body starts; only the caller, having seen "extern" or a DPI
// import string literal, actually knows which case this is). Return
// type follows the exact same implicit-vs-explicit ambiguity ports/
// parameters/variables do ("function foo(...)" vs "function int
// foo(...)"), resolved by the same parseTypeAndName -- except for the
// class constructor, "function new(...);", where "new" is SV's one
// genuinely dual-role keyword: reserved everywhere else (the new
// operator), but the mandatory, typeless constructor name here.
// parseTypeAndName's final step requires a plain identifier and would
// reject "new" outright (it's lexed as a keyword, not an identifier),
// so that case is special-cased before delegating to it.
func (p *parser) parseFunction(isPrototype bool) (ast.Decl, bool) {
	p.advance() // "function"
	if p.peek().Text == "automatic" || p.peek().Text == "static" {
		p.advance() // lifetime qualifier -- consumed, not tracked
	}

	var returnType ast.Type
	var nameTok preprocessor.Token
	var ok bool
	if p.peek().Kind == token.KindKeyword && p.peek().Text == "new" {
		nameTok, ok = p.advance(), true
	} else {
		returnType, nameTok, ok = p.parseTypeAndName()
	}
	if !ok {
		return nil, false
	}
	nameTok = p.resolveQualifiedName(nameTok)
	args := p.parseArgList()

	fn := &ast.Function{Name: nameTok.Text, ReturnType: returnType, Args: args, Prototype: isPrototype}
	fn.Position = namePosition(nameTok)

	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after function signature")
	}

	if isPrototype {
		fn.EndLine, fn.EndCharacter = fn.Line, fn.Character
		return fn, true
	}

	end := p.skipBody("endfunction")
	fn.EndLine, fn.EndCharacter = end.Line, end.Character
	if end.Kind != token.KindEOF {
		p.advance() // "endfunction"
		p.skipEndLabel()
	}
	return fn, true
}

// parseTask mirrors parseFunction, minus a return type -- tasks never
// have one.
func (p *parser) parseTask(isPrototype bool) (ast.Decl, bool) {
	p.advance() // "task"
	if p.peek().Text == "automatic" || p.peek().Text == "static" {
		p.advance()
	}

	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	nameTok = p.resolveQualifiedName(nameTok)
	args := p.parseArgList()

	task := &ast.Task{Name: nameTok.Text, Args: args, Prototype: isPrototype}
	task.Position = namePosition(nameTok)

	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after task signature")
	}

	if isPrototype {
		task.EndLine, task.EndCharacter = task.Line, task.Character
		return task, true
	}

	end := p.skipBody("endtask")
	task.EndLine, task.EndCharacter = end.Line, end.Character
	if end.Kind != token.KindEOF {
		p.advance() // "endtask"
		p.skipEndLabel()
	}
	return task, true
}

// resolveQualifiedName checks for a "::" immediately after a
// function/task's parsed name -- an out-of-class method body matching an
// extern prototype declared inside a class, e.g. "function void
// foo::bar(); ... endfunction" (mirrors nols's own internal/sv/scan.go,
// which already handles this exact pattern for its own goto-definition/
// declaration support). If present, consumes the "::" and the real name
// that follows, returning that as the effective name. The class
// qualifier itself isn't retained -- ast.Function/Task have no field for
// it; unifying an out-of-class body back with its class's extern
// prototype is a future phase's job, not this one's.
func (p *parser) resolveQualifiedName(nameTok preprocessor.Token) preprocessor.Token {
	if p.peek().Kind != token.KindColonColon {
		return nameTok
	}
	p.advance() // "::"
	if realName, ok := p.expectIdent(); ok {
		return realName
	}
	return nameTok
}

// parseDPIImport parses "import <string-literal> [context|pure]
// function/task ...;" -- the cursor is expected to be at "import", with
// the following token already confirmed to be a string literal (see the
// parseDecl dispatch). Always a prototype -- DPI imports never have a
// body. The optional "c_identifier =" foreign-name-mapping form isn't
// handled -- a rare feature; encountering one produces a recorded error
// via the fallback below rather than being silently misparsed.
func (p *parser) parseDPIImport() (ast.Decl, bool) {
	p.advance() // "import"
	p.advance() // the DPI spec string literal, e.g. "DPI-C"
	if p.peek().Text == "context" || p.peek().Text == "pure" {
		p.advance() // DPI qualifier -- consumed, not tracked
	}
	switch p.peek().Text {
	case "function":
		return p.parseFunction(true)
	case "task":
		return p.parseTask(true)
	}
	p.errorf(p.peek(), "expected function or task after DPI import")
	return nil, false
}

// parseArgList parses a function/task's argument list, identical in
// shape and splitting strategy to parsePortEntry's port list (ports.go)
// -- SplitBalancedArgs first, then each already-isolated group parsed
// independently.
func (p *parser) parseArgList() []ast.Arg {
	if p.peek().Kind != token.KindLParen {
		return nil
	}
	p.advance() // '('

	groups, newPos, closed := preprocessor.SplitBalancedArgs(p.toks, p.pos)
	p.pos = newPos
	if !closed {
		p.errorf(p.peek(), "unterminated argument list")
	}
	if isEmptyGroups(groups) {
		return nil
	}

	var args []ast.Arg
	for _, group := range groups {
		if arg, ok := p.parseArgEntry(group); ok {
			args = append(args, arg)
		}
	}
	return args
}

func (p *parser) parseArgEntry(group []preprocessor.Token) (ast.Arg, bool) {
	sub := newSubParser(group)
	dir := sub.consumeDirection()

	typ, nameTok, ok := sub.parseTypeAndName()
	if !ok {
		p.mergeErrors(sub)
		return ast.Arg{}, false
	}

	arg := ast.Arg{Direction: dir, Type: typ, Name: nameTok.Text}
	arg.Position = namePosition(nameTok)
	arg.UnpackedDims = sub.parseDims()
	if sub.peek().Kind == token.KindAssign {
		sub.advance()
		arg.Default = sub.remainingTokens()
	} else if sub.pos < len(sub.toks) {
		// Same trailing-garbage backstop as parsePortEntry/
		// parseVariableDeclarator -- anything left unconsumed here isn't
		// a '[' or '=', so it's malformed and would otherwise vanish
		// silently.
		p.errorf(sub.peek(), "unexpected trailing tokens after argument %q", nameTok.Text)
	}
	p.mergeErrors(sub)
	return arg, true
}

// blockOpenKeywords/blockCloseKeywords are the block-delimiter keyword
// pairs that can appear inside a function/task body -- tracked via one
// combined depth counter (mirroring the preprocessor's own combined-
// depth simplification in SplitBalancedArgs) since skipBody only needs
// to find the matching endfunction/endtask, never to validate which
// specific pair opened or closed. "randcase" closes via "endcase" the
// same as "case"/"casex"/"casez" (and, like them, has no block label of
// its own to skip).
var blockOpenKeywords = map[string]bool{"begin": true, "fork": true, "case": true, "casex": true, "casez": true, "randcase": true}
var blockCloseKeywords = map[string]bool{"end": true, "join": true, "join_any": true, "join_none": true, "endcase": true}

// isWaitOrDisableFork reports whether tok ("wait"/"disable") is
// immediately followed by "fork" -- the standalone "wait fork;"/
// "disable fork;" statements (LRM 9.6.1/9.6.2), which have nothing to do
// with the fork/join block-delimiter pair skipBody/skipProceduralConstruct
// otherwise track via blockOpenKeywords/blockCloseKeywords. Neither of
// those maps, which key purely off a token's own text, can tell this
// "fork" apart from a real block-opening one -- doing so needs the
// PRECEDING token too. Without this check, "wait fork;"/"disable fork;"
// increments depth for a "fork" with no matching "join" ever coming,
// running the scan to end of file and silently dropping every
// declaration after it.
func isWaitOrDisableFork(text string, next preprocessor.Token) bool {
	return (text == "wait" || text == "disable") && next.Kind == token.KindKeyword && next.Text == "fork"
}

// skipBody consumes a function/task's body -- never parsed, since
// statement/expression grammar is entirely out of scope for this parser
// -- stopping at endKeyword ("endfunction"/"endtask") at depth zero,
// left unconsumed for the caller.
func (p *parser) skipBody(endKeyword string) preprocessor.Token {
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected %s", endKeyword)
			return tok
		}
		if depth == 0 && tok.Kind == token.KindKeyword && tok.Text == endKeyword {
			return tok
		}
		if tok.Kind == token.KindKeyword {
			if isWaitOrDisableFork(tok.Text, p.peekAt(1)) {
				p.advance() // "wait"/"disable"
				p.advance() // "fork"
				continue
			}
			switch {
			case blockOpenKeywords[tok.Text]:
				depth++
			case blockCloseKeywords[tok.Text]:
				if depth > 0 {
					depth--
				}
			}
		}
		p.advance()
	}
}

// skipProceduralConstruct consumes one `initial`/`always`/`always_comb`/
// `always_ff`/`always_latch`/`final` construct's statement (the keyword
// itself already consumed by the caller) -- never parsed, same reasoning
// as skipBody, but with no single terminating end-keyword the way a
// function/task body has: it ends at a ';' at depth zero (a single
// statement, e.g. "initial $display(...);") or at the matching close of
// an opening begin/fork/case (a block, e.g. "initial begin ... end"),
// tracking both `( { [ nesting (so a for-loop header's own ';'s, or a
// call's argument list, don't look like the statement's terminator) and
// block-keyword depth (reusing blockOpen/blockCloseKeywords) in the same
// pass. Once a statement/block at depth zero completes, continueAfterStatement
// checks for a trailing "while (cond);" (a do-while's own terminator,
// LRM 12.7.2 -- the do-while's body, a single statement or begin/end
// block, is otherwise indistinguishable from any other statement/block at
// the point this reaches depth zero again) and/or a following "else" (an
// if/else chain's next branch), continuing to skip those as part of the
// same construct rather than returning and leaving them dangling at
// container scope, where they'd otherwise misparse as their own bad
// declaration.
func (p *parser) skipProceduralConstruct() {
	blockDepth := 0
	parenDepth := 0
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			return
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrack, token.KindLBrace:
			parenDepth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if parenDepth > 0 {
				parenDepth--
			}
		case token.KindKeyword:
			if isWaitOrDisableFork(tok.Text, p.peekAt(1)) {
				p.advance() // "wait"/"disable"
				p.advance() // "fork"
				continue
			}
			if blockOpenKeywords[tok.Text] {
				blockDepth++
				p.advance()
				if tok.Text == "begin" || tok.Text == "fork" {
					// "begin : label" / "fork : label" -- a named block
					// (LRM 9.3.1), the same "block identifiers" feature as
					// skipEndLabel handles for containers/functions/tasks,
					// just introduced BEFORE the block's contents here
					// instead of after. "case"/"casex"/"casez"/"randcase"
					// have no such label.
					p.skipEndLabel()
				}
				continue
			} else if blockCloseKeywords[tok.Text] {
				if blockDepth > 0 {
					blockDepth--
					p.advance()
					if tok.Text != "endcase" {
						// "end : label" / "join[_any|_none] : label" --
						// closing half of the same named-block feature;
						// "endcase" has no label to match.
						p.skipEndLabel()
					}
					if blockDepth == 0 && parenDepth == 0 {
						if p.continueAfterStatement() {
							continue
						}
						return
					}
					continue
				}
			}
		case token.KindSemi:
			if blockDepth == 0 && parenDepth == 0 {
				p.advance()
				if p.continueAfterStatement() {
					continue
				}
				return
			}
		}
		p.advance()
	}
}

// continueAfterStatement is called by skipProceduralConstruct once a
// statement or block at depth zero has just completed. It first consumes
// a do-while's trailing "while (cond);" clause if present (the true
// terminator of the whole do-while -- not a new statement of its own, so
// this always happens regardless of the return value below), then
// reports whether the caller should keep scanning rather than return:
// true iff an immediately following "else" is present (an if/else
// chain's next branch, consumed here so the chain's remaining branch is
// skipped as part of the same construct -- looping naturally handles an
// "else if ... else ..." chain of any length, since each "else"/"if" this
// consumes just leaves the next branch to be discovered the same way).
// A bare "while (cond);" with no following "else" means the whole
// construct is now fully consumed, so it must NOT itself signal "keep
// scanning" -- doing so would resume the generic per-token loop with
// nothing left to bound it, consuming whatever unrelated declaration
// follows.
func (p *parser) continueAfterStatement() bool {
	if p.peek().Kind == token.KindKeyword && p.peek().Text == "while" {
		p.advance() // "while"
		p.skipParenGroup()
		if p.peek().Kind == token.KindSemi {
			p.advance()
		}
	}
	if p.peek().Kind == token.KindKeyword && p.peek().Text == "else" {
		p.advance() // "else"
		return true
	}
	return false
}
