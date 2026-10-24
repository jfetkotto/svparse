package preprocessor

import (
	"fmt"
	"testing"
)

// fakeResolver is a small in-memory IncludeResolver: files are keyed by
// their logical name directly (all test fixtures use flat, unique
// names, so fromFile-relative resolution isn't needed here) -- good
// enough to exercise Preprocess's include handling without touching a
// real filesystem.
type fakeResolver struct {
	files map[string]string
}

func (r *fakeResolver) Resolve(includedPath, fromFile string) (text, resolvedPath string, err error) {
	t, ok := r.files[includedPath]
	if !ok {
		return "", "", fmt.Errorf("no such file: %s", includedPath)
	}
	return t, includedPath, nil
}

func TestIncludeBasic(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"leaf.svh": "module leaf; endmodule"}}
	toks, errs := Preprocess("root.sv", "`include \"leaf.svh\"", r)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "module", "leaf", ";", "endmodule")
}

func TestIncludedFileTokensCarryItsOwnFile(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"leaf.svh": "module leaf; endmodule"}}
	toks, _ := Preprocess("root.sv", "`include \"leaf.svh\"", r)
	for _, tok := range toks {
		if tok.File != "leaf.svh" {
			t.Errorf("expected token %q to be attributed to leaf.svh, got File=%q", tok.Text, tok.File)
		}
	}
}

func TestIncludedFileOwnDirectivesAreProcessed(t *testing.T) {
	r := &fakeResolver{files: map[string]string{
		"leaf.svh": "`define WIDTH 8\nlogic [`WIDTH-1:0] data;",
	}}
	toks, errs := Preprocess("root.sv", "`include \"leaf.svh\"", r)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "logic", "[", "8", "-", "1", ":", "0", "]", "data", ";")
}

func TestNestedInclude(t *testing.T) {
	r := &fakeResolver{files: map[string]string{
		"mid.svh":  "`include \"leaf.svh\"",
		"leaf.svh": "module leaf; endmodule",
	}}
	toks, errs := Preprocess("root.sv", "`include \"mid.svh\"", r)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "module", "leaf", ";", "endmodule")
}

func TestIncludeMacroDefinedInIncludedFileUsableAfterInclude(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"defs.svh": "`define WIDTH 8"}}
	toks, errs := Preprocess("root.sv", "`include \"defs.svh\"\nlogic [`WIDTH-1:0] data;", r)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "logic", "[", "8", "-", "1", ":", "0", "]", "data", ";")
}

func TestIncludeSameFileTwiceFromUnrelatedPlacesIsFine(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"leaf.svh": "x"}}
	toks, errs := Preprocess("root.sv", "`include \"leaf.svh\"\n`include \"leaf.svh\"", r)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors (re-including a file from two unrelated places must not trip cycle detection): %+v", errs)
	}
	assertTexts(t, toks, "x", "x")
}

func TestIncludeSelfCycleDetected(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"a.svh": "`include \"a.svh\""}}
	_, errs := Preprocess("root.sv", "`include \"a.svh\"", r)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one cyclic-include error, got %+v", errs)
	}
}

func TestIncludeMutualCycleDetected(t *testing.T) {
	r := &fakeResolver{files: map[string]string{
		"a.svh": "`include \"b.svh\"",
		"b.svh": "`include \"a.svh\"",
	}}
	_, errs := Preprocess("root.sv", "`include \"a.svh\"", r)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one cyclic-include error, got %+v", errs)
	}
}

func TestIncludeResolverErrorRecordedNotFatal(t *testing.T) {
	r := &fakeResolver{files: map[string]string{}}
	toks, errs := Preprocess("root.sv", "`include \"missing.svh\"\nafter", r)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
	assertTexts(t, toks, "after")
}

func TestIncludeAngleBracketFormNotSupported(t *testing.T) {
	toks, errs := Preprocess("root.sv", "`include <foo.svh>\nafter", nil)
	if len(errs) != 1 {
		t.Fatalf("expected one error for the unsupported <path> form, got %+v", errs)
	}
	// '<', foo, '.', svh, '>' are all left as ordinary tokens afterward --
	// `include just didn't consume them as its own argument.
	if len(toks) == 0 {
		t.Fatalf("expected some passthrough tokens")
	}
}

func TestIncludeWithoutResolverRecordsError(t *testing.T) {
	_, errs := Preprocess("root.sv", "`include \"leaf.svh\"", nil)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
}

func TestSourceMapThroughIncludeAndMacro(t *testing.T) {
	r := &fakeResolver{files: map[string]string{"defs.svh": "`define WIDTH 8"}}
	toks, _ := Preprocess("root.sv", "`include \"defs.svh\"\nlogic [`WIDTH-1:0] data;", r)
	widthTok := toks[2] // "8"
	if widthTok.MacroName != "WIDTH" {
		t.Fatalf("expected MacroName WIDTH, got %+v", widthTok)
	}
	if widthTok.File != "defs.svh" {
		t.Fatalf("expected the expansion to be attributed to defs.svh (where WIDTH was defined), got %q", widthTok.File)
	}
	if widthTok.InvokedFrom == nil || widthTok.InvokedFrom.File != "root.sv" {
		t.Fatalf("expected InvokedFrom to point at the invocation in root.sv, got %+v", widthTok.InvokedFrom)
	}
}
