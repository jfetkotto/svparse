package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

// forEachInstantiation calls f for every instantiation in the declarations,
// descending into containers, classes and packages.
func forEachInstantiation(decls []ast.Decl, f func(*ast.Instantiation)) {
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.Instantiation:
			f(d)
		case *ast.Container:
			forEachInstantiation(d.Body, f)
		case *ast.Class:
			forEachInstantiation(d.Body, f)
		case *ast.Package:
			forEachInstantiation(d.Body, f)
		}
	}
}

func backwards(p ast.Position) bool {
	return p.EndLine < p.Line || (p.EndLine == p.Line && p.EndCharacter < p.Character)
}

// A parameter override or a port connection used to set only a start, so its
// end was 0:0, a backwards range. Every one in the vendored corpus must now
// have a span that does not end before it starts.
func TestOverrideAndConnectionSpansAreWellFormedAcrossTheCorpus(t *testing.T) {
	checked := 0
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
		forEachInstantiation(f.Decls, func(in *ast.Instantiation) {
			for _, o := range in.ParamOverrides {
				checked++
				if backwards(o.Position) {
					t.Errorf("%s: override %q has a backwards span %+v", rel, o.Name, o.Position)
				}
			}
			for _, inst := range in.Instances {
				for _, c := range inst.Connections {
					checked++
					if backwards(c.Position) {
						t.Errorf("%s: connection %q has a backwards span %+v", rel, c.Name, c.Position)
					}
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 5 {
		t.Fatalf("only %d overrides and connections checked: the walk is broken", checked)
	}
}

// textAt returns the source text a single-line position covers.
func textAt(src string, p ast.Position) string {
	lines := strings.Split(src, "\n")
	if p.Line != p.EndLine || p.Line >= len(lines) || p.EndCharacter > len(lines[p.Line]) {
		return "<not on one line>"
	}
	return lines[p.Line][p.Character:p.EndCharacter]
}

func TestOverrideAndConnectionSpansCoverTheRightText(t *testing.T) {
	src := "module top; leaf #(8, .W(4)) u(.clk, .d(x[3:0]), .*, a + 1, .e()); endmodule"
	toks, _ := preprocessor.Preprocess("t.sv", src, nil)
	f, errs := Parse("t.sv", toks)
	if len(errs) != 0 {
		t.Fatalf("parse errors: %+v", errs)
	}
	var in *ast.Instantiation
	forEachInstantiation(f.Decls, func(i *ast.Instantiation) { in = i })
	if in == nil {
		t.Fatal("no instantiation")
	}
	var overrides, conns []string
	for _, o := range in.ParamOverrides {
		overrides = append(overrides, textAt(src, o.Position))
	}
	for _, c := range in.Instances[0].Connections {
		conns = append(conns, textAt(src, c.Position))
	}
	// A named entry covers its name alone; a positional one covers its tokens.
	if got := strings.Join(overrides, "|"); got != "8|W" {
		t.Errorf("overrides cover %q", got)
	}
	if got := strings.Join(conns, "|"); got != "clk|d|.*|a + 1|e" {
		t.Errorf("connections cover %q", got)
	}
}
