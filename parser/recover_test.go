package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

// containerNames returns the name of every top-level container in f, so a
// recovery test can assert "the declaration AFTER the broken one survived"
// without caring how many errors the broken one produced.
func containerNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		if c, ok := d.(*ast.Container); ok {
			out = append(out, c.Name)
		}
	}
	return out
}

func assertRecoveredTo(t *testing.T, src, wantName string) {
	t.Helper()
	f, errs := parseSrc(t, src)
	if len(errs) == 0 {
		t.Fatalf("expected at least one error for malformed input, got none")
	}
	names := containerNames(f)
	for _, n := range names {
		if n == wantName {
			return
		}
	}
	t.Fatalf("declaration %q was swallowed by error recovery; surviving containers: %v (errors: %+v)", wantName, names, errs)
}

// A declaration that consumes tokens and then reports failure must not let
// parseBody's recover() run: recover unconditionally eats the token at the
// cursor, which by then is the NEXT declaration's first token, and skips to
// the following ';' -- deleting a whole module from the parse result. These
// are all realistic mid-edit states.

func TestRecoverTruncatedVariableDeclKeepsNextModule(t *testing.T) {
	assertRecoveredTo(t, "logic\nmodule top;\nendmodule\n", "top")
}

func TestRecoverEmptyVariableDeclKeepsNextModule(t *testing.T) {
	assertRecoveredTo(t, "logic ;\nmodule top;\nendmodule\n", "top")
}

func TestRecoverTruncatedInstantiationKeepsNextModule(t *testing.T) {
	assertRecoveredTo(t, "module a;\n  leaf\nendmodule\nmodule top;\nendmodule\n", "top")
}

func TestRecoverTruncatedTypedefStructKeepsNextModule(t *testing.T) {
	assertRecoveredTo(t, "typedef struct packed {\n  logic [7:0] a;\n}\nmodule top;\nendmodule\n", "top")
}

func TestRecoverTruncatedVirtualInterfaceKeepsNextModule(t *testing.T) {
	assertRecoveredTo(t, "class c;\n  virtual my_if\nendclass\nmodule top;\nendmodule\n", "top")
}
