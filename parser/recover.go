package parser

import (
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

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

// proceduralStartKeywords are the keywords that begin a procedural block
// or statement recognized (but never parsed) at container scope --
// initial/always*/final (LRM 9.2) or a continuous assignment (LRM 10.3).
// Concurrent assertions (LRM 16.14) reuse concurrentAssertionKeywords,
// already defined in containers.go for parseDecl's own dispatch/labeled-
// form lookahead, rather than duplicating "assert"/"assume"/"cover"/
// "restrict" here.
var proceduralStartKeywords = map[string]bool{
	"initial": true, "always": true, "always_comb": true, "always_ff": true,
	"always_latch": true, "final": true, "assign": true,
}

// isDeclBoundaryKeyword reports whether text is a keyword that plausibly
// starts a NEW declaration/construct this parser recognizes at container
// scope -- the union of isDeclStartKeyword, isEndKeyword,
// proceduralStartKeywords, and concurrentAssertionKeywords. Unlike
// isDeclStartKeyword/isEndKeyword, which recover() uses to know where a
// fully-abandoned declaration can pick back up, this is used by the
// handful of "scan forward for my own terminating ';'" helpers
// (skipProceduralConstruct, skipHeaderToSemiStrict, collectExprUntil)
// that -- unlike recover() -- are NOT already in a failure state: they
// believe they're still scanning content belonging to the declaration
// currently being parsed, and have no other way to notice the real
// terminator went missing and they've wandered into the next
// declaration's own tokens instead. Each call site applies this only at
// a point it has already established is bracket/paren/block depth zero
// for its own construct -- isDeclBoundaryKeyword itself does no depth
// tracking of its own.
func isDeclBoundaryKeyword(text string) bool {
	return isDeclStartKeyword(text) || isEndKeyword(text) || proceduralStartKeywords[text] || concurrentAssertionKeywords[text]
}

// isTypeCastKeyword reports whether tok is a builtin type keyword
// immediately followed by "'" -- a type/sign cast ("int'(x)",
// "signed'(y)", LRM 6.24.1), not a new declaration starting with that
// type keyword. Without this, a cast legitimately appearing inside an
// expression this parser otherwise never inspects (a parameter's default
// value, or any other unparsed content a decl-boundary-aware scan passes
// over) would be misread as the next declaration beginning and cut short
// mid-expression -- the same same-text-different-role ambiguity
// isWaitOrDisableFork resolves for "fork" via one token of lookahead.
func isTypeCastKeyword(tok, next preprocessor.Token) bool {
	return tok.Kind == token.KindKeyword && dataTypeKeywords[tok.Text] && next.Kind == token.KindTick
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
