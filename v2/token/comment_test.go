package token

import "testing"

func TestCommentEnd(t *testing.T) {
	cases := []struct {
		name         string
		c            Comment
		wantL, wantC int
	}{
		{"line comment", Comment{Text: "// hi", Line: 2, Character: 4}, 2, 9},
		{"block on one line", Comment{Text: "/* x */", Line: 0, Character: 3}, 0, 10},
		{"block over lines", Comment{Text: "/* a\n  b */", Line: 1, Character: 2}, 2, 6},
		{"carriage return is zero-width", Comment{Text: "// a\r", Line: 0, Character: 0}, 0, 4},
		{"supplementary character is two units", Comment{Text: "// \U0001F600", Line: 0, Character: 0}, 0, 5},
	}
	for _, c := range cases {
		if l, ch := c.c.End(); l != c.wantL || ch != c.wantC {
			t.Errorf("%s: End() = (%d, %d), want (%d, %d)", c.name, l, ch, c.wantL, c.wantC)
		}
	}
}

func TestCommentBlock(t *testing.T) {
	if (Comment{Text: "// x"}).Block() || !(Comment{Text: "/* x */"}).Block() {
		t.Error("Block() misclassifies")
	}
}

func TestCommentBody(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"line", "// a note", "a note"},
		{"line, no space", "//note", "note"},
		{"doc slashes", "/// documented", "documented"},
		{"bang doc", "//! documented", "documented"},
		{"rule of slashes", "//////////", ""},
		{"line with carriage return", "// win\r", "win"},
		{"block one line", "/* a note */", "a note"},
		{"block with star margin", "/*\n * First.\n * Second.\n */", "First.\nSecond."},
		{"block without margin", "/*\n  First.\n  Second.\n*/", "First.\nSecond."},
		{"javadoc style", "/**\n * Brief.\n */", "Brief."},
		{"unterminated block", "/* never closed", "never closed"},
		{"empty", "/**/", ""},
	}
	for _, c := range cases {
		if got := (Comment{Text: c.text}).Body(); got != c.want {
			t.Errorf("%s: Body(%q) = %q, want %q", c.name, c.text, got, c.want)
		}
	}
}
