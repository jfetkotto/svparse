package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(args, &o, &e)
	return code, o.String(), e.String()
}

const cleanSrc = "module top; logic a; leaf u(.d(a), .q()); endmodule\nmodule leaf(input d, output q); assign q = d; endmodule\n"
const warnSrc = "module top; leaf u(.d(nope), .q()); endmodule\nmodule leaf(input d, output q); assign q = d; endmodule\n"
const errSrc = "module top; wire w; initial w = 1; endmodule\n"

func TestExitStatuses(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.sv", cleanSrc)
	warn := write(t, dir, "warn.sv", warnSrc)
	bad := write(t, dir, "bad.sv", errSrc)
	syntax := write(t, dir, "syntax.sv", "module ( ;\n")

	for _, c := range []struct {
		name string
		args []string
		want int
	}{
		{"clean check", []string{"check", clean}, 0},
		{"clean lint", []string{"lint", clean}, 0},
		{"lint warnings pass", []string{"lint", warn}, 0},
		{"--werror fails warnings", []string{"lint", "--werror", warn}, 1},
		{"semantic error", []string{"check", bad}, 1},
		{"syntax error", []string{"check", syntax}, 1},
		{"no files", []string{"check"}, 2},
		{"unknown command", []string{"frobnicate", clean}, 2},
		{"no command", nil, 2},
		{"help", []string{"help"}, 0},
		{"missing file", []string{"check", filepath.Join(dir, "nope.sv")}, 2},
		{"bad flag", []string{"check", "--nonsense", clean}, 2},
		{"bad format", []string{"check", "--format", "xml", clean}, 2},
		{"bad nettype", []string{"check", "--default-nettype", "tri", clean}, 2},
	} {
		if code, _, _ := run(t, c.args...); code != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, code, c.want)
		}
	}
}

func TestTextOutput(t *testing.T) {
	dir := t.TempDir()
	bad := write(t, dir, "bad.sv", errSrc)
	code, out, _ := run(t, "check", bad)
	if code != 1 || !strings.Contains(out, "bad.sv:1:29: error[procedural-assign-to-net]") || !strings.Contains(out, "1 error(s), 0 warning(s)") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// Duplicate definitions print their related location indented.
	dup := write(t, dir, "dup.sv", "module m; endmodule\nmodule m; endmodule\n")
	_, out, _ = run(t, "check", dup)
	if !strings.Contains(out, "error[duplicate-definition]") || !strings.Contains(out, "  ") || !strings.Contains(out, "note: first defined here") {
		t.Errorf("related output:\n%s", out)
	}
}

func TestJSONOutput(t *testing.T) {
	dir := t.TempDir()
	warn := write(t, dir, "warn.sv", warnSrc)
	_, out, _ := run(t, "lint", "--format", "json", warn)
	var items []struct {
		File     string
		Line     int
		Column   int
		Severity string
		Code     string
		Message  string
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(items) != 1 || items[0].Code != "implicit-net" || items[0].Severity != "warning" || items[0].Line != 1 || items[0].File != warn {
		t.Errorf("items: %+v", items)
	}
	// A clean run is an empty array, not nothing.
	clean := write(t, dir, "clean.sv", cleanSrc)
	_, out, _ = run(t, "check", "--format", "json", clean)
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("clean json: %q", out)
	}
}

func TestIncludesDefinesAndTops(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "inc/defs.svh", "`define WIDTH 4\n")
	main := write(t, dir, "main.sv", "`include \"defs.svh\"\nmodule top; logic [`WIDTH-1:0] v; `ifdef EXTRA\n extra e(); `endif\nendmodule\nmodule extra; endmodule\nmodule other; nosuch u(); endmodule\n")

	// Without -I the include fails.
	if code, out, _ := run(t, "check", main); code != 1 || !strings.Contains(out, "defs.svh") {
		t.Errorf("missing include: %d\n%s", code, out)
	}
	// -I finds it; --top limits elaboration so `other` is not reported.
	if code, out, _ := run(t, "check", "-I", filepath.Join(dir, "inc"), "--top", "top", main); code != 0 {
		t.Errorf("with -I and --top: %d\n%s", code, out)
	}
	if code, out, _ := run(t, "check", "-I", filepath.Join(dir, "inc"), main); code != 1 || !strings.Contains(out, "unknown-module") {
		t.Errorf("every top is elaborated without --top: %d\n%s", code, out)
	}
	// -D reaches the preprocessor, and the elab command shows the effect.
	_, out, _ := run(t, "elab", "-I", filepath.Join(dir, "inc"), "--top", "top", "-DEXTRA", main)
	if !strings.Contains(out, "top (top)") || !strings.Contains(out, "  e (extra)") {
		t.Errorf("-DEXTRA:\n%s", out)
	}
	_, out, _ = run(t, "elab", "-I", filepath.Join(dir, "inc"), "--top", "top", main)
	if strings.Contains(out, "extra") {
		t.Errorf("without -D the ifdef branch is skipped:\n%s", out)
	}
}

func TestFlagsAfterFilesAndFileLists(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.sv", "module top; leaf u(); endmodule\n")
	b := write(t, dir, "b.sv", "module leaf; endmodule\n")
	list := write(t, dir, "files.f", "# comment\n"+a+"\n\n"+b+"\n")
	if code, out, _ := run(t, "check", a, b, "--werror"); code != 0 {
		t.Errorf("flags after files: %d\n%s", code, out)
	}
	if code, out, _ := run(t, "check", "-f", list); code != 0 {
		t.Errorf("-f: %d\n%s", code, out)
	}
	if code, _, _ := run(t, "check", "-f", filepath.Join(dir, "missing.f")); code != 2 {
		t.Error("a missing -f list is a usage error")
	}
	// "--" ends the flags: a file may start with a dash.
	dash := write(t, dir, "-odd.sv", cleanSrc)
	if code, _, _ := run(t, "check", "--", dash); code != 0 {
		t.Error("-- before a file")
	}
	// Cross-file definitions need both files.
	if code, out, _ := run(t, "check", a); code != 1 || !strings.Contains(out, "unknown-module") {
		t.Errorf("a alone: %d\n%s", code, out)
	}
}

func TestLintConfigAndRules(t *testing.T) {
	dir := t.TempDir()
	warn := write(t, dir, "warn.sv", warnSrc)
	off := write(t, dir, "off.json", `{"rules": {"implicit-net": "off"}}`)
	asError := write(t, dir, "err.json", `{"rules": {"implicit-net": "error"}}`)
	ignore := write(t, dir, "ignore.json", `{"ignore": ["warn.sv"]}`)
	badCfg := write(t, dir, "bad.json", `{"rules": {"nonsense": "off"}}`)

	if code, out, _ := run(t, "lint", "--config", off, warn); code != 0 || strings.Contains(out, "implicit-net") {
		t.Errorf("off: %d\n%s", code, out)
	}
	if code, out, _ := run(t, "lint", "--config", asError, warn); code != 1 || !strings.Contains(out, "error[implicit-net]") {
		t.Errorf("error level: %d\n%s", code, out)
	}
	if code, out, _ := run(t, "lint", "--config", ignore, warn); code != 0 || strings.Contains(out, "implicit-net") {
		t.Errorf("ignore: %d\n%s", code, out)
	}
	if code, _, e := run(t, "lint", "--config", badCfg, warn); code != 2 || !strings.Contains(e, "nonsense") {
		t.Errorf("invalid config: %d %s", code, e)
	}
	if code, _, _ := run(t, "lint", "--config", filepath.Join(dir, "missing.json"), warn); code != 2 {
		t.Error("missing config")
	}
	// check does not run the lint rules.
	if _, out, _ := run(t, "check", warn); strings.Contains(out, "implicit-net") {
		t.Errorf("check ran lint:\n%s", out)
	}
	// .svparse.json in the working directory is the default.
	write(t, dir, ".svparse.json", `{"rules": {"implicit-net": "off"}}`)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	if code, out, _ := run(t, "lint", "warn.sv"); code != 0 || strings.Contains(out, "implicit-net") {
		t.Errorf("default config: %d\n%s", code, out)
	}
	_, out, _ := run(t, "lint", "--list-rules")
	if !strings.Contains(out, "implicit-net") || !strings.Contains(out, "warning") {
		t.Errorf("--list-rules:\n%s", out)
	}
}

func TestSuppressionAndNettypeNone(t *testing.T) {
	dir := t.TempDir()
	sup := write(t, dir, "sup.sv", "module top; leaf u(.d(nope), .q()); // svparse: disable=implicit-net\nendmodule\nmodule leaf(input d, output q); assign q = d; endmodule\n")
	if _, out, _ := run(t, "lint", sup); strings.Contains(out, "implicit-net") {
		t.Errorf("suppression comment ignored:\n%s", out)
	}
	warn := write(t, dir, "warn.sv", warnSrc)
	if code, out, _ := run(t, "check", "--default-nettype", "none", warn); code != 1 || !strings.Contains(out, "undeclared-identifier") {
		t.Errorf("--default-nettype none: %d\n%s", code, out)
	}
}

func TestElabAndDumpAST(t *testing.T) {
	dir := t.TempDir()
	src := write(t, dir, "d.sv", "module top #(parameter N = 2); genvar i; for (i = 0; i < N; i++) begin : g leaf #(.W(i + 1)) u(); end endmodule\nmodule leaf #(parameter W = 1); endmodule\n")
	_, out, _ := run(t, "elab", src)
	for _, want := range []string{"top (top) N=32'sh00000002", "  g[0]", "    u (leaf) W=*32'sh00000001", "    u (leaf) W=*32'sh00000002"} {
		if !strings.Contains(out, want) {
			t.Errorf("elab output missing %q:\n%s", want, out)
		}
	}
	code, out, _ := run(t, "dump-ast", src)
	if code != 0 || !strings.Contains(out, "(file ") || !strings.Contains(out, "(module top") || !strings.Contains(out, "gen-for") {
		t.Errorf("dump-ast: %d\n%s", code, out)
	}
	// dump-ast fails only for syntax problems, not semantic ones.
	bad := write(t, dir, "bad.sv", errSrc)
	if code, out, _ := run(t, "dump-ast", bad); code != 0 || !strings.Contains(out, "(module top") {
		t.Errorf("dump-ast of semantically wrong code: %d", code)
	}
	syntax := write(t, dir, "syntax.sv", "module ( ;\n")
	if code, _, e := run(t, "dump-ast", syntax); code != 1 || !strings.Contains(e, "syntax-error") {
		t.Errorf("dump-ast of broken code: %d %s", code, e)
	}
}

func TestMFCU(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.sv", "int shared;\nmodule ma; endmodule\n")
	b := write(t, dir, "b.sv", "int shared;\nmodule mb; endmodule\n")
	if code, _, _ := run(t, "check", a, b); code != 0 {
		t.Error("separate units may reuse names")
	}
	if code, out, _ := run(t, "check", "--mfcu", a, b); code != 1 || !strings.Contains(out, "duplicate-declaration") {
		t.Errorf("--mfcu: %d\n%s", code, out)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
