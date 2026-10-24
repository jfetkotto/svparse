package parser

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

// TestEveryNodeSpanIsWellFormedAcrossTheCorpus checks every node Walk can
// reach, in all vendored files: its end must not precede its start. The older
// TestSVTestsCorpusSpansAreWellFormed only looks at declarations, which let
// ParamOverride and PortConnection through with a zero End for a long time.
func TestEveryNodeSpanIsWellFormedAcrossTheCorpus(t *testing.T) {
	nodes := 0
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(corpusDir, path)
		toks, _ := preprocessor.Preprocess(rel, string(data), corpusIncludeResolver{})
		f, _ := Parse(rel, toks)
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			nodes++
			p := n.Pos()
			if p.EndLine < p.Line || (p.EndLine == p.Line && p.EndCharacter < p.Character) {
				t.Errorf("%s: %T has a backwards span %d:%d to %d:%d", rel, n, p.Line, p.Character, p.EndLine, p.EndCharacter)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if nodes < 5000 {
		t.Fatalf("only %d nodes checked: the walk is broken", nodes)
	}
}

// textAt returns the source text a position covers (single-line ASCII source).
func textAt(src string, p ast.Position) string {
	lines := strings.Split(src, "\n")
	if p.Line != p.EndLine || p.Line >= len(lines) || p.EndCharacter > len(lines[p.Line]) {
		return fmt.Sprintf("<span %d:%d-%d:%d is not on one line>", p.Line, p.Character, p.EndLine, p.EndCharacter)
	}
	return lines[p.Line][p.Character:p.EndCharacter]
}

// TestSubNodeSpansCoverTheirSourceText pins what each sub-node's Position
// covers, using the source text itself so a wrong offset cannot hide. By
// convention a named override or connection is positioned on its name alone;
// positional and wildcard forms cover their tokens.
func TestSubNodeSpansCoverTheirSourceText(t *testing.T) {
	const src = "module m; leaf #(8, .W(16 + 1), .T(int)) u [3] (.*, .a(x), .b, .c(), y + 1); endmodule"
	f, errs := parseDecls(t, src)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	inst, _ := find[*ast.Instantiation](f)

	var got []string
	for _, o := range inst.ParamOverrides {
		got = append(got, "override:"+textAt(src, o.Pos()))
	}
	i := inst.Instances[0]
	got = append(got, "instance:"+textAt(src, i.Pos()))
	for _, c := range i.Connections {
		got = append(got, "connection:"+textAt(src, c.Pos()))
	}
	want := []string{
		"override:8", // positional: its tokens
		"override:W", // named: the name token only, not the dot or the value
		"override:T",
		"instance:u", // the instance name, not its dimensions or connections
		"connection:.*",
		"connection:a",
		"connection:b",
		"connection:c",
		"connection:y + 1", // positional: all its tokens
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("spans cover\n got: %q\nwant: %q", got, want)
	}
}

func TestModportPortSpansCoverTheirNames(t *testing.T) {
	// The corpus has no modports, so this is the only check on their spans.
	const src = "interface i; modport mp (input a, b, output c); endinterface"
	f, errs := parseDecls(t, src)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	mp, _ := find[*ast.Modport](f)
	var got []string
	for _, p := range mp.Ports {
		got = append(got, textAt(src, p.Pos()))
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("modport port spans cover %q, want a,b,c", got)
	}
}

func TestSubNodeSpansWithMultiLineAndMalformedInput(t *testing.T) {
	// A positional connection that spans lines still gets a forward span, and
	// malformed groups (a stray comma, an unclosed list) never get a backwards one.
	for _, src := range []string{
		"module m; leaf u (a +\n  b, c); endmodule",
		"module m; leaf #(,) u (,); endmodule",
		"module m; leaf #(.W( u (.a(; endmodule",
		"module m; leaf #(1 u (",
	} {
		f, _ := parseDecls(t, src)
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			p := n.Pos()
			if p.EndLine < p.Line || (p.EndLine == p.Line && p.EndCharacter < p.Character) {
				t.Errorf("%q: %T has a backwards span %+v", src, n, p)
			}
			return true
		})
	}
}
