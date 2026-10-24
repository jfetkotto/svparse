package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/lint"
)

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func codes(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return strings.Join(out, ",")
}

func TestAnalyzeCollectsEveryStage(t *testing.T) {
	dir := t.TempDir()
	// A preprocessor error, a syntax error, a semantic error, an elaboration
	// error and a lint finding, from one design.
	a := write(t, dir, "a.sv", "`endif\nmodule top; wire w; initial w = 1; nosuch u(); leaf v(.d(imp), .q()); endmodule\n")
	b := write(t, dir, "b.sv", "module leaf(input d, output q); assign q = d; endmodule\nmodule ( ;\n")
	res, err := Analyze(context.Background(), Options{Files: []string{a, b}, Lint: true})
	if err != nil {
		t.Fatal(err)
	}
	got := codes(res.Diagnostics)
	for _, want := range []string{"preprocessor-error", "syntax-error", "procedural-assign-to-net", "unknown-module", "implicit-net"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if !res.HasErrors() || res.Design == nil || len(res.Files) != 2 || len(res.Comp.Files()) != 2 {
		t.Errorf("result: errors=%v design=%v files=%d", res.HasErrors(), res.Design != nil, len(res.Files))
	}
	for i := 1; i < len(res.Diagnostics); i++ {
		if diag.Less(res.Diagnostics[i], res.Diagnostics[i-1]) {
			t.Fatal("diagnostics must be sorted")
		}
	}
}

func TestAnalyzeStagesAreOptional(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "a.sv", "module top; nosuch u(); endmodule\n")
	res, err := Analyze(context.Background(), Options{Files: []string{f}})
	if err != nil || res.Design != nil || strings.Contains(codes(res.Diagnostics), "unknown-module") {
		t.Errorf("without Elaborate there is no design and no elaboration error: %v %v", err, codes(res.Diagnostics))
	}
	res, _ = Analyze(context.Background(), Options{Files: []string{f}, Elaborate: true})
	if res.Design == nil || !strings.Contains(codes(res.Diagnostics), "unknown-module") {
		t.Errorf("with Elaborate: %v", codes(res.Diagnostics))
	}
}

func TestAnalyzeIncludesDefinesAndSuppression(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "inc/w.svh", "`define W 3\n")
	f := write(t, dir, "a.sv", "`include \"w.svh\"\nmodule top(output o); logic [`W:0] v = 0; assign o = v[0]; `ifdef D\n leaf u(.d(imp), .q()); // svparse: disable=implicit-net\n `endif\nendmodule\nmodule leaf(input d, output q); assign q = d; endmodule\n")
	opts := Options{Files: []string{f}, IncludeDirs: []string{filepath.Join(dir, "inc")}, Defines: map[string]string{"D": ""}, Lint: true}
	res, err := Analyze(context.Background(), opts)
	if err != nil || len(res.Diagnostics) != 0 {
		t.Fatalf("%v %v", err, res.Diagnostics)
	}
	if len(res.Comments) == 0 {
		t.Error("comments must be kept for suppression")
	}
	// The same file without the suppression comment's effect (rule at error).
	opts.LintConfig = lint.Config{Rules: map[string]string{"implicit-net": "error"}}
	res, _ = Analyze(context.Background(), opts)
	if len(res.Diagnostics) != 0 {
		t.Errorf("a suppressed finding stays suppressed at any severity: %v", res.Diagnostics)
	}
	opts.IncludeDirs = nil
	if res, _ = Analyze(context.Background(), opts); !res.HasErrors() {
		t.Error("an unresolved include is an error")
	}
}

func TestAnalyzeUnreadableFile(t *testing.T) {
	if _, err := Analyze(context.Background(), Options{Files: []string{filepath.Join(t.TempDir(), "nope.sv")}}); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestResolverFindsAbsoluteAndNestedIncludes(t *testing.T) {
	dir := t.TempDir()
	abs := write(t, dir, "abs.svh", "wire from_abs;\n")
	write(t, dir, "sub/near.svh", "`include \"deeper.svh\"\n")
	write(t, dir, "sub/deeper.svh", "wire deep;\n")
	f := write(t, dir, "sub/a.sv", "module m;\n`include \""+abs+"\"\n`include \"near.svh\"\nendmodule\n")
	res, err := Analyze(context.Background(), Options{Files: []string{f}})
	if err != nil || res.HasErrors() {
		t.Errorf("%v %v", err, res.Diagnostics)
	}
}

// Files are handled by parallel workers but come out in the order given: the
// first definition of a duplicated module is the one from the earlier file,
// and the same run gives the same diagnostics every time.
func TestAnalyzeParallelParsingKeepsFileOrder(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 40; i++ {
		files = append(files, write(t, dir, fmt.Sprintf("f%02d.sv", i), fmt.Sprintf("module m%02d; logic x; endmodule\n", i)))
	}
	dup := write(t, dir, "dup.sv", "module m05; endmodule\n")
	files = append(files, dup)
	var first string
	for run := 0; run < 5; run++ {
		res, err := Analyze(context.Background(), Options{Files: files})
		if err != nil {
			t.Fatal(err)
		}
		for i, f := range res.Files {
			if f.Path != files[i] {
				t.Fatalf("file %d is %s, want %s", i, f.Path, files[i])
			}
		}
		var b strings.Builder
		for _, d := range res.Diagnostics {
			b.WriteString(d.String() + "\n")
		}
		if run == 0 {
			first = b.String()
			if !strings.Contains(first, "duplicate-definition") || !strings.Contains(first, "dup.sv") {
				t.Fatalf("expected the later file to be the duplicate:\n%s", first)
			}
		} else if b.String() != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", run, b.String(), first)
		}
	}
}

func TestAnalyzeMissingFileAmongManyIsAnError(t *testing.T) {
	dir := t.TempDir()
	files := []string{write(t, dir, "a.sv", "module a; endmodule\n"), filepath.Join(dir, "missing.sv"), write(t, dir, "b.sv", "module b; endmodule\n")}
	if _, err := Analyze(context.Background(), Options{Files: files}); err == nil {
		t.Fatal("expected an error for the unreadable file")
	}
}
