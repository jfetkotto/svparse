package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

// TestDocCommentsReachDeclarations is the path a hover provider takes:
// preprocess keeping comments, parse, and ask the comment index what sits above
// each declaration's line.
func TestDocCommentsReachDeclarations(t *testing.T) {
	const src = `// Free-running counter.
// Wraps at 255.
module counter #(
  parameter int W = 8 // width
) (
  input logic clk
);
  /* Current value. */
  logic [7:0] q;

  // Clears the counter.
  function void reset(); endfunction

  // Undocumented: a blank line separates this from reset's helper below.

  task helper(); endtask
endmodule

/** A tagged-union struct. */
typedef struct { int a; } s_t;
`
	res := preprocessor.PreprocessDetailed("c.sv", src, nil, preprocessor.Options{KeepComments: true})
	f, errs := Parse("c.sv", res.Tokens)
	if len(errs) != 0 || len(res.Errors) != 0 {
		t.Fatalf("errors: %v %v", errs, res.Errors)
	}
	idx := preprocessor.NewCommentIndex(res.Comments)

	docs := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		var name string
		switch n := n.(type) {
		case *ast.Container:
			name = n.Name
		case *ast.Variable:
			name = n.Name
		case *ast.Function:
			name = n.Name
		case *ast.Task:
			name = n.Name
		case *ast.Parameter:
			name = n.Name
		case *ast.Typedef:
			name = n.Name
		default:
			return true
		}
		docs[name] = preprocessor.DocText(idx.Leading("c.sv", n.Pos().Line))
		return true
	})

	want := map[string]string{
		"counter": "Free-running counter.\nWraps at 255.",
		"q":       "Current value.",
		"reset":   "Clears the counter.",
		"helper":  "", // a blank line separates it from the comment above
		"W":       "", // its only comment trails it
		"s_t":     "A tagged-union struct.",
	}
	for name, w := range want {
		if got, ok := docs[name]; !ok || got != w {
			t.Errorf("doc for %s = %q (found: %v), want %q", name, got, ok, w)
		}
	}

	// And the trailing comment on the parameter's line.
	for _, d := range f.Decls {
		if c, ok := d.(*ast.Container); ok {
			for _, p := range c.Params {
				if tc, ok := idx.Trailing("c.sv", p.Pos().Line); !ok || tc.Body() != "width" {
					t.Errorf("parameter %s trailing comment = %q, %v", p.Name, tc.Text, ok)
				}
			}
		}
	}
}

func TestParsingIsUnaffectedByKeepingComments(t *testing.T) {
	const src = "// doc\nmodule m (input logic a); // t\n  logic b; /* x */\nendmodule\n"
	plainToks, _ := preprocessor.Preprocess("c.sv", src, nil)
	withToks := preprocessor.PreprocessDetailed("c.sv", src, nil, preprocessor.Options{KeepComments: true}).Tokens
	a, _ := Parse("c.sv", plainToks)
	b, _ := Parse("c.sv", withToks)
	if ast.Dump(a) != ast.Dump(b) {
		t.Errorf("the tree differs:\n%s\n%s", ast.Dump(a), ast.Dump(b))
	}
}
