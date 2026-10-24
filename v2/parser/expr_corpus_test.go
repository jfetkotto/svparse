package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
)

// knownRejectedSpans lists every raw expression span in the vendored corpus
// that ParseExpr rejects, as "path [kind] tokens", one line per occurrence
// (a repeated line means the same span appears more than once in that file).
// Like the other baselines in this repository it changes only on purpose: a
// new line is a regression in the expression parser, a vanished one means a
// fix landed and the line must be removed with it.
//
// Every entry is accounted for; none is a defect in the expression grammar:
//   - wildcard dimension "[*]" of an associative array: not an expression;
//     dimension parsing (feature/expr-fields) handles it.
//   - a macro that was never defined (it needs the file's :defines: metadata).
var knownRejectedSpans = []string{
	"chapter-5/5.6.4--compiler-directives-preprocessor-macro_1.sv [var-init] VAR_1 + VAR_2",
	"chapter-7/arrays/associative/other.sv [dim] *",
	"chapter-7/arrays/associative/wildcard.sv [dim] *",
	"generic/class/class_test_55.sv [dim] *",
	"generic/class/class_test_55.sv [dim] *",
	"generic/class/class_test_55.sv [dim] *",
	"generic/class/class_test_55.sv [dim] *",
}

// TestParseExprOnCorpusSpans runs ParseExpr over every expression the
// declaration parser captured as a raw span (initializers, defaults,
// dimension bounds, overrides, port connections) across the whole vendored
// corpus: real expressions written by real authors, which no hand-written
// table covers.
func TestParseExprOnCorpusSpans(t *testing.T) {
	var rejected []string
	total := 0
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(corpusDir, path)
		rel = filepath.ToSlash(rel)
		toks, _ := preprocessor.Preprocess(rel, string(data), corpusIncludeResolver{})
		f, _ := Parse(rel, toks)

		check := func(kind string, span []preprocessor.Token) {
			if len(span) == 0 {
				return
			}
			total++
			e, errs := ParseExpr(span)
			if e == nil {
				t.Errorf("%s [%s]: nil tree", rel, kind)
			}
			if len(errs) > 0 {
				texts := make([]string, len(span))
				for i, tok := range span {
					texts[i] = tok.Text
				}
				rejected = append(rejected, rel+" ["+kind+"] "+strings.Join(texts, " "))
			}
		}
		dims := func(kind string, ds []ast.Dim) {
			for _, d := range ds {
				check(kind, d.Left)
				check(kind, d.Right)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Variable:
				check("var-init", n.Initial)
				dims("dim", n.UnpackedDims)
			case *ast.Parameter:
				check("param-default", n.Default)
			case *ast.EnumMember:
				check("enum-value", n.Value)
			case *ast.Port:
				check("port-default", n.Default)
			case *ast.Arg:
				check("arg-default", n.Default)
			case *ast.ParamOverride:
				check("override", n.Value)
			case *ast.PortConnection:
				check("connection", n.Expr)
			case *ast.Type:
				dims("dim", n.PackedDims)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total < 1500 {
		t.Fatalf("only %d spans found: the extraction is broken", total)
	}

	sort.Strings(rejected)
	want := append([]string(nil), knownRejectedSpans...)
	sort.Strings(want)
	if strings.Join(rejected, "\n") != strings.Join(want, "\n") {
		t.Errorf("rejected spans differ from knownRejectedSpans (%d of %d spans rejected)\n got:\n  %s\nwant:\n  %s",
			len(rejected), total, strings.Join(rejected, "\n  "), strings.Join(want, "\n  "))
	}
}
