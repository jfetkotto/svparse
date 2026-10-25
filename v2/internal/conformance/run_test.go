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

func TestRunElaborationStageDetectsSemanticErrors(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"procedural net", "module top; wire w; initial w = 1; endmodule"},
		{"duplicate module", "module top; endmodule module top; endmodule"},
		{"unknown module", "module top; nosuch u(); endmodule"},
		{"void return", "module top; function void f(); return 1; endfunction endmodule"},
		{"unknown parameter", "module top; leaf #(.NOPE(1)) u(); endmodule module leaf; endmodule"},
	} {
		res := Run(t.TempDir(), "a.sv", tc.src, ParseMetadata(":type: elaboration\n"))
		if !res.Failed || res.Ran != StageElaboration {
			t.Errorf("%s: Failed=%v Ran=%v %v", tc.name, res.Failed, res.Ran, res.Diagnostics)
		}
	}
}

func TestRunParsingOnlyDoesNotElaborate(t *testing.T) {
	// The same files are accepted when the test only exercises parsing.
	res := Run(t.TempDir(), "a.sv", "module top; wire w; initial w = 1; endmodule", ParseMetadata(":type: parsing\n"))
	if res.Failed || res.Ran != StageParsing {
		t.Errorf("Failed=%v Ran=%v %v", res.Failed, res.Ran, res.Diagnostics)
	}
}

func TestRunWarningsDoNotFailATest(t *testing.T) {
	// A port-width mismatch is only a warning in elaboration.
	src := "module top; logic [15:0] a; leaf u(.d(a)); endmodule module leaf(input [7:0] d); endmodule"
	if res := Run(t.TempDir(), "a.sv", src, ParseMetadata(":type: elaboration\n")); res.Failed {
		t.Errorf("a warning failed the run: %v", res.Diagnostics)
	}
}

func TestRunElaboratesTheNamedTopOnly(t *testing.T) {
	src := "module top; endmodule module other; nosuch u(); endmodule"
	if res := Run(t.TempDir(), "a.sv", src, ParseMetadata(":type: elaboration\n:top_module: top\n")); res.Failed {
		t.Errorf("only top should be elaborated: %v", res.Diagnostics)
	}
	if res := Run(t.TempDir(), "a.sv", src, ParseMetadata(":type: elaboration\n")); !res.Failed {
		t.Error("without :top_module: every uninstantiated module is elaborated")
	}
}

func TestRunSimulationTestsAreJudgedOnElaboration(t *testing.T) {
	res := Run(t.TempDir(), "a.sv", "module top; wire w; initial w = 1; endmodule", ParseMetadata(":type: simulation elaboration parsing\n"))
	if !res.Failed || res.Ran != StageElaboration {
		t.Errorf("Failed=%v Ran=%v", res.Failed, res.Ran)
	}
}

func TestRunDefersFailuresOnlySimulationCanShow(t *testing.T) {
	src := "module m; endmodule\n"
	sim := ParseMetadata(":should_fail_because: the assertion never holds\n:type: simulation\n")
	if res := Run(t.TempDir(), "a.sv", src, sim); res.Failed || !res.Deferred {
		t.Errorf("a simulation-only negative should be deferred: %+v", res)
	}
	elab := ParseMetadata(":should_fail_because: x\n:type: simulation elaboration\n")
	if res := Run(t.TempDir(), "a.sv", src, elab); res.Deferred {
		t.Errorf("a test that names elaboration is judged: %+v", res)
	}
	valid := ParseMetadata(":type: simulation\n")
	if res := Run(t.TempDir(), "a.sv", src, valid); res.Deferred {
		t.Errorf("a test that must pass is never deferred: %+v", res)
	}
	caught := ParseMetadata(":should_fail_because: x\n:type: simulation\n")
	if res := Run(t.TempDir(), "a.sv", "module ( ;\n", caught); !res.Failed || res.Deferred {
		t.Errorf("a failure that is detected is a failure, not deferred: %+v", res)
	}
}
