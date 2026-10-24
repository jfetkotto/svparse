package preprocessor

import (
	"strings"

	"github.com/jfetkotto/svparse/v2/token"
)

// Reserved words of IEEE 1364-2005 (Table 19-1), the base of the legacy
// keyword sets that `begin_keywords selects (LRM 22.14).
const verilog2005Keywords = `always and assign automatic begin buf bufif0 bufif1 case casex casez cell cmos
config deassign default defparam design disable edge else end endcase endconfig endfunction endgenerate
endmodule endprimitive endspecify endtable endtask event for force forever fork function generate genvar
highz0 highz1 if ifnone incdir include initial inout input instance integer join large liblist library
localparam macromodule medium module nand negedge nmos nor noshowcancelled not notif0 notif1 or output
parameter pmos posedge primitive pull0 pull1 pulldown pullup pulsestyle_onevent pulsestyle_ondetect rcmos
real realtime reg release repeat rnmos rpmos rtran rtranif0 rtranif1 scalared showcancelled signed small
specify specparam strong0 strong1 supply0 supply1 table task time tran tranif0 tranif1 tri tri0 tri1 triand
trior trireg unsigned use uwire vectored wait wand weak0 weak1 while wire wor xnor xor`

const (
	configKeywords = "cell config design endconfig incdir include instance liblist library use"
	// Added by 1364-2001 over 1364-1995.
	verilog2001Additions = "automatic endgenerate generate genvar localparam signed unsigned " +
		"noshowcancelled showcancelled pulsestyle_onevent pulsestyle_ondetect " + configKeywords
)

// legacyKeywords returns the reserved words of the `begin_keywords version
// string, or nil when the version is a SystemVerilog one (every keyword of
// this lexer applies) or unknown. LRM 22.14.
func legacyKeywords(version string) map[string]bool {
	set := map[string]bool{}
	add := func(words string) {
		for _, w := range strings.Fields(words) {
			set[w] = true
		}
	}
	remove := func(words string) {
		for _, w := range strings.Fields(words) {
			delete(set, w)
		}
	}
	add(verilog2005Keywords)
	switch version {
	case "1364-2005":
	case "1364-2001":
		remove("uwire")
	case "1364-2001-noconfig":
		remove("uwire " + configKeywords)
	case "1364-1995":
		remove("uwire " + verilog2001Additions)
	default:
		return nil
	}
	return set
}

// handleBeginKeywords starts a region in which only a legacy version's
// reserved words are keywords: the others (`logic`, `bit`, ...) are plain
// identifiers there (LRM 22.14).
func (p *preprocessor) handleBeginKeywords(directiveTok Token, src *tokenSource) {
	arg := p.collectLogicalLine(src, directiveTok.Line)
	var set map[string]bool
	if len(arg) == 1 && arg[0].Kind == token.KindStringLiteral {
		if v, ok := unquote(arg[0].Text); ok {
			set = legacyKeywords(v)
		}
	}
	p.keywords = append(p.keywords, set)
}

// handleEndKeywords ends the innermost `begin_keywords region.
func (p *preprocessor) handleEndKeywords(directiveTok Token, src *tokenSource) {
	p.collectLogicalLine(src, directiveTok.Line)
	if len(p.keywords) > 0 {
		p.keywords = p.keywords[:len(p.keywords)-1]
	}
}

// emit appends tok to the output, demoting a keyword that the current
// `begin_keywords region does not reserve to an identifier.
func (p *preprocessor) emit(tok Token) {
	if tok.Kind == token.KindKeyword && len(p.keywords) > 0 {
		if set := p.keywords[len(p.keywords)-1]; set != nil && !set[tok.Text] {
			tok.Kind = token.KindIdent
		}
	}
	p.out = append(p.out, tok)
}
