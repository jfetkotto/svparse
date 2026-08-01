package parser

import "github.com/jfetkotto/svparse/token"

// endKeywords are the container/member end keywords this parser's
// grammar covers -- used both to detect "this container is done" and,
// in recover, "stop skipping, something structurally recognizable is
// here." Deliberately doesn't include end keywords for constructs this
// parser only recognizes well enough to skip whole (covergroup/property/
// sequence/clocking/checker's endgroup/endproperty/endsequence/
// endclocking/endchecker, handled directly by parseDecl's own dispatch
// via skipToKeyword rather than through parseBody's endKeyword mechanism
// -- see containers.go) or that remain fully out of scope (endconfig,
// endprimitive, endtable) -- the latter are unrecognized tokens like any
// other, handled by recover's generic skip.
var endKeywords = map[string]bool{
	"endmodule": true, "endinterface": true, "endprogram": true,
	"endclass": true, "endpackage": true, "endfunction": true, "endtask": true,
}

// containerStartKeywords are the keywords that begin a new top-level (or
// class-body) declaration recover should stop at rather than skip past,
// so a malformed declaration doesn't eat the start of the next one.
var containerStartKeywords = map[string]bool{
	"module": true, "macromodule": true, "interface": true, "program": true,
	"class": true, "package": true, "virtual": true,
	"typedef": true, "parameter": true, "localparam": true,
	"function": true, "task": true, "extern": true, "import": true,
	"constraint": true, "static": true, "rand": true, "randc": true,
}

func isEndKeyword(text string) bool { return endKeywords[text] }

// isDeclStartKeyword reports whether text is any keyword that starts a
// declaration this parser recognizes -- containerStartKeywords plus the
// builtin data-type keywords a variable/net declaration can start with
// (dataTypeKeywords, defined in variables.go).
func isDeclStartKeyword(text string) bool {
	return containerStartKeywords[text] || dataTypeKeywords[text]
}

// recover skips tokens -- tracking ( { [ depth so a ';' nested inside
// (e.g. a struct body) isn't mistaken for the enclosing, malformed
// declaration's own terminator -- until a top-level ';' (consumed) or a
// recognized container-start/end keyword (not consumed, left for the
// caller's dispatch loop to see). Generalizes nols's own popMatching's
// "a stray/mismatched keyword doesn't corrupt the rest of the parse"
// philosophy: one malformed declaration doesn't take down parsing of
// everything after it.
//
// The token at the cursor when recover is called already failed to
// parse as a declaration -- that's why recover was called -- so it is
// always consumed unconditionally before the stop conditions are even
// considered. Without that, a stray/mismatched end keyword with nothing
// open to match it (e.g. "endclass" at top level) would satisfy the
// stop condition on the very first check, before consuming anything,
// and the caller's loop would spin on it forever: parseDecl fails on
// it again, recover is called again, it stops again without consuming
// again. One guaranteed-forward-progress token up front rules that out.
func (p *parser) recover() {
	depth := 0
	updateDepth := func(kind token.Kind) {
		switch kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		}
	}

	if first := p.peek(); first.Kind != token.KindEOF {
		updateDepth(first.Kind)
		p.advance()
		if depth == 0 && first.Kind == token.KindSemi {
			return
		}
	}

	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			return
		}
		if depth == 0 {
			if tok.Kind == token.KindSemi {
				p.advance()
				return
			}
			if tok.Kind == token.KindKeyword && (isDeclStartKeyword(tok.Text) || isEndKeyword(tok.Text)) {
				return
			}
		}
		updateDepth(tok.Kind)
		p.advance()
	}
}
