package token

import "testing"

func TestTokenEnd(t *testing.T) {
	cases := []struct {
		name         string
		tok          Token
		wantL, wantC int
	}{
		{"identifier", Token{Text: "abc", Line: 2, Character: 4}, 2, 7},
		{"empty text", Token{Text: "", Line: 1, Character: 3}, 1, 3},
		{"supplementary-plane character is two UTF-16 units", Token{Text: "a\U0001F600b", Character: 1}, 0, 5},
		{"carriage returns are zero-width", Token{Text: "\"0\r\r\"", Character: 0}, 0, 3},
		{"newline starts a new line at column 0", Token{Text: "\"ab\\\ncd\"", Line: 5, Character: 9}, 6, 3},
		{"CRLF line continuation", Token{Text: "\"ab\\\r\ncd\"", Character: 0}, 1, 3},
	}
	for _, c := range cases {
		if l, ch := c.tok.End(); l != c.wantL || ch != c.wantC {
			t.Errorf("%s: End() = (%d, %d), want (%d, %d)", c.name, l, ch, c.wantL, c.wantC)
		}
	}
}
