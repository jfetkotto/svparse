package token

import "testing"

func TestIsIdentifier(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"clk", true},
		{"_leading_underscore", true},
		{"a$b", true},
		{"n0", true},
		{"A_1$", true},
		{"", false},
		{"0start", false},
		{"$display", false},
		{"has space", false},
		{"has-dash", false},
		// Non-ASCII is the case a unicode.IsLetter-based approximation gets
		// wrong: it accepts these, then the lexer tags them KindInvalid.
		{"café", false},
		{"naïve_sig", false},
		{"Ω", false},
	}
	for _, tc := range cases {
		if got := IsIdentifier(tc.s); got != tc.want {
			t.Errorf("IsIdentifier(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}
