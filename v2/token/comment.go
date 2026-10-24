package token

import "strings"

// Comment is a source comment, kept for tools that read what the code says
// about itself: a doc comment shown on hover, a "lint off" suppression marker.
// The lexer discards comments unless asked (lexer.LexWithComments), so they
// never reach the token stream.
type Comment struct {
	// Text is the exact source text, delimiters included: "// note", or a
	// "/* ... */" block (possibly spanning lines, and running to the end of
	// the file if unterminated). A line comment excludes its newline.
	Text string

	// Line and Character are where it starts, as for Token.
	Line      int
	Character int

	// OwnLine is true if no token precedes the comment on its first line, so
	// it is not a trailing comment on a statement above. A "// doc" line over a
	// declaration is OwnLine; the "// width" in "logic [7:0] x; // width" is not.
	OwnLine bool
}

// Block reports whether c is a "/* ... */" comment rather than a "//" one.
func (c Comment) Block() bool { return strings.HasPrefix(c.Text, "/*") }

// End returns the position just past the comment, tracked as Token.End does.
func (c Comment) End() (line, char int) {
	return endPosition(c.Line, c.Character, c.Text)
}

// Body is the comment's text without its delimiters or decoration. For a line
// comment that is what follows the slashes ("///" and "//!" count as slashes),
// trimmed. For a block comment it is each line trimmed of whitespace and of a
// leading "*", joined with newlines, so
//
//	/*
//	 * First line.
//	 * Second line.
//	 */
//
// reads as "First line.\nSecond line.".
func (c Comment) Body() string {
	if !c.Block() {
		text := strings.TrimLeft(c.Text[2:], "/!")
		return strings.TrimSpace(text)
	}
	text := strings.TrimPrefix(c.Text, "/*")
	text = strings.TrimSuffix(text, "*/")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimSpace(strings.TrimPrefix(l, "*"))
		lines[i] = l
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
