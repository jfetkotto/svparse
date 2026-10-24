package token

// Token is one lexical token: its category, exact source text, and start
// position. Text is the literal source slice (e.g. the exact spelling of
// an identifier or number, including digit separators) so a caller never
// needs to re-slice the original source. Position is start-only (Line,
// zero-based; Character, a UTF-16 column -- see utf16.go) with no
// end-position tracking, matching the minimalism of an End-less token
// design: an end position is derivable from Text alone, via End.
type Token struct {
	Kind      Kind
	Text      string
	Line      int
	Character int
}

// End returns the position just past the token's last character. It tracks
// columns exactly as the lexer does while scanning: a UTF-16 unit count per
// character, a carriage return is zero-width, and a newline (inside a string
// literal's line continuation, say) starts a new line at column 0. Computing
// Character + UTF16Len(Text) instead is wrong for any token containing a CR
// or a newline.
func (t Token) End() (line, char int) {
	return endPosition(t.Line, t.Character, t.Text)
}

// endPosition is the position just past text that starts at (line, char),
// advancing exactly as the lexer does.
func endPosition(line, char int, text string) (int, int) {
	for _, r := range text {
		switch r {
		case '\n':
			line++
			char = 0
		case '\r':
			// Zero-width, matching the lexer's own column tracking.
		default:
			char += UTF16Width(r)
		}
	}
	return line, char
}
