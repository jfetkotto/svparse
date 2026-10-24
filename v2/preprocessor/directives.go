package preprocessor

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jfetkotto/svparse/v2/token"
)

// compilerDirectives are the names LRM 22.5.1 reserves: a macro may not be
// defined with one of them.
var compilerDirectives = map[string]bool{
	"define": true, "undef": true, "undefineall": true, "include": true,
	"ifdef": true, "ifndef": true, "else": true, "elsif": true, "endif": true,
	"timescale": true, "default_nettype": true, "unconnected_drive": true,
	"nounconnected_drive": true, "pragma": true, "line": true,
	"celldefine": true, "endcelldefine": true, "resetall": true,
	"begin_keywords": true, "end_keywords": true, "__FILE__": true, "__LINE__": true,
}

// checkDirective validates the arguments of a directive whose arguments the
// preprocessor otherwise discards (LRM 22.3, 22.7, 22.9, 22.11, 22.12). An
// argument that is itself a macro reference is not checked: its value is not
// known here.
func (p *preprocessor) checkDirective(tok Token, args []Token) {
	for _, a := range args {
		if a.Kind == token.KindDirective {
			return
		}
	}
	bad := func(format string, a ...any) { p.errorf(tok.File, tok.Line, tok.Character, format, a...) }
	switch tok.Text {
	case "resetall":
		if p.designDepth > 0 {
			bad("`resetall is illegal inside a design element (LRM 22.3)")
		}
	case "timescale":
		if msg := checkTimescale(args); msg != "" {
			bad("`timescale: %s (LRM 22.7)", msg)
		}
	case "unconnected_drive":
		if len(args) != 1 || (args[0].Text != "pull0" && args[0].Text != "pull1") {
			bad("`unconnected_drive takes pull0 or pull1 (LRM 22.9)")
		}
	case "nounconnected_drive":
		if len(args) != 0 {
			bad("`nounconnected_drive takes no argument (LRM 22.9)")
		}
	case "pragma":
		if len(args) == 0 || args[0].Kind != token.KindIdent && args[0].Kind != token.KindKeyword {
			bad("`pragma needs a pragma name (LRM 22.11)")
		}
	case "line":
		if msg := checkLine(args); msg != "" {
			bad("`line: %s (LRM 22.12)", msg)
		}
	}
}

// checkLine validates "`line number "filename" level".
func checkLine(args []Token) string {
	if len(args) > 0 && args[0].Kind == token.KindSub {
		return "the line number must be a positive integer"
	}
	if len(args) != 3 {
		return "expected a line number, a file name string and a level"
	}
	n, err := strconv.Atoi(args[0].Text)
	if args[0].Kind != token.KindIntLiteral || err != nil || n < 1 {
		return "the line number must be a positive integer"
	}
	if args[1].Kind != token.KindStringLiteral {
		return "the file name must be a string literal"
	}
	if lvl := args[2].Text; args[2].Kind != token.KindIntLiteral || (lvl != "0" && lvl != "1" && lvl != "2") {
		return "the level must be 0, 1 or 2"
	}
	return ""
}

var timescaleRe = regexp.MustCompile(`^(\d+)(s|ms|us|ns|ps|fs)/(\d+)(s|ms|us|ns|ps|fs)$`)

var timeUnitExp = map[string]int{"s": 0, "ms": -3, "us": -6, "ns": -9, "ps": -12, "fs": -15}

// checkTimescale validates "`timescale unit / precision": each a 1, 10 or 100
// followed by a time unit, the precision no coarser than the unit.
func checkTimescale(args []Token) string {
	var sb strings.Builder
	for _, a := range args {
		sb.WriteString(a.Text)
	}
	m := timescaleRe.FindStringSubmatch(sb.String())
	if m == nil {
		return "expected \"unit / precision\", e.g. 1ns / 1ps"
	}
	exp := func(mag, unit string) (int, bool) {
		e, ok := map[string]int{"1": 0, "10": 1, "100": 2}[mag]
		return e + timeUnitExp[unit], ok
	}
	ue, ok1 := exp(m[1], m[2])
	pe, ok2 := exp(m[3], m[4])
	if !ok1 || !ok2 {
		return "the magnitude must be 1, 10 or 100"
	}
	if pe > ue {
		return "the precision must be at least as fine as the time unit"
	}
	return ""
}

// trackDesignElement follows module/interface/program/package/primitive
// nesting in the output stream, so `resetall can be refused inside one.
func (p *preprocessor) trackDesignElement(tok Token) {
	if tok.Kind != token.KindKeyword {
		return
	}
	prev := ""
	if n := len(p.out); n > 0 {
		prev = p.out[n-1].Text
	}
	switch tok.Text {
	case "module", "macromodule", "program", "package", "primitive":
		if prev != "extern" {
			p.designDepth++
		}
	case "interface":
		if prev != "extern" && prev != "virtual" {
			p.designDepth++
		}
	case "class":
		if prev == "interface" { // an interface class is not a design element
			p.designDepth--
		}
	case "endmodule", "endprogram", "endpackage", "endprimitive", "endinterface":
		if p.designDepth > 0 {
			p.designDepth--
		}
	}
}
