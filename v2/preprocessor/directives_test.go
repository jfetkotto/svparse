package preprocessor

import (
	"strings"
	"testing"
)

func TestDirectiveArgumentChecks(t *testing.T) {
	cases := []struct {
		src  string
		want string // substring of the one expected error; empty = none
	}{
		{"`line 1 \"f\" 0\n", ""},
		{"`line 7 \"f\" 2\n", ""},
		{"`line 1 \"f\" 3\n", "level"},
		{"`line 1 f 2\n", "string"},
		{"`line -12 \"f\" 2\n", "positive"},
		{"`line 0 \"f\" 2\n", "positive"},
		{"`line 1 \"f\"\n", "expected a line number"},
		{"`timescale 1ns/1ps\n", ""},
		{"`timescale 10 ns / 100 ps\n", ""},
		{"`timescale 100us / 1ns\n", ""},
		{"`timescale 9 ns / 1 ps\n", "magnitude"},
		{"`timescale 1 ns / 10 ns\n", "precision"},
		{"`timescale 1ns\n", "unit / precision"},
		{"`unconnected_drive pull0\n`nounconnected_drive\n", ""},
		{"`unconnected_drive\n", "pull0 or pull1"},
		{"`unconnected_drive pull2\n", "pull0 or pull1"},
		{"`nounconnected_drive pull0\n", "no argument"},
		{"`pragma protect begin\n", ""},
		{"`pragma\n", "pragma name"},
		{"`resetall\nmodule m; endmodule\n`resetall\n", ""},
		{"module m;\n`resetall\nendmodule\n", "design element"},
		{"interface class c; endclass\n`resetall\n", ""},
		{"`define include 1\n", "cannot be redefined"},
		{"`define W 8\n", ""},
	}
	for _, c := range cases {
		_, errs := Preprocess("a.sv", c.src, nil)
		switch {
		case c.want == "" && len(errs) != 0:
			t.Errorf("%q: unexpected %+v", c.src, errs)
		case c.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, c.want)):
			t.Errorf("%q: want one error containing %q, got %+v", c.src, c.want, errs)
		}
	}
}
