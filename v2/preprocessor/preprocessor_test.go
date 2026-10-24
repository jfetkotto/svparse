package preprocessor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/token"
)

func pp(t *testing.T, src string) ([]Token, []Error) {
	t.Helper()
	return Preprocess("root.sv", src, nil)
}

func texts(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}

func assertTexts(t *testing.T, toks []Token, want ...string) {
	t.Helper()
	got := texts(toks)
	if len(got) != len(want) {
		t.Fatalf("got %d tokens %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestObjectLikeMacroExpansion(t *testing.T) {
	toks, errs := pp(t, "`define WIDTH 8\nlogic [`WIDTH-1:0] data;")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "logic", "[", "8", "-", "1", ":", "0", "]", "data", ";")
}

func TestObjectLikeMacroExpandsToMultipleTokens(t *testing.T) {
	toks, _ := pp(t, "`define PAIR a, b\nfoo(`PAIR);")
	assertTexts(t, toks, "foo", "(", "a", ",", "b", ")", ";")
}

func TestEmptyBodyMacroExpandsToNothing(t *testing.T) {
	toks, _ := pp(t, "`define FLAG\nbefore `FLAG after")
	assertTexts(t, toks, "before", "after")
}

func TestUndefRemovesDefinition(t *testing.T) {
	toks, _ := pp(t, "`define FOO 1\n`undef FOO\n`FOO")
	// FOO is no longer a known macro after `undef -- its reference passes
	// through literally as an unrecognized directive.
	assertTexts(t, toks, "FOO")
	if toks[0].Kind != token.KindDirective {
		t.Fatalf("expected the unresolved `FOO to remain a KindDirective token, got %+v", toks[0])
	}
}

func TestUndefineallClearsEverything(t *testing.T) {
	toks, _ := pp(t, "`define FOO 1\n`define BAR 2\n`undefineall\n`FOO `BAR")
	assertTexts(t, toks, "FOO", "BAR")
}

func TestMacroBodyLineContinuation(t *testing.T) {
	toks, errs := pp(t, "`define FOO a + \\\nb\n`FOO")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "a", "+", "b")
}

func TestSelfReferentialMacroDoesNotInfiniteLoop(t *testing.T) {
	toks, _ := pp(t, "`define FOO `FOO\n`FOO")
	// The inner `FOO (from FOO's own body) can't re-expand while FOO is
	// already being expanded -- it passes through literally.
	assertTexts(t, toks, "FOO")
	if toks[0].MacroName != "FOO" {
		t.Fatalf("expected the passed-through token to still be attributed to FOO's expansion, got %+v", toks[0])
	}
}

func TestMutuallyRecursiveMacrosDoNotInfiniteLoop(t *testing.T) {
	toks, _ := pp(t, "`define A `B\n`define B `A\n`A")
	// A -> B -> A (blocked, A is already expanding) -> literal `A.
	assertTexts(t, toks, "A")
}

func TestIfdefTrueBranch(t *testing.T) {
	toks, errs := pp(t, "`define DEBUG\n`ifdef DEBUG\nyes\n`endif")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "yes")
}

func TestIfdefFalseBranchSkipped(t *testing.T) {
	toks, _ := pp(t, "`ifdef DEBUG\nyes\n`endif\nafter")
	assertTexts(t, toks, "after")
}

func TestIfndef(t *testing.T) {
	toks, _ := pp(t, "`ifndef DEBUG\nyes\n`endif")
	assertTexts(t, toks, "yes")
}

func TestElseBranch(t *testing.T) {
	toks, _ := pp(t, "`ifdef DEBUG\nyes\n`else\nno\n`endif")
	assertTexts(t, toks, "no")
}

func TestElsifChainOnlyTakenBranchEmits(t *testing.T) {
	src := "`define B\n" +
		"`ifdef A\n" +
		"a_branch\n" +
		"`elsif B\n" +
		"b_branch\n" +
		"`elsif C\n" +
		"c_branch\n" +
		"`else\n" +
		"else_branch\n" +
		"`endif"
	toks, _ := pp(t, src)
	assertTexts(t, toks, "b_branch")
}

func TestElsifAfterTakenBranchStaysInactive(t *testing.T) {
	src := "`define A\n`define B\n`ifdef A\na_branch\n`elsif B\nb_branch\n`endif"
	toks, _ := pp(t, src)
	// A's branch is taken first; B's `elsif must not also fire even
	// though B is also defined.
	assertTexts(t, toks, "a_branch")
}

func TestMalformedElsifDoesNotLeaveTakenBranchActive(t *testing.T) {
	// FOO is defined, so `ifdef FOO's branch is taken; the malformed
	// `elsif that follows (not followed by an identifier) must not leave
	// the frame's active state untouched -- doing so would let B_branch
	// also emit, since the frame was still "active" from FOO's branch.
	src := "`define FOO\n`ifdef FOO\na_branch\n`elsif 123\nb_branch\n`endif"
	toks, errs := pp(t, src)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the malformed `elsif")
	}
	assertTexts(t, toks, "a_branch")
}

func TestMalformedElsifBranchItselfStaysInactive(t *testing.T) {
	// FOO is undefined, so `ifdef FOO's branch is NOT taken; the malformed
	// `elsif that follows must also not emit its own content -- a
	// malformed condition is treated as false, same as a well-formed one
	// naming an undefined macro.
	src := "`ifdef FOO\na_branch\n`elsif 123\nb_branch\n`endif"
	toks, errs := pp(t, src)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the malformed `elsif")
	}
	assertTexts(t, toks)
}

func TestDefineInsideSkippedBranchIsNotProcessed(t *testing.T) {
	toks, _ := pp(t, "`ifdef NOTDEFINED\n`define FOO 1\n`endif\n`FOO")
	// FOO was never actually defined (its `define was inside a skipped
	// branch), so the reference passes through literally.
	assertTexts(t, toks, "FOO")
}

func TestNestedIfdefInsideSkippedBranchStaysBalanced(t *testing.T) {
	src := "`ifdef NOTDEFINED\n" +
		"`ifdef ALSONOTDEFINED\n" +
		"inner\n" +
		"`endif\n" +
		"middle\n" +
		"`endif\n" +
		"after"
	toks, _ := pp(t, src)
	assertTexts(t, toks, "after")
}

func TestUnbalancedEndifIsIgnoredNotFatal(t *testing.T) {
	toks, errs := pp(t, "`endif\nafter")
	assertTexts(t, toks, "after")
	if len(errs) != 1 {
		t.Fatalf("expected one error for the stray `endif, got %+v", errs)
	}
}

func TestUnbalancedElseIsIgnoredNotFatal(t *testing.T) {
	_, errs := pp(t, "`else")
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestUnrecognizedDirectivePassesThrough(t *testing.T) {
	// `mystery_directive` is neither a conditional/macro directive nor a
	// known-argument one (see knownArgumentDirectives) -- lexically
	// indistinguishable from a reference to an undefined macro, so it must
	// pass through as a single token rather than being discarded, the same
	// as a genuinely undefined macro reference would.
	toks, errs := pp(t, "`mystery_directive\nmodule top;")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if toks[0].Kind != token.KindDirective || toks[0].Text != "mystery_directive" {
		t.Fatalf("expected `mystery_directive to pass through unmodified, got %+v", toks[0])
	}
}

func TestKnownArgumentDirectiveDiscardsRestOfLine(t *testing.T) {
	// `timescale (and other LRM chapter 22 directives whose argument
	// grammar this preprocessor doesn't parse) must not leak its raw
	// argument tokens into the output -- a parser has no way to make sense
	// of "1ns" / "/" / "1ps" as ordinary source text.
	toks, errs := pp(t, "`timescale 1ns / 1ps\nmodule top;")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "module", "top", ";")
}

func TestKnownArgumentDirectiveDoesNotLeakIntoFollowingDeclaration(t *testing.T) {
	// Regression case for the exact motivating bug: `default_nettype wire`
	// used to leave a dangling "wire" token in front of the module that
	// followed it.
	toks, errs := pp(t, "`default_nettype wire\nmodule top; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "module", "top", ";", "endmodule")
}

func TestUndefinedInlineMacroReferenceDoesNotConsumeRestOfLine(t *testing.T) {
	// An undefined macro referenced inline inside a larger expression must
	// pass through as a single token, NOT trigger the known-argument-
	// directive line-discarding behavior -- `WIDTH` is lexically identical
	// to a directive name until looked up, and is neither a conditional/
	// macro directive nor in knownArgumentDirectives.
	toks, errs := pp(t, "logic [`WIDTH-1:0] data;")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "logic", "[", "WIDTH", "-", "1", ":", "0", "]", "data", ";")
}

func TestExpandedTokenSourceMapPointsAtInvocation(t *testing.T) {
	toks, _ := pp(t, "`define WIDTH 8\nlogic [`WIDTH-1:0] data;")
	widthTok := toks[2] // "8", from the expansion
	if widthTok.MacroName != "WIDTH" {
		t.Fatalf("expected MacroName WIDTH, got %+v", widthTok)
	}
	if widthTok.File != "root.sv" {
		t.Fatalf("expected the expanded token's File to be the definition file, got %q", widthTok.File)
	}
	if widthTok.InvokedFrom == nil {
		t.Fatalf("expected InvokedFrom to be set")
	}
	if widthTok.InvokedFrom.Text != "WIDTH" || widthTok.InvokedFrom.Kind != token.KindDirective {
		t.Fatalf("expected InvokedFrom to point at the `WIDTH invocation token, got %+v", widthTok.InvokedFrom)
	}
	// The invocation site itself is on line 1 (0-based), inside the
	// second source line -- confirms InvokedFrom carries a real position,
	// not a zero value.
	if widthTok.InvokedFrom.Line != 1 {
		t.Fatalf("expected the invocation to be on line 1, got %+v", widthTok.InvokedFrom)
	}
}

func TestPlainTokensCarryFileAndNoMacroAttribution(t *testing.T) {
	toks, _ := pp(t, "module top;")
	if toks[0].File != "root.sv" {
		t.Fatalf("expected File to be set on a plain token, got %+v", toks[0])
	}
	if toks[0].MacroName != "" || toks[0].InvokedFrom != nil {
		t.Fatalf("expected no macro attribution on a plain token, got %+v", toks[0])
	}
}

func TestInitialMacroObjectLike(t *testing.T) {
	toks, errs := PreprocessWithOptions("root.sv", "logic [`WIDTH-1:0] data;", nil, Options{
		InitialMacros: map[string]string{"WIDTH": "8"},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "logic", "[", "8", "-", "1", ":", "0", "]", "data", ";")
}

func TestInitialMacroEmptyValueDefinesFlagWithNoBody(t *testing.T) {
	toks, _ := PreprocessWithOptions("root.sv", "before `FLAG after", nil, Options{
		InitialMacros: map[string]string{"FLAG": ""},
	})
	assertTexts(t, toks, "before", "after")
}

func TestInitialMacroFeedsIfdef(t *testing.T) {
	// The realistic use case: a +define+ from a filelist gating an
	// `ifdef in source, exactly like a `define earlier in the same file
	// would.
	toks, _ := PreprocessWithOptions("root.sv", "`ifdef SIMULATION\nlogic sim_only;\n`endif", nil, Options{
		InitialMacros: map[string]string{"SIMULATION": ""},
	})
	assertTexts(t, toks, "logic", "sim_only", ";")
}

func TestInitialMacroOverriddenByLaterDefine(t *testing.T) {
	// A later `define for the same name in-source takes over, matching
	// how re-defining any macro works -- initial macros aren't special
	// once seeded, just pre-populated entries in the same map.
	toks, _ := PreprocessWithOptions("root.sv", "`define WIDTH 16\n`WIDTH", nil, Options{
		InitialMacros: map[string]string{"WIDTH": "8"},
	})
	assertTexts(t, toks, "16")
}

func TestPreprocessNeverPanicsOnArbitraryInput(t *testing.T) {
	inputs := []string{
		"", "`", "`ifdef", "`define", "`undef", "`elsif", "`else", "`endif",
		"`ifdef `ifdef `ifdef", "`define FOO `FOO `FOO `FOO",
		"`define A `B\n`define B `C\n`define C `A\n`A",
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Preprocess(%q) panicked: %v", in, r)
				}
			}()
			Preprocess("root.sv", in, nil)
		}()
	}
}

// The mirror of TestUnbalancedEndifIsIgnoredNotFatal. An unterminated
// conditional used to be the quietest possible failure: every token after
// it is skipped, so the file reaches the parser with its declarations
// gone, and errs was EMPTY -- nothing anywhere explained why.
func TestUnterminatedIfdefIsReported(t *testing.T) {
	toks, errs := pp(t, "`ifdef FOO\nmodule top;\nendmodule\n")
	assertTexts(t, toks)
	if len(errs) != 1 {
		t.Fatalf("expected one error for the unterminated `ifdef, got %+v", errs)
	}
	if errs[0].Line != 0 {
		t.Fatalf("expected the error at the `ifdef itself (line 0), got %+v", errs[0])
	}
	if !strings.Contains(errs[0].Message, "unterminated") {
		t.Fatalf("unexpected message: %q", errs[0].Message)
	}
}

func TestUnterminatedIfdefReportsOnePerOpenFrame(t *testing.T) {
	_, errs := pp(t, "`ifdef A\n`ifdef B\n")
	if len(errs) != 2 {
		t.Fatalf("expected one error per open frame, got %+v", errs)
	}
}

func TestTerminatedIfdefReportsNothing(t *testing.T) {
	_, errs := pp(t, "`ifdef FOO\n`endif\nafter\n")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
}

// A macro chain that doubles at each level is not recursive, so neither
// the self-reference guard nor the include-cycle guard sees anything
// wrong -- without a budget this expands to 2^n tokens and never returns.
// Driven against a deliberately tiny maxSteps so the test proves the
// budget stops it without having to burn the real 20-million-token one.
func TestRunawayMacroExpansionIsBounded(t *testing.T) {
	var src strings.Builder
	const levels = 40
	src.WriteString("`define L0 x\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&src, "`define L%d `L%d `L%d\n", i, i-1, i-1)
	}
	fmt.Fprintf(&src, "`L%d\n", levels)

	p := newPreprocessor(nil)
	p.maxSteps = 100_000
	p.including["test.sv"] = true
	p.pushFile("test.sv", src.String())
	p.run()

	for _, e := range p.errs {
		if strings.Contains(e.Message, "giving up") {
			return
		}
	}
	t.Fatalf("expected the expansion budget to stop this, got %+v", p.errs)
}
