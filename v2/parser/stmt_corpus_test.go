package parser

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/token"
)

// knownRejectedBlocks lists every initial/always/final block in the vendored
// corpus whose statement the statement parser reports an error for, as
// "path:line first error". Like the other baselines it changes only on
// purpose: a new line is a regression, a vanished one means a fix landed and
// the line must be removed with it. None is a defect in the statement grammar:
//   - four files are negative tests (:should_fail_because:) of illegal syntax,
//     which the parser is right to reject: an unterminated macro expansion, an
//     assignment used in an expression without parentheses, an illegal integer
//     literal, an illegal real literal;
//   - four depend on things outside the statement parser: a vendor macro that
//     is never defined (`SV_COV_CHECK), the unsupported predefined `__FILE__,
//     and UVM macros (`uvm_info, `uvm_error) whose library is not vendored.
var knownRejectedBlocks = []string{
	"chapter-11/11.3.6--assign_in_expr_inv.sv:22 expected ';', found \"=\"",
	"chapter-16/16.17--expect-uvm.sv:111 expected a statement, found \"uvm_error\"",
	"chapter-16/16.2--assert-final-uvm.sv:61 expected ';', found \"else\"",
	"chapter-20/20.14--coverage.sv:27 expected an expression, found \"SV_COV_CHECK\"",
	"chapter-22/22.5.1--define-expansion_21.sv:19 expected ')' to close the argument list, found \"end\"",
	"chapter-5/5.6.4--compiler-directives-debug.sv:17 expected an expression, found \"__FILE__\"",
	"chapter-5/5.7.1--integers-unsized-illegal.sv:19 expected ';', found \"af\"",
	"chapter-5/5.7.2-real-constants-illegal.sv:19 expected an expression, found \".\"",
}

// In a file that defines or includes macros, nodes built from expanded tokens
// need not nest inside their parent's span: an expanded token keeps its true
// position in the macro definition (see preprocessor.Token), and a pasted or
// stringized one is synthesised, taking a neighbour's position but its own
// longer text. Nothing on such a token reliably marks it, so those files are
// checked only for forward-pointing spans, and the rest for full nesting.

// checkSpansForward asserts only that no node's span ends before it starts,
// the property that holds even across a macro expansion.
func checkSpansForward(t *testing.T, where string, n ast.Node) {
	t.Helper()
	ast.Inspect(n, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if p := node.Pos(); p.EndLine < p.Line || (p.EndLine == p.Line && p.EndCharacter < p.Character) {
			t.Errorf("%s: %T has a backwards span %+v", where, node, p)
		}
		return true
	})
}

// TestParseStmtOnCorpusProceduralBlocks parses the statement after every
// initial, final, and always keyword in all vendored files: 700-odd real
// blocks, written by real authors, that no hand-written table covers. Each
// resulting tree must also be a well-formed tree: spans nested and forward,
// every node visited once.
func TestParseStmtOnCorpusProceduralBlocks(t *testing.T) {
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
		usesMacros := strings.Contains(string(data), "`define") || strings.Contains(string(data), "`include")
		for i, tk := range toks {
			if tk.Kind != token.KindKeyword || i+1 >= len(toks) {
				continue
			}
			switch tk.Text {
			case "initial", "final", "always", "always_comb", "always_ff", "always_latch":
			default:
				continue
			}
			total++
			p := &parser{toks: toks, pos: i + 1}
			s := p.parseStatement()
			where := fmt.Sprintf("%s:%d", rel, tk.Line+1)
			if len(p.errs) > 0 {
				rejected = append(rejected, where+" "+p.errs[0].Message)
				continue
			}
			ownTypes(s)
			fillExprs(s)
			if usesMacros {
				checkSpansForward(t, where, s)
			} else {
				checkSpans(t, where, s)
			}
			counts := map[ast.Node]int{}
			ast.Inspect(s, func(n ast.Node) bool {
				if n != nil {
					counts[n]++
				}
				return true
			})
			for n, k := range counts {
				if k > 1 {
					t.Errorf("%s: %T visited %d times", where, n, k)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total < 600 {
		t.Fatalf("only %d blocks found: the extraction is broken", total)
	}

	sort.Strings(rejected)
	want := append([]string(nil), knownRejectedBlocks...)
	sort.Strings(want)
	if strings.Join(rejected, "\n") != strings.Join(want, "\n") {
		t.Errorf("rejected blocks differ from knownRejectedBlocks (%d of %d rejected)\n got:\n  %s\nwant:\n  %s",
			len(rejected), total, strings.Join(rejected, "\n  "), strings.Join(want, "\n  "))
	}
}
