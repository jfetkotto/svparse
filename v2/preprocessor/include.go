package preprocessor

import "github.com/jfetkotto/svparse/token"

// handleInclude consumes an `include directive's path and pushes the
// resolved file's tokens as a new source, so the same main loop that
// processes everything else also handles the included file's own
// directives (definitions, conditionals, nested includes).
//
// Only the double-quoted form (`include "path") is supported -- the
// angle-bracket form (`include <path>) would need to reconstruct an
// exact path from separately-tokenized '<' ident '.' ident '>' tokens,
// which needs whitespace fidelity the lexer deliberately doesn't keep,
// and this project's target workspace convention already uses quoted,
// filelist-style paths exclusively.
func (p *preprocessor) handleInclude(directiveTok Token, src *tokenSource) {
	pathTok, ok := p.next(src)
	if !ok || pathTok.Kind != token.KindStringLiteral {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character,
			"`include requires a quoted path (the <path> form is not supported)")
		return
	}
	path, ok := unquote(pathTok.Text)
	if !ok {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "malformed `include path %s", pathTok.Text)
		return
	}

	if p.resolver == nil {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`include %q: no IncludeResolver configured", path)
		return
	}
	text, resolvedPath, err := p.resolver.Resolve(path, directiveTok.File)
	if err != nil {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`include %q: %s", path, err)
		return
	}

	// Cycle detection is scoped to "currently on the active inclusion
	// stack" (cleared via onPop below when this source is fully
	// consumed), not "ever included" -- a file legitimately included from
	// two unrelated places, or guarded by its own `ifndef, must still
	// work.
	if p.including[resolvedPath] {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "cyclic `include of %q", resolvedPath)
		return
	}
	p.including[resolvedPath] = true

	p.stack = append(p.stack, &tokenSource{
		toks:  p.lexToTokens(resolvedPath, text),
		onPop: func() { delete(p.including, resolvedPath) },
	})
}

// unquote strips path's surrounding double quotes (as read verbatim from
// a KindStringLiteral token, delimiters included). It doesn't decode any
// backslash escapes -- a real filesystem path is vanishingly unlikely to
// need octal/hex escapes, so the exact source substring between the
// quotes is used as-is.
func unquote(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	return s[1 : len(s)-1], true
}
