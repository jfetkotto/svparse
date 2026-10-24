package lint

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/elab"
	"github.com/jfetkotto/svparse/parser"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/sema"
)

// analyze runs the front end over one source and lints it with cfg.
func analyze(t testing.TB, path, src string, cfg Config) []diag.Diagnostic {
	t.Helper()
	res := preprocessor.PreprocessDetailed(path, src, nil, preprocessor.Options{KeepComments: true})
	file, errs := parser.Parse(path, res.Tokens)
	if len(errs) != 0 {
		t.Fatalf("%s: parse errors %+v", path, errs)
	}
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	design, _ := elab.Elaborate(context.Background(), comp, elab.Options{})
	return Run(context.Background(), Input{Comp: comp, Design: design, Comments: res.Comments}, cfg)
}

// implicitOnly is Config{} for the framework tests, which are about one rule:
// every other rule is switched off.
func implicitOnly() Config { return only("implicit-net") }

// analyzeAST runs the front end and returns a Context for rules' helpers.
func analyzeAST(t testing.TB, src string) *Context {
	t.Helper()
	res := preprocessor.PreprocessDetailed("t.sv", src, nil, preprocessor.Options{KeepComments: true})
	file, errs := parser.Parse("t.sv", res.Tokens)
	if len(errs) != 0 {
		t.Fatalf("parse errors %+v", errs)
	}
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	return &Context{Comp: comp, Model: comp.Model()}
}

func only(rules ...string) Config {
	keep := map[string]bool{}
	for _, r := range rules {
		keep[r] = true
	}
	cfg := Config{Rules: map[string]string{}}
	for _, r := range Rules() {
		if !keep[r.Name] {
			cfg.Rules[r.Name] = "off"
		}
	}
	return cfg
}

// unregister removes a rule a test added.
func allOff() map[string]string {
	m := map[string]string{}
	for _, r := range Rules() {
		m[r.Name] = "off"
	}
	return m
}

func withIgnore(cfg Config, globs ...string) Config {
	cfg.Ignore = globs
	return cfg
}

func unregister(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(rules, name)
	delete(diag.Known, name)
}

func TestRegistry(t *testing.T) {
	rs := Rules()
	if len(rs) == 0 {
		t.Fatal("no rules registered")
	}
	if !sort.SliceIsSorted(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name }) {
		t.Error("Rules must be sorted by name")
	}
	for _, r := range rs {
		if !diag.ValidCode(r.Name) || r.Doc == "" || r.Check == nil {
			t.Errorf("malformed rule %+v", r)
		}
		if diag.Known[r.Name] != r.Doc {
			t.Errorf("rule %q is not registered as a diagnostic code", r.Name)
		}
		if got, ok := Lookup(r.Name); !ok || got.Name != r.Name {
			t.Errorf("Lookup(%q) failed", r.Name)
		}
	}
	if _, ok := Lookup("no-such-rule"); ok {
		t.Error("Lookup of an unknown rule")
	}
}

func TestRegisterRejectsBadRules(t *testing.T) {
	good := func(*Context) {}
	for name, r := range map[string]Rule{
		"not kebab": {Name: "Bad_Name", Doc: "x", Check: good},
		"no doc":    {Name: "needs-doc", Check: good},
		"no check":  {Name: "needs-check", Doc: "x"},
		"duplicate": {Name: "implicit-net", Doc: "x", Check: good},
		"collision": {Name: "syntax-error", Doc: "x", Check: good},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Register should panic", name)
				}
			}()
			Register(r)
		}()
	}
}

func TestConfigLoadAndValidate(t *testing.T) {
	cfg, err := LoadConfig(strings.NewReader(`{"rules": {"implicit-net": "error"}, "ignore": ["third_party/**"]}`))
	if err != nil || cfg.Rules["implicit-net"] != "error" || len(cfg.Ignore) != 1 {
		t.Fatalf("%+v %v", cfg, err)
	}
	for name, src := range map[string]string{
		"unknown rule":  `{"rules": {"nonsense": "off"}}`,
		"unknown level": `{"rules": {"implicit-net": "loud"}}`,
		"unknown key":   `{"rulez": {}}`,
		"bad json":      `{"rules": `,
	} {
		if _, err := LoadConfig(strings.NewReader(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if cfg, err := LoadConfig(strings.NewReader("")); err != nil || len(cfg.Rules) != 0 {
		t.Errorf("an empty config is valid: %v %v", cfg, err)
	}
}

const implicitSrc = "module top;\n  leaf u(.d(a), .q(b));\nendmodule\nmodule leaf(input d, output q);\nendmodule\n"

func TestSeverityAndOff(t *testing.T) {
	ds := analyze(t, "t.sv", implicitSrc, implicitOnly())
	if len(ds) != 2 || ds[0].Code != "implicit-net" || ds[0].Severity != diag.Warning {
		t.Fatalf("default: %v", ds)
	}
	for level, want := range map[string]diag.Severity{"error": diag.Error, "info": diag.Info, "hint": diag.Hint} {
		cfg := implicitOnly()
		cfg.Rules["implicit-net"] = level
		ds := analyze(t, "t.sv", implicitSrc, cfg)
		if len(ds) != 2 || ds[0].Severity != want {
			t.Errorf("%s: %v", level, ds)
		}
	}
	if ds := analyze(t, "t.sv", implicitSrc, Config{Rules: allOff()}); len(ds) != 0 {
		t.Errorf("off: %v", ds)
	}
}

func TestIgnoreGlobs(t *testing.T) {
	for _, c := range []struct {
		glob, path string
		want       bool
	}{
		{"third_party/**", "third_party/x.sv", true},
		{"third_party/**", "/abs/third_party/deep/y.sv", true},
		{"third_party/**", "my_third_party/x.sv", false},
		{"*.sv", "a.sv", true},
		{"*.sv", "dir/a.sv", true}, // matches the end of the path
		{"gen/*.sv", "gen/a.sv", true},
		{"gen/*.sv", "gen/sub/a.sv", false},
		{"**/gen/*.sv", "x/y/gen/a.sv", true},
		{"a?.sv", "ab.sv", true},
		{"a?.sv", "a/.sv", false},
		{`dir/**`, `dir\x.sv`, true}, // Windows separators
	} {
		if got := (Config{Ignore: []string{c.glob}}).ignoreMatcher()(c.path); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
	if ds := analyze(t, "third_party/x.sv", implicitSrc, withIgnore(implicitOnly(), "third_party/**")); len(ds) != 0 {
		t.Errorf("an ignored file still reports: %v", ds)
	}
}

func TestSuppressionComments(t *testing.T) {
	cases := []struct {
		name, src string
		want      int // findings left
	}{
		{"none", "module top;\n  leaf u(.d(a), .q(b));\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 2},
		{"trailing", "module top;\n  leaf u(.d(a), .q(b)); // svparse: disable=implicit-net\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 0},
		{"own line applies to the next", "module top;\n  // svparse: disable=implicit-net\n  leaf u(.d(a), .q(b));\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 0},
		{"own line does not reach further", "module top;\n  // svparse: disable=implicit-net\n\n  leaf u(.d(a), .q(b));\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 2},
		{"next-line", "module top;\n  leaf u(.d(a), .q(b)); // svparse: disable-next-line=implicit-net\n  leaf v(.d(c), .q());\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 2},
		{"line", "module top;\n  // svparse: disable-line=implicit-net\n  leaf u(.d(a), .q(b));\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 2},
		{"file", "// svparse: disable-file=implicit-net\nmodule top;\n  leaf u(.d(a), .q(b));\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 0},
		{"all", "module top;\n  leaf u(.d(a), .q(b)); // svparse: disable=all\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 0},
		{"list", "module top;\n  leaf u(.d(a), .q(b)); // svparse: disable=other-rule, implicit-net\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 0},
		{"other rule", "module top;\n  leaf u(.d(a), .q(b)); // svparse: disable=other-rule\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 2},
		{"block comment", "module top;\n  leaf u(.d(a), .q(b)); /* svparse: disable=implicit-net */\nendmodule\nmodule leaf(input d, output q);\nendmodule\n", 0},
	}
	for _, c := range cases {
		if got := analyze(t, "t.sv", c.src, implicitOnly()); len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %v", c.name, len(got), c.want, got)
		}
	}
	var nilSup *Suppressions
	if nilSup.Suppressed("x", "f", 0) {
		t.Error("a nil Suppressions suppresses nothing")
	}
}

func TestARulePanicIsIsolated(t *testing.T) {
	Register(Rule{Name: "test-panics", Doc: "always fails", Severity: diag.Warning, Check: func(c *Context) { panic("boom") }})
	defer unregister("test-panics")
	ds := analyze(t, "t.sv", implicitSrc, only("implicit-net", "test-panics"))
	var internal, implicit int
	for _, d := range ds {
		switch d.Code {
		case diag.CodeInternalError:
			internal++
			if !strings.Contains(d.Message, "test-panics") || !strings.Contains(d.Message, "boom") {
				t.Errorf("message %q", d.Message)
			}
		case "implicit-net":
			implicit++
		}
	}
	if internal != 1 || implicit != 2 {
		t.Errorf("internal=%d implicit=%d: %v", internal, implicit, ds)
	}
}

func TestRunWithoutElaborationAndCancellation(t *testing.T) {
	res := preprocessor.PreprocessDetailed("t.sv", implicitSrc, nil, preprocessor.Options{})
	file, _ := parser.Parse("t.sv", res.Tokens)
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	if ds := Run(context.Background(), Input{Comp: comp}, implicitOnly()); len(ds) != 0 {
		t.Errorf("rules that need the design do nothing without it: %v", ds)
	}
	if ds := Run(nil, Input{}, Config{}); ds != nil {
		t.Errorf("no compilation: %v", ds)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	design, _ := elab.Elaborate(context.Background(), comp, elab.Options{})
	if ds := Run(ctx, Input{Comp: comp, Design: design}, Config{}); len(ds) != 0 {
		t.Errorf("a cancelled run reports nothing new: %v", ds)
	}
}

// Every rule has a testdata directory with files that must report (lines
// marked "// want") and files that must not, and the findings must be exactly
// the marked lines.
func TestRuleTestdata(t *testing.T) {
	for _, r := range Rules() {
		dir := filepath.Join("testdata", r.Name)
		files, _ := filepath.Glob(filepath.Join(dir, "*.sv"))
		if len(files) == 0 {
			t.Errorf("rule %q has no testdata in %s", r.Name, dir)
			continue
		}
		positive, negative := 0, 0
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var want []int
			sc := bufio.NewScanner(strings.NewReader(string(data)))
			for n := 1; sc.Scan(); n++ {
				if strings.Contains(sc.Text(), "// want") {
					want = append(want, n)
				}
			}
			if len(want) > 0 {
				positive++
			} else {
				negative++
			}
			var got []int
			for _, d := range analyze(t, f, string(data), only(r.Name)) {
				if d.Code != r.Name {
					t.Errorf("%s: unexpected %s", f, d)
				}
				got = append(got, d.Range.Line+1)
			}
			sort.Ints(got)
			if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
				t.Errorf("%s: findings on lines %v, want %v", f, got, want)
			}
		}
		if positive == 0 || negative == 0 {
			t.Errorf("rule %q needs both a positive and a negative testdata file (has %d, %d)", r.Name, positive, negative)
		}
	}
}

func TestPositionsAreWithinTheirFile(t *testing.T) {
	for _, d := range analyze(t, "t.sv", implicitSrc, implicitOnly()) {
		if d.Range.File != "t.sv" || d.Range.Line != 1 {
			t.Errorf("position %+v", d.Range)
		}
	}
	_ = ast.Position{}
}

func FuzzLoadConfig(f *testing.F) {
	f.Add(`{"rules": {"implicit-net": "off"}, "ignore": ["a/**", "*.sv", "x?y"]}`)
	f.Add(`{"ignore": ["(((", "**/**", "\\"]}`)
	f.Fuzz(func(t *testing.T, src string) {
		cfg, err := LoadConfig(strings.NewReader(src))
		if err == nil {
			cfg.ignoreMatcher()("some/file.sv")
		}
	})
}

func FuzzSuppressions(f *testing.F) {
	f.Add("// svparse: disable=a,b\n/* svparse: disable-file = all */ // svparse: disable-next-line=")
	f.Fuzz(func(t *testing.T, src string) {
		res := preprocessor.PreprocessDetailed("f.sv", src, nil, preprocessor.Options{KeepComments: true})
		s := ParseSuppressions(res.Comments)
		s.Suppressed("implicit-net", "f.sv", 3)
	})
}

// Every vendored file runs through every rule without panicking; the corpus is
// full of tiny test modules, so findings are plentiful but each must carry a
// registered code and a position in its file.
func TestCorpusLint(t *testing.T) {
	counts := map[string]int{}
	files := 0
	err := filepath.WalkDir("../testdata/sv-tests", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		res := preprocessor.PreprocessDetailed(path, string(data), nil, preprocessor.Options{KeepComments: true})
		file, _ := parser.Parse(path, res.Tokens)
		comp := sema.NewCompilation(sema.Options{})
		comp.AddFile(file)
		design, _ := elab.Elaborate(context.Background(), comp, elab.Options{MaxInstances: 10000, MaxSteps: 100000})
		files++
		for _, d := range Run(context.Background(), Input{Comp: comp, Design: design, Comments: res.Comments}, Config{}) {
			counts[d.Code]++
			if _, ok := diag.Known[d.Code]; !ok {
				t.Fatalf("%s: unregistered code %q", path, d.Code)
			}
			if d.Code == diag.CodeInternalError {
				t.Errorf("%s: %s", path, d.Message)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d files: %v", files, counts)
	if files < 900 {
		t.Fatalf("only %d files", files)
	}
}

// FuzzLint runs the whole front end and every rule on arbitrary source.
func FuzzLint(f *testing.F) {
	f.Add("module top(input a, output b); logic c; leaf u(.d(a), .q(x)); assign b = c; endmodule module leaf(input d, output q); endmodule")
	f.Add("module t; genvar i; for (i=0;i<2;i++) begin : g logic v; assign v = 1; end initial begin $display(g[0].v); end endmodule")
	f.Fuzz(func(t *testing.T, src string) {
		res := preprocessor.PreprocessDetailed("f.sv", src, nil, preprocessor.Options{KeepComments: true})
		file, _ := parser.Parse("f.sv", res.Tokens)
		comp := sema.NewCompilation(sema.Options{})
		comp.AddFile(file)
		design, _ := elab.Elaborate(context.Background(), comp, elab.Options{MaxDepth: 12, MaxInstances: 2000, MaxGenerateIters: 200, MaxSteps: 20000})
		for _, d := range Run(context.Background(), Input{Comp: comp, Design: design, Comments: res.Comments}, Config{}) {
			if d.Code == diag.CodeInternalError {
				t.Fatalf("a rule failed: %s", d.Message)
			}
		}
	})
}
