package token

// The SV grammar's plain identifier production (IEEE 1800-2017 Annex A:
// [a-zA-Z_][a-zA-Z0-9_$]*) is deliberately ASCII-only. It lives here, in
// the vocabulary package, rather than privately inside the lexer, because
// the lexer is not the only thing that has to agree on it: a consumer that
// decides where a "word" starts and ends at a cursor position, or validates
// a proposed new identifier before writing it into a file, must draw the
// same boundary the lexer does or the two silently disagree.
//
// A looser rule (unicode.IsLetter, say) is the tempting approximation, and
// the one that goes wrong quietly: it accepts a name this lexer will then
// tag KindInvalid, so a rename to it produces source that no longer
// tokenizes and a symbol that can never be found again.

// IsIdentStart reports whether r can begin a plain SV identifier.
func IsIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

// IsIdentChar reports whether r can appear in a plain SV identifier after
// its first character.
func IsIdentChar(r rune) bool {
	return IsIdentStart(r) || (r >= '0' && r <= '9') || r == '$'
}

// IsIdentifier reports whether s is spelled as a plain SV identifier. It
// says nothing about whether s is a reserved word -- check Keywords for
// that; a caller validating a rename target wants both, but the two
// questions are separate (an escaped identifier, "\foo.bar ", is a legal
// name that is not spelled this way at all).
func IsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !IsIdentStart(r) {
				return false
			}
			continue
		}
		if !IsIdentChar(r) {
			return false
		}
	}
	return true
}
