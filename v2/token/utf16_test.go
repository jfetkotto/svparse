package token

import "testing"

// The 😀 below is U+1F600, a supplementary-plane character: 1 rune but 2
// UTF-16 code units. Everything after it on the same line sits one column
// further in UTF-16 than a rune count would say.

func TestUTF16Len(t *testing.T) {
	if got := UTF16Len("a😀b"); got != 4 {
		t.Fatalf("UTF16Len(a😀b) = %d, want 4", got)
	}
	if got := UTF16Len("wide_top"); got != 8 {
		t.Fatalf("UTF16Len(wide_top) = %d, want 8", got)
	}
}

func TestUTF16Width(t *testing.T) {
	if got := UTF16Width('a'); got != 1 {
		t.Fatalf("UTF16Width('a') = %d, want 1", got)
	}
	if got := UTF16Width('😀'); got != 2 {
		t.Fatalf("UTF16Width('😀') = %d, want 2", got)
	}
}

func TestRuneColumn(t *testing.T) {
	line := []rune("a😀bc")
	cases := []struct {
		utf16Col int
		wantRune int
	}{
		{0, 0},  // before "a"
		{1, 1},  // before "😀"
		{3, 2},  // "😀" occupies UTF-16 columns 1-2; column 3 is right after it, before "b"
		{4, 3},  // before "c"
		{5, 4},  // end of line
		{99, 4}, // past the end, clamps to the line length
	}
	for _, c := range cases {
		if got := RuneColumn(line, c.utf16Col); got != c.wantRune {
			t.Errorf("RuneColumn(line, %d) = %d, want %d", c.utf16Col, got, c.wantRune)
		}
	}
}

func TestUTF16Column(t *testing.T) {
	line := []rune("a😀bc")
	cases := []struct {
		runeIdx int
		want    int
	}{
		{0, 0}, // "a"
		{1, 1}, // "😀"
		{2, 3}, // "b" -- after the 2-wide emoji
		{3, 4}, // "c"
		{4, 5}, // end of line
	}
	for _, c := range cases {
		if got := UTF16Column(line, c.runeIdx); got != c.want {
			t.Errorf("UTF16Column(line, %d) = %d, want %d", c.runeIdx, got, c.want)
		}
	}
}

func TestRuneColumnAndUTF16ColumnAreInverses(t *testing.T) {
	line := []rune("clk_😀_rst")
	for runeIdx := range line {
		col := UTF16Column(line, runeIdx)
		if got := RuneColumn(line, col); got != runeIdx {
			t.Errorf("RuneColumn(line, UTF16Column(line, %d)) = %d, want %d", runeIdx, got, runeIdx)
		}
	}
}
