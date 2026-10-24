package token

// Token is one lexical token: its category, exact source text, and start
// position. Text is the literal source slice (e.g. the exact spelling of
// an identifier or number, including digit separators) so a caller never
// needs to re-slice the original source. Position is start-only (Line,
// zero-based; Character, a UTF-16 column -- see utf16.go) with no
// end-position tracking, matching the minimalism of an End-less token
// design: an end position is trivially derivable from Text for
// single-line tokens (Character + UTF16Len(Text)), which covers
// everything except KindStringLiteral spanning a line continuation.
type Token struct {
	Kind      Kind
	Text      string
	Line      int
	Character int
}
