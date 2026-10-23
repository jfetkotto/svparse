package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunClampsToImplementedStages(t *testing.T) {
	meta := ParseMetadata(":type: simulation elaboration parsing\n")
	res := Run(t.TempDir(), "a.sv", "module m; endmodule\n", meta)
	if res.Ran != MaxImplemented {
		t.Errorf("Ran = %v, want clamp to %v", res.Ran, MaxImplemented)
	}
	if res.Failed {
		t.Errorf("clean module failed: %v", res.Diagnostics)
	}
}

func TestRunPreprocessingOnlySkipsParser(t *testing.T) {
	// Not valid declaration syntax, but a preprocessing-only test must not
	// be judged by the parser.
	meta := ParseMetadata(":type: preprocessing\n")
	res := Run(t.TempDir(), "a.sv", "module ( ;;; endmodule\n", meta)
	if res.Failed || res.Ran != StagePreprocessing {
		t.Errorf("Failed = %v, Ran = %v, diags = %v", res.Failed, res.Ran, res.Diagnostics)
	}
}

func TestRunReportsParseErrors(t *testing.T) {
	meta := ParseMetadata(":type: parsing\n")
	res := Run(t.TempDir(), "a.sv", "module ( ;\n", meta)
	if !res.Failed || len(res.Diagnostics) == 0 {
		t.Errorf("expected a parse failure, got %+v", res)
	}
}

func TestRunDefinesReachThePreprocessor(t *testing.T) {
	src := "`ifndef FLAG\n  garbage ( {{\n`endif\nmodule m; endmodule\n"
	meta := ParseMetadata(":type: parsing\n")
	if res := Run(t.TempDir(), "a.sv", src, meta); !res.Failed {
		t.Fatal("without the define, the guarded garbage should fail")
	}
	meta = ParseMetadata(":type: parsing\n:defines: FLAG\n")
	if res := Run(t.TempDir(), "a.sv", src, meta); res.Failed {
		t.Errorf("with the define, expected clean: %v", res.Diagnostics)
	}
}

func TestRunResolvesIncludes(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sub/near.svh", "wire near;\n")
	write("inc/far.svh", "wire far;\n")
	src := "module m;\n`include \"near.svh\"\n`include \"far.svh\"\nendmodule\n"

	meta := ParseMetadata(":type: parsing\n:incdirs: inc\n")
	if res := Run(root, "sub/a.sv", src, meta); res.Failed {
		t.Errorf("sibling and :incdirs: includes should resolve: %v", res.Diagnostics)
	}
	meta = ParseMetadata(":type: parsing\n")
	if res := Run(root, "sub/a.sv", src, meta); !res.Failed {
		t.Error("far.svh is outside the file's dir and no :incdirs:, expected an unresolved-include failure")
	}
}
