package preprocessor

import (
	"strings"

	"github.com/jfetkotto/svparse/token"
)

// macroDef is a stored define. params/isFunctionLike distinguish a
// function-like macro (define FOO(a,b) ...) from an object-like one
// (define WIDTH 8) -- object-like macros never populate params.
// body is every token of the replacement list exactly as written, still
// carrying whatever File/MacroName/InvokedFrom it already had from being
// read off the definition site (ordinarily just File set, since a
// define is read from a plain file/include source, not from inside
// another expansion) -- substitution and self-reference handling both
// happen at expansion time, not here.
type macroDef struct {
	name           string
	isFunctionLike bool
	params         []macroParam
	body           []Token
	defFile        string
}

// macroParam is one formal parameter of a function-like macro. SV allows
// a default value (define FOO(a, b=1) ...), used when an invocation
// omits a trailing argument.
type macroParam struct {
	name        string
	hasDefault  bool
	defaultToks []Token
}

// handleDefine consumes a define directive: the macro name; then, only
// if a '(' immediately follows the name with no gap (see isAdjacent --
// the LRM's own rule for telling a function-like macro's parameter list
// apart from an object-like macro whose body simply starts with a
// literal "("), its parameter list; then its replacement-list body,
// which is every token on the same logical line, extended across a
// token.KindLineContinuation.
func (p *preprocessor) handleDefine(directiveTok Token, src *tokenSource) {
	nameTok, ok := p.next(src)
	if !ok || nameTok.Kind != token.KindIdent {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "define not followed by a macro name")
		return
	}

	var params []macroParam
	isFunctionLike := false
	bodyStartLine := nameTok.Line
	if next, ok := p.peek(src); ok && next.Kind == token.KindLParen && isAdjacent(nameTok, next) {
		isFunctionLike = true
		src.pos++ // consume '('
		params, ok = p.parseMacroParams(directiveTok, src)
		if !ok {
			return // error already recorded
		}
		if src.pos > 0 {
			bodyStartLine = src.toks[src.pos-1].Line // the line the closing ')' ended up on
		}
	}

	body := p.collectLogicalLine(src, bodyStartLine)
	p.macros[nameTok.Text] = &macroDef{
		name: nameTok.Text, isFunctionLike: isFunctionLike, params: params,
		body: body, defFile: directiveTok.File,
	}
}

// isAdjacent reports whether b starts exactly where a's text ends, with
// no gap -- derivable purely from position data, without the lexer
// needing to preserve whitespace as tokens.
func isAdjacent(a, b Token) bool {
	return a.Line == b.Line && b.Character == a.Character+token.UTF16Len(a.Text)
}

// parseMacroParams parses a define's parameter list, the caller having
// already consumed the opening '('. Each comma-separated group (see
// splitBalancedArgs) is either a bare parameter name or "name = default
// tokens".
func (p *preprocessor) parseMacroParams(directiveTok Token, src *tokenSource) ([]macroParam, bool) {
	groups, closed := p.splitBalancedArgs(src)
	if !closed {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "define parameter list missing closing ')'")
		return nil, false
	}
	if isEmptyParenGroups(groups) {
		return nil, true // FOO() -- zero parameters, not one blank one
	}

	params := make([]macroParam, 0, len(groups))
	for _, g := range groups {
		param, ok := parseParamGroup(g)
		if !ok {
			p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "malformed macro parameter")
			continue
		}
		params = append(params, param)
	}
	return params, true
}

func parseParamGroup(group []Token) (macroParam, bool) {
	for i, tok := range group {
		if tok.Kind != token.KindAssign {
			continue
		}
		if i != 1 || group[0].Kind != token.KindIdent {
			return macroParam{}, false
		}
		return macroParam{name: group[0].Text, hasDefault: true, defaultToks: group[i+1:]}, true
	}
	if len(group) == 1 && group[0].Kind == token.KindIdent {
		return macroParam{name: group[0].Text}, true
	}
	return macroParam{}, false
}

// collectLogicalLine consumes tokens from src while they remain on
// startLine, extending across any token.KindLineContinuation (dropped,
// not included in the result) -- used for a define's replacement list,
// which ends at the first real (non-continued) line break, or at end of
// input.
func (p *preprocessor) collectLogicalLine(src *tokenSource, startLine int) []Token {
	currentLine := startLine
	var out []Token
	for src.pos < len(src.toks) {
		tok := src.toks[src.pos]
		if tok.Kind == token.KindEOF {
			break
		}
		if tok.Kind == token.KindLineContinuation {
			src.pos++
			currentLine++
			continue
		}
		if tok.Line != currentLine {
			break
		}
		out = append(out, tok)
		src.pos++
	}
	return out
}

func (p *preprocessor) handleUndef(directiveTok Token, src *tokenSource) {
	nameTok, ok := p.next(src)
	if !ok || nameTok.Kind != token.KindIdent {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "undef not followed by a macro name")
		return
	}
	delete(p.macros, nameTok.Text)
}

// expandMacro expands one macro invocation: for a function-like macro,
// parses the invocation's argument list first (bailing, with the
// reference left literal, if no arguments follow at all -- a malformed
// invocation); pushes the substituted body as a new token source, so the
// same main loop that dispatched this invocation processes the expansion
// too, including expanding any macro references left unexpanded inside a
// substituted argument (SV has no stringize/paste operators that would
// need an argument's literal, unexpanded text, so this deferred/lazy
// expansion -- rather than expanding arguments up front -- is simpler and
// behaviorally equivalent). The self-reference guard (p.expanding) is
// armed here and disarmed via the pushed source's onPop, once every token
// of this expansion has been consumed.
func (p *preprocessor) expandMacro(def *macroDef, invocationTok Token, src *tokenSource) {
	invocation := invocationTok // stable copy: &invocation outlives this call

	var argGroups [][]Token
	if def.isFunctionLike {
		next, ok := p.peek(src)
		if !ok || next.Kind != token.KindLParen {
			p.errorf(invocationTok.File, invocationTok.Line, invocationTok.Character,
				"function-like macro %q referenced without arguments", def.name)
			p.out = append(p.out, invocationTok)
			return
		}
		src.pos++ // consume '('
		groups, closed := p.splitBalancedArgs(src)
		if !closed {
			p.errorf(invocationTok.File, invocationTok.Line, invocationTok.Character,
				"unterminated argument list for macro %q", def.name)
			return
		}
		if !(isEmptyParenGroups(groups) && len(def.params) == 0) {
			argGroups = groups
		}
	}

	p.expanding[def.name] = true
	p.stack = append(p.stack, &tokenSource{
		toks:  p.substitute(def, argGroups, &invocation),
		onPop: func() { delete(p.expanding, def.name) },
	})
}

// substitute builds a macro's expansion: a body token matching a
// parameter name is replaced by that argument's token slice verbatim
// (preserving whatever File/MacroName/InvokedFrom those argument tokens
// already carry -- correct for nested expansion, since an argument's
// true origin doesn't change by being substituted elsewhere); every
// other body token is tagged with this invocation. Once every parameter
// is substituted, mergeTokenPaste and mergeStringize resolve any paste
// (two adjacent backticks) or stringize (“ `" “) operator the body
// contained -- after substitution, not before, so pasting a and b uses
// their ACTUAL arguments, and “ `"x`" “ stringizes x's actual argument
// text, matching the LRM's "arguments are substituted before
// paste/stringize are evaluated" order.
func (p *preprocessor) substitute(def *macroDef, argGroups [][]Token, invocation *Token) []Token {
	var expansion []Token
	for _, bodyTok := range def.body {
		if def.isFunctionLike && bodyTok.Kind == token.KindIdent {
			if argToks, isParam := p.argFor(def, argGroups, bodyTok.Text, invocation); isParam {
				expansion = append(expansion, argToks...)
				continue
			}
		}
		t := bodyTok
		t.MacroName = def.name
		t.InvokedFrom = invocation
		expansion = append(expansion, t)
	}
	return mergeStringize(mergeTokenPaste(expansion))
}

// mergeTokenPaste resolves every paste (two adjacent backticks,
// KindPaste) operator in toks: its
// immediate left and right neighbors (if any -- a paste at either end of
// the body, with nothing on that side, is simply dropped, per the LRM
// leaving it a no-op there) are merged into one token, spelled as the
// concatenation of their Text, positioned at the left neighbor's
// position (or the right neighbor's, if there is no left one). Repeats
// until no KindPaste tokens remain, so a chain of three pasted
// identifiers resolves left to right in successive passes ("a"+"b" ->
// "ab", then "ab"+"c" -> "abc") rather than needing special-case
// handling for more than one paste in a row.
func mergeTokenPaste(toks []Token) []Token {
	for {
		idx := -1
		for i, t := range toks {
			if t.Kind == token.KindPaste {
				idx = i
				break
			}
		}
		if idx == -1 {
			return toks
		}

		haveLeft := idx > 0
		haveRight := idx+1 < len(toks)

		var merged []Token
		merged = append(merged, toks[:idx]...)
		if haveLeft {
			merged = merged[:len(merged)-1] // the left neighbor is folded into the merged token below, not kept separately
		}

		if haveLeft || haveRight {
			var base Token
			leftText, rightText := "", ""
			if haveLeft {
				base = toks[idx-1]
				leftText = toks[idx-1].Text
			}
			if haveRight {
				rightText = toks[idx+1].Text
				if !haveLeft {
					base = toks[idx+1] // no left neighbor -- fall back to the right one for position/kind
				}
			}
			pasted := base
			pasted.Text = leftText + rightText
			if looksLikeIdentifier(pasted.Text) {
				pasted.Kind = token.KindIdent
			}
			merged = append(merged, pasted)
		}

		rest := idx + 1
		if haveRight {
			rest = idx + 2
		}
		merged = append(merged, toks[rest:]...)
		toks = merged
	}
}

// mergeStringize resolves every matched “ `" “ (KindMacroQuote) pair in
// toks into a single KindStringLiteral token positioned at the opening
// quote, whose text is a double-quoted, space-joined rendering of
// whatever tokens fell between the pair (an approximation, not exact
// whitespace fidelity -- the same simplification joinTokenText/formatType
// already make elsewhere for a re-rendered token span). An unmatched
// trailing KindMacroQuote (no closing partner before the body ends) is
// treated as closing at end of input, same tolerant spirit as the rest of
// this package.
func mergeStringize(toks []Token) []Token {
	var out []Token
	for i := 0; i < len(toks); i++ {
		if toks[i].Kind != token.KindMacroQuote {
			out = append(out, toks[i])
			continue
		}

		open := toks[i]
		j := i + 1
		var inner []Token
		for j < len(toks) && toks[j].Kind != token.KindMacroQuote {
			inner = append(inner, toks[j])
			j++
		}

		var text strings.Builder
		text.WriteByte('"')
		for k, t := range inner {
			if k > 0 {
				text.WriteByte(' ')
			}
			text.WriteString(t.Text)
		}
		text.WriteByte('"')

		str := open
		str.Kind = token.KindStringLiteral
		str.Text = text.String()
		out = append(out, str)

		i = j // if toks[j] is the closing KindMacroQuote, the loop's own i++ skips past it; if j reached the end, this just ends the loop
	}
	return out
}

// looksLikeIdentifier reports whether s is spelled like a plain SV
// identifier (LRM Annex A's [a-zA-Z_][a-zA-Z0-9_$]*) -- used to decide
// whether a pasted token should become KindIdent (the overwhelmingly
// common case, e.g. pasting two identifier fragments into a longer name)
// or keep its left operand's original kind (e.g. pasting onto a number,
// which this package doesn't attempt to re-lex into a new numeric
// literal).
func looksLikeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case i > 0 && (r >= '0' && r <= '9' || r == '$'):
		default:
			return false
		}
	}
	return true
}

func (p *preprocessor) argFor(def *macroDef, argGroups [][]Token, paramName string, invocation *Token) ([]Token, bool) {
	for i, param := range def.params {
		if param.name != paramName {
			continue
		}
		if i < len(argGroups) {
			return argGroups[i], true
		}
		if param.hasDefault {
			return param.defaultToks, true
		}
		p.errorf(invocation.File, invocation.Line, invocation.Character,
			"macro %q invoked without required argument %q", def.name, paramName)
		return nil, true
	}
	return nil, false
}
