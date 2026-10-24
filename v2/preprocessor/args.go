package preprocessor

import "github.com/jfetkotto/svparse/token"

// SplitBalancedArgs consumes toks starting at pos, which must be
// positioned right after an already-consumed opening '(', up to and
// including its matching ')', splitting the tokens in between into
// comma-separated groups. Nesting inside another ( { [ '{ pair -- a
// nested call, an array/bit-select, an assignment-pattern literal --
// isn't split at its own, deeper comma. newPos is the index just past
// the matching ')' (or, if none was found, wherever scanning stopped).
//
// Exported because both this package's own macro-argument/parameter-
// list parsing and svparse/parser's instantiation port-connection/
// parameter-override parsing need the identical splitting shape and
// would otherwise duplicate it.
//
// Returns closed=false if input ran out before a balancing ')' was found
// (malformed/truncated input) -- the caller decides how to report that.
func SplitBalancedArgs(toks []Token, pos int) (groups [][]Token, newPos int, closed bool) {
	depth := 0
	var current []Token
	flush := func() { groups = append(groups, current); current = nil }

	for pos < len(toks) {
		tok := toks[pos]
		if tok.Kind == token.KindEOF {
			return groups, pos, false
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen:
			if depth == 0 {
				flush()
				pos++
				return groups, pos, true
			}
			depth--
		case token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		case token.KindComma:
			if depth == 0 {
				flush()
				pos++
				continue
			}
		}
		current = append(current, tok)
		pos++
	}
	return groups, pos, false
}

// splitBalancedArgs adapts SplitBalancedArgs to a tokenSource, the shape
// every call site inside this package already works with.
func (p *preprocessor) splitBalancedArgs(src *tokenSource) (groups [][]Token, closed bool) {
	groups, newPos, closed := SplitBalancedArgs(src.toks, src.pos)
	src.pos = newPos
	return groups, closed
}

// isEmptyParenGroups reports whether groups is exactly what
// SplitBalancedArgs returns for a bare "()" -- one single empty group --
// which means zero arguments/parameters were actually written, not one
// blank one.
func isEmptyParenGroups(groups [][]Token) bool {
	return len(groups) == 1 && len(groups[0]) == 0
}
