package preprocessor

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

// A realistic snippet exercising every feature this milestone added
// together: `include (of a header defining an object-like macro and a
// flag macro), a function-like macro with a nested ternary, `ifdef
// picking the true branch, and `undef. The point, as with the lexer's
// own integration test, isn't to check every single token -- it's to
// prove the pieces cohere on real-shaped input, not just in isolation.
const integrationHeader = "`define WIDTH 8\n`define SIM\n"

const integrationRoot = "`include \"defs.svh\"\n" +
	"\n" +
	"`define MAX(a,b) ((a) > (b) ? (a) : (b))\n" +
	"\n" +
	"module top;\n" +
	"  logic [`WIDTH-1:0] data;\n" +
	"\n" +
	"`ifdef SIM\n" +
	"  initial data = `MAX(1,2);\n" +
	"`else\n" +
	"  initial data = 0;\n" +
	"`endif\n" +
	"\n" +
	"`undef WIDTH\n" +
	"endmodule\n"

func integrationResolver() *fakeResolver {
	return &fakeResolver{files: map[string]string{"defs.svh": integrationHeader}}
}

func TestIntegrationSnippetNoErrors(t *testing.T) {
	_, errs := Preprocess("root.sv", integrationRoot, integrationResolver())
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestIntegrationSnippetNoLeftoverUnresolvedDirectives(t *testing.T) {
	toks, _ := Preprocess("root.sv", integrationRoot, integrationResolver())
	for _, tok := range toks {
		if tok.Kind == token.KindDirective {
			t.Errorf("unexpected unresolved directive token in output: %+v", tok)
		}
	}
}

func TestIntegrationSnippetTakesTrueIfdefBranchOnly(t *testing.T) {
	toks, _ := Preprocess("root.sv", integrationRoot, integrationResolver())
	texts := texts(toks)
	// A bare "0" also legitimately appears in the [WIDTH-1:0] bit range
	// regardless of which `ifdef branch is taken, so check for the
	// `else branch's specific "data = 0 ;" subsequence instead of any
	// occurrence of "0".
	elseBranch := []string{"data", "=", "0", ";"}
	if containsSubsequence(texts, elseBranch) {
		t.Fatalf("expected the `else branch (initial data = 0;) to be skipped, got %v", texts)
	}
}

func TestIntegrationSnippetFunctionLikeMacroExpandedCorrectly(t *testing.T) {
	toks, _ := Preprocess("root.sv", integrationRoot, integrationResolver())
	texts := texts(toks)
	// The expanded MAX(1,2) ternary, in order, must appear somewhere in
	// the output -- rather than pin the whole file's exact token
	// sequence, confirm this specific expansion by finding it as a
	// contiguous subsequence.
	want := []string{"(", "(", "1", ")", ">", "(", "2", ")", "?", "(", "1", ")", ":", "(", "2", ")", ")"}
	if !containsSubsequence(texts, want) {
		t.Fatalf("expected MAX(1,2)'s expansion %v as a contiguous run in %v", want, texts)
	}
}

func TestIntegrationSnippetWidthResolvesToHeaderDefinition(t *testing.T) {
	toks, _ := Preprocess("root.sv", integrationRoot, integrationResolver())
	for _, tok := range toks {
		if tok.MacroName != "WIDTH" {
			continue
		}
		if tok.Text != "8" {
			t.Fatalf("expected WIDTH's expansion to be \"8\", got %+v", tok)
		}
		if tok.File != "defs.svh" {
			t.Fatalf("expected WIDTH's expansion to be attributed to defs.svh, got %q", tok.File)
		}
		if tok.InvokedFrom == nil || tok.InvokedFrom.File != "root.sv" {
			t.Fatalf("expected InvokedFrom to point back at root.sv, got %+v", tok.InvokedFrom)
		}
		return
	}
	t.Fatalf("expected to find a token attributed to WIDTH's expansion")
}

func containsSubsequence(haystack, needle []string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		match := true
		for i, w := range needle {
			if haystack[start+i] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func FuzzPreprocessNeverPanics(f *testing.F) {
	seeds := []string{
		"", integrationRoot,
		"`define FOO `FOO\n`FOO",
		"`define A `B\n`define B `A\n`A",
		"`define FOO(a,b) a+b\n`FOO(1,2)",
		"`define FOO(a,b) a+b\n`FOO(1)",
		"`define FOO(a,b) a+b\n`FOO(1,2,3,4)",
		"`ifdef `ifdef `ifdef",
		"`elsif FOO",
		"`else",
		"`endif `endif `endif",
		"`include \"nonexistent.svh\"",
		"`include <angle.svh>",
		"`include",
		"`undef",
		"`undefineall",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	r := integrationResolver()
	f.Fuzz(func(t *testing.T, src string) {
		Preprocess("root.sv", src, r)
	})
}

func FuzzPreprocessNeverPanicsWithCyclicResolver(f *testing.F) {
	f.Add("`include \"a.svh\"")
	f.Add("")
	cyclic := &fakeResolver{files: map[string]string{
		"a.svh": "`include \"b.svh\"",
		"b.svh": "`include \"a.svh\"",
	}}
	f.Fuzz(func(t *testing.T, src string) {
		Preprocess("root.sv", src, cyclic)
	})
}
