package preprocessor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/token"
)

func tokComment(text string, line int, own bool) token.Comment {
	return token.Comment{Text: text, Line: line, OwnLine: own}
}

func commentTexts(cs []Comment) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.File + ":" + c.Text
	}
	return out
}

func TestPreprocessDetailedKeepsCommentsWithTheirFile(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"leaf.svh": "// in leaf\nwire w; /* leaf trailing */\n"}}
	const src = "// in root\n`include \"leaf.svh\"\nmodule m; // after include\nendmodule\n"
	res := PreprocessDetailed("root.sv", src, r, Options{KeepComments: true})
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %+v", res.Errors)
	}
	got := commentTexts(res.Comments)
	want := []string{
		"root.sv:// in root", "root.sv:// after include", // the root file is read first, whole
		"leaf.svh:// in leaf", "leaf.svh:/* leaf trailing */",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comments\n got: %q\nwant: %q", got, want)
	}
	// Positions are true positions in the file they came from.
	for _, c := range res.Comments {
		if c.File == "leaf.svh" && c.Text == "// in leaf" && (c.Line != 0 || c.Character != 0) {
			t.Errorf("leaf comment at %d:%d, want 0:0", c.Line, c.Character)
		}
	}
}

func TestKeepingCommentsNeverChangesTheTokens(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"leaf.svh": "// c\nwire w;\n"}}
	const src = "`define W 8 // width\n// c\n`include \"leaf.svh\"\nmodule m #(P = `W); // t\nendmodule\n"
	plain, plainErrs := PreprocessWithOptions("root.sv", src, r, Options{})
	res := PreprocessDetailed("root.sv", src, r, Options{KeepComments: true})
	if len(plain) != len(res.Tokens) || len(plainErrs) != len(res.Errors) {
		t.Fatalf("token/error counts differ: %d/%d, %d/%d", len(plain), len(res.Tokens), len(plainErrs), len(res.Errors))
	}
	for i := range plain {
		if plain[i].Token != res.Tokens[i].Token || plain[i].File != res.Tokens[i].File {
			t.Errorf("token %d differs", i)
		}
	}
	if len(res.Comments) == 0 {
		t.Error("expected comments")
	}
}

func TestCommentsAreOffByDefault(t *testing.T) {
	res := PreprocessDetailed("a.sv", "// c\nmodule m; endmodule", nil, Options{})
	if len(res.Comments) != 0 {
		t.Errorf("got comments %v without KeepComments", commentTexts(res.Comments))
	}
}

func TestCommandLineMacrosContributeNoComments(t *testing.T) {
	res := PreprocessDetailed("a.sv", "module m; endmodule", nil,
		Options{KeepComments: true, InitialMacros: map[string]string{"X": "1 // from the command line"}})
	if len(res.Comments) != 0 {
		t.Errorf("a +define+ value leaked comments: %v", commentTexts(res.Comments))
	}
}

func TestCommentsInInactiveBranchesAndMacroBodies(t *testing.T) {
	// An inactive branch is still in the file; a macro body's comment is
	// returned once where it is written, not once per use.
	const src = "`ifdef NOPE\n// in dead code\n`endif\n`define M x // in the body\n`M `M `M\n"
	res := PreprocessDetailed("a.sv", src, nil, Options{KeepComments: true})
	got := strings.Join(commentTexts(res.Comments), "|")
	if got != "a.sv:// in dead code|a.sv:// in the body" {
		t.Errorf("comments = %q", got)
	}
}

func TestCommentIndexLeading(t *testing.T) {
	const src = `// stray, then a blank line

// Counts up.
// Wraps at 255.
module counter; // trailing, not documentation
  logic [7:0] q; // data

  /* Resets the count.
   * Active high. */
  function void reset(); endfunction

  // not documentation: a blank line follows

  function void other(); endfunction
endmodule
`
	res := PreprocessDetailed("c.sv", src, nil, Options{KeepComments: true})
	idx := NewCommentIndex(res.Comments)

	cases := []struct {
		line int
		want string
	}{
		{4, "Counts up.\nWraps at 255."}, // module counter: the stray comment is cut off by the blank line
		{5, ""},                          // logic q: only a trailing comment above it
		{9, "Resets the count.\nActive high."},
		{13, ""}, // other(): a blank line separates the comment
		{0, ""},  // nothing above line 0
	}
	for _, c := range cases {
		if got := DocText(idx.Leading("c.sv", c.line)); got != c.want {
			t.Errorf("Leading(line %d) = %q, want %q", c.line, got, c.want)
		}
	}
	if got := idx.Leading("other.sv", 4); got != nil {
		t.Errorf("another file's lines must not match: %v", got)
	}
}

func TestCommentIndexTrailing(t *testing.T) {
	res := PreprocessDetailed("c.sv", "logic a; // width\n// own line\nlogic b;\n", nil, Options{KeepComments: true})
	idx := NewCommentIndex(res.Comments)
	if c, ok := idx.Trailing("c.sv", 0); !ok || c.Body() != "width" {
		t.Errorf("Trailing(0) = %v, %v", c.Text, ok)
	}
	if _, ok := idx.Trailing("c.sv", 1); ok {
		t.Error("an own-line comment is not a trailing comment")
	}
	if _, ok := idx.Trailing("c.sv", 2); ok {
		t.Error("line 2 has no comment")
	}
}

func TestCommentIndexDeduplicatesAFileIncludedTwice(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"leaf.svh": "// shared doc\nwire w;\n"}}
	res := PreprocessDetailed("root.sv", "`include \"leaf.svh\"\n`include \"leaf.svh\"\n", r, Options{KeepComments: true})
	if len(res.Comments) != 2 {
		t.Fatalf("expected the comment to be returned per inclusion, got %v", commentTexts(res.Comments))
	}
	idx := NewCommentIndex(res.Comments)
	if got := idx.All("leaf.svh"); len(got) != 1 {
		t.Errorf("index kept %d copies, want 1", len(got))
	}
	if got := DocText(idx.Leading("leaf.svh", 1)); got != "shared doc" {
		t.Errorf("Leading = %q", got)
	}
}

func TestCommentIndexSurvivesUnsortedInput(t *testing.T) {
	// Comments arrive root-first, then includes; the index must not depend on order.
	cs := []Comment{
		{File: "f", Comment: tokComment("// second", 3, true)},
		{File: "f", Comment: tokComment("// first", 2, true)},
	}
	idx := NewCommentIndex(cs)
	if got := DocText(idx.Leading("f", 4)); got != "first\nsecond" {
		t.Errorf("Leading = %q, want the two lines in source order", got)
	}
}

func FuzzKeepCommentsNeverChangesTheTokens(f *testing.F) {
	for _, s := range []string{"// c\nmodule m; endmodule", "`define A 1 // c\n`A", "`ifdef X\n/* y */\n`endif", "/*", "a//b\n`include \"x\"", "`define F(a) a // c\n`F(1)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		plain, plainErrs := PreprocessWithOptions("f.sv", src, nil, Options{})
		res := PreprocessDetailed("f.sv", src, nil, Options{KeepComments: true})
		if len(plain) != len(res.Tokens) || len(plainErrs) != len(res.Errors) {
			t.Fatalf("keeping comments changed the result: %d/%d tokens, %d/%d errors", len(plain), len(res.Tokens), len(plainErrs), len(res.Errors))
		}
		for i := range plain {
			if plain[i].Token != res.Tokens[i].Token || plain[i].File != res.Tokens[i].File {
				t.Fatalf("token %d differs", i)
			}
		}
		idx := NewCommentIndex(res.Comments)
		for _, c := range res.Comments {
			idx.Leading(c.File, c.Line) // must not panic
			idx.Trailing(c.File, c.Line)
		}
	})
}
