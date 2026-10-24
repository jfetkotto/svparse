package diag

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/parser"
	"github.com/jfetkotto/svparse/preprocessor"
)

func TestSeverityNamesRoundTrip(t *testing.T) {
	for _, s := range []Severity{Error, Warning, Info, Hint} {
		got, ok := ParseSeverity(s.String())
		if !ok || got != s {
			t.Errorf("%v: ParseSeverity(%q) = %v, %v", s, s.String(), got, ok)
		}
	}
	if _, ok := ParseSeverity("fatal"); ok {
		t.Error("ParseSeverity accepted an unknown name")
	}
	if Severity(-1).String() != "unknown" || Severity(99).String() != "unknown" {
		t.Error("out-of-range severity should print as unknown")
	}
	if !(Error < Warning && Warning < Info && Info < Hint) {
		t.Error("severities must be ordered most severe first")
	}
}

func TestSeverityEncodesAsName(t *testing.T) {
	b, err := json.Marshal(struct{ S Severity }{Warning})
	if err != nil || string(b) != `{"S":"warning"}` {
		t.Fatalf("got %s, %v", b, err)
	}
}

func TestKnownCodesAreWellFormed(t *testing.T) {
	for _, c := range []string{CodeSyntaxError, CodePreprocessorError, CodeInternalError} {
		if _, ok := Known[c]; !ok {
			t.Errorf("code constant %q is not registered in Known", c)
		}
	}
	for code, doc := range Known {
		if !ValidCode(code) {
			t.Errorf("code %q is not kebab-case", code)
		}
		if strings.TrimSpace(doc) == "" {
			t.Errorf("code %q has no description", code)
		}
	}
}

func TestValidCode(t *testing.T) {
	for code, want := range map[string]bool{
		"syntax-error": true, "width-mismatch": true, "a": true, "rule-2": true,
		"": false, "-": false, "a--b": false, "-a": false, "a-": false, "Syntax": false, "a_b": false, "a b": false,
	} {
		if got := ValidCode(code); got != want {
			t.Errorf("ValidCode(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestFromParserKeepsEveryError(t *testing.T) {
	toks, _ := preprocessor.Preprocess("t.sv", "module m;\nlogic ;\nendmodule", nil)
	_, errs := parser.Parse("t.sv", toks)
	if len(errs) == 0 {
		t.Fatal("test source should not parse cleanly")
	}
	ds := FromParser(errs)
	if len(ds) != len(errs) {
		t.Fatalf("got %d diagnostics for %d errors", len(ds), len(errs))
	}
	for i, d := range ds {
		e := errs[i]
		if d.Code != CodeSyntaxError || d.Severity != Error || d.Message != e.Message {
			t.Errorf("diagnostic %d = %+v", i, d)
		}
		r := d.Range
		if r.File != e.File || r.Line != e.Line || r.Character != e.Character || r.EndLine != e.Line || r.EndCharacter != e.Character {
			t.Errorf("range %+v does not match error %+v", r, e)
		}
	}
}

func TestFromPreprocessor(t *testing.T) {
	_, errs := preprocessor.Preprocess("t.sv", "`endif\n", nil)
	if len(errs) == 0 {
		t.Fatal("test source should produce a preprocessor error")
	}
	ds := FromPreprocessor(errs)
	if len(ds) != len(errs) || ds[0].Code != CodePreprocessorError {
		t.Fatalf("got %+v", ds)
	}
}

func TestConvertersAcceptNilAndAnyPosition(t *testing.T) {
	if got := FromParser(nil); len(got) != 0 {
		t.Errorf("FromParser(nil) = %v", got)
	}
	if got := FromPreprocessor(nil); len(got) != 0 {
		t.Errorf("FromPreprocessor(nil) = %v", got)
	}
	got := FromParser([]parser.Error{{File: "", Line: -3, Character: 1 << 40, Message: ""}})
	_ = got[0].String() // must not panic on odd values
}

func TestStringIsOneBased(t *testing.T) {
	d := Diagnostic{Code: "x-y", Severity: Warning, Range: ast.Position{File: "a.sv", Line: 0, Character: 4}, Message: "boom"}
	if got, want := d.String(), "a.sv:1:5: warning[x-y]: boom"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSortIsByFileThenPositionThenCode(t *testing.T) {
	at := func(file string, line, char int, code, msg string) Diagnostic {
		return Diagnostic{Code: code, Range: ast.Position{File: file, Line: line, Character: char}, Message: msg}
	}
	in := []Diagnostic{
		at("b.sv", 0, 0, "a", "m"),
		at("a.sv", 2, 0, "a", "m"),
		at("a.sv", 1, 5, "b", "m"),
		at("a.sv", 1, 5, "a", "z"),
		at("a.sv", 1, 5, "a", "m"),
		at("a.sv", 1, 2, "z", "m"),
	}
	Sort(in)
	var got []string
	for _, d := range in {
		got = append(got, d.Range.File+":"+d.Code+d.Message)
	}
	want := []string{"a.sv:zm", "a.sv:am", "a.sv:az", "a.sv:bm", "a.sv:am", "b.sv:am"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestSetDeduplicatesAndSorts(t *testing.T) {
	var s Set
	d1 := Diagnostic{Code: "a-b", Range: ast.Position{File: "f.sv", Line: 3}, Message: "m"}
	d2 := Diagnostic{Code: "a-b", Range: ast.Position{File: "f.sv", Line: 1}, Message: "m"}
	if !s.Add(d1) || s.Add(d1) {
		t.Error("Add should report only the first insertion of a diagnostic")
	}
	s.AddAll([]Diagnostic{d2, d1})
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	if got := s.Sorted(); got[0].Range.Line != 1 || got[1].Range.Line != 3 {
		t.Errorf("Sorted = %v", got)
	}
	// Different end positions are different diagnostics.
	d3 := d1
	d3.Range.EndCharacter = 4
	if !s.Add(d3) {
		t.Error("a diagnostic with a different range is not a duplicate")
	}
	if s.HasErrors() != true {
		t.Error("severity zero is Error")
	}
	var empty Set
	if empty.HasErrors() || empty.Len() != 0 || len(empty.Sorted()) != 0 {
		t.Error("zero Set should be empty")
	}
}

func TestSortedReturnsACopy(t *testing.T) {
	var s Set
	s.Add(Diagnostic{Code: "a", Range: ast.Position{Line: 2}})
	s.Add(Diagnostic{Code: "a", Range: ast.Position{Line: 1}})
	out := s.Sorted()
	out[0].Message = "changed"
	if s.Sorted()[0].Message != "" {
		t.Error("mutating the result changed the Set")
	}
}
