package sema

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
)

func parseFile(t *testing.T, path, src string) *ast.File {
	t.Helper()
	toks, _ := preprocessor.Preprocess(path, src, nil)
	f, errs := parser.Parse(path, toks)
	if len(errs) != 0 {
		t.Fatalf("%s: parse errors: %+v", path, errs)
	}
	return f
}

func compile(t *testing.T, opts Options, files map[string]string, order ...string) *Compilation {
	t.Helper()
	c := NewCompilation(opts)
	for _, p := range order {
		c.AddFile(parseFile(t, p, files[p]))
	}
	return c
}

func names(defs []*Definition) string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return strings.Join(out, ",")
}

func TestDefinitionsAndLookup(t *testing.T) {
	c := compile(t, Options{}, map[string]string{
		"a.sv": "module top; sub u(); endmodule\nmodule sub; endmodule\ninterface bus; endinterface",
		"b.sv": "package pkg; endpackage\nprogram prog; endprogram\nprimitive udp (o, i); output o; input i; table 0 : 1; endtable endprimitive\nconfig cfg; design top; endconfig",
	}, "a.sv", "b.sv")
	if got := names(c.Definitions()); got != "top,sub,bus,pkg,prog,udp,cfg" {
		t.Errorf("Definitions = %s", got)
	}
	for name, kind := range map[string]DefKind{"top": DefModule, "bus": DefInterface, "prog": DefProgram, "udp": DefUDP} {
		d, ok := c.Lookup(name)
		if !ok || d.Kind != kind || d.Name != name {
			t.Errorf("Lookup(%s) = %v, %v", name, d, ok)
		}
	}
	if d, ok := c.Package("pkg"); !ok || d.Kind != DefPackage || d.File != "b.sv" {
		t.Errorf("Package(pkg) = %v, %v", d, ok)
	}
	if _, ok := c.Lookup("pkg"); ok {
		t.Error("a package is not in the design-element name space")
	}
	if _, ok := c.Package("top"); ok {
		t.Error("a module is not in the package name space")
	}
	if d, ok := c.Config("cfg"); !ok || d.Kind != DefConfig {
		t.Errorf("Config = %v, %v", d, ok)
	}
	if _, ok := c.Lookup("missing"); ok {
		t.Error("Lookup of an unknown name")
	}
	if len(c.Diagnostics()) != 0 {
		t.Errorf("unexpected diagnostics: %v", c.Diagnostics())
	}
}

func TestDuplicateDefinitions(t *testing.T) {
	c := compile(t, Options{}, map[string]string{
		"a.sv": "module m; endmodule\npackage p; endpackage\nconfig k; design m; endconfig",
		"b.sv": "program m; endprogram\npackage p; endpackage\nconfig k; design m; endconfig\nmodule m; endmodule",
	}, "a.sv", "b.sv")
	ds := c.Diagnostics()
	if len(ds) != 4 {
		t.Fatalf("got %d diagnostics: %v", len(ds), ds)
	}
	for _, d := range ds {
		if d.Code != diag.CodeDuplicateDefinition || d.Severity != diag.Error || len(d.Related) != 1 {
			t.Errorf("bad diagnostic %+v", d)
		}
		if d.Range.File != "b.sv" || d.Related[0].Range.File != "a.sv" {
			t.Errorf("duplicate should point at b.sv with the original in a.sv: %+v", d)
		}
	}
	if !strings.Contains(ds[0].Message, `"m"`) {
		t.Errorf("message %q", ds[0].Message)
	}
	first, _ := c.Lookup("m")
	if first.File != "a.sv" || first.Kind != DefModule {
		t.Error("the first definition wins")
	}
}

func TestNameSpacesAreSeparate(t *testing.T) {
	// A module, a package and a configuration may share a name.
	c := compile(t, Options{}, map[string]string{
		"a.sv": "module x; endmodule\npackage x; endpackage\nconfig x; design x; endconfig",
	}, "a.sv")
	if len(c.Diagnostics()) != 0 {
		t.Errorf("unexpected: %v", c.Diagnostics())
	}
}

func TestUnitScopes(t *testing.T) {
	files := map[string]string{
		"a.sv": "typedef enum {RED, GREEN} color_t;\nclass C; endclass\nfunction int f(); return 1; endfunction\nparameter P = 1;\nint v;\nmodule m; endmodule",
		"b.sv": "class C; endclass\nint v;\nmodule n; endmodule",
	}
	sep := compile(t, Options{}, files, "a.sv", "b.sv")
	if len(sep.Diagnostics()) != 0 {
		t.Errorf("separate units may reuse names: %v", sep.Diagnostics())
	}
	ua, _ := sep.Unit("a.sv")
	ub, _ := sep.Unit("b.sv")
	if ua == ub || ua.Name != "a.sv" || len(sep.Units()) != 2 {
		t.Errorf("per-file units: %v %v", ua, ub)
	}
	if got := strings.Join(ua.Names(), ","); got != "color_t,RED,GREEN,C,f,P,v" {
		t.Errorf("unit a names = %s", got)
	}
	if d, ok := ua.Lookup("GREEN"); !ok {
		t.Error("enum constants are declared in the unit")
	} else if _, isMember := d.(*ast.EnumMember); !isMember {
		t.Errorf("GREEN is %T", d)
	}
	if _, ok := ua.Lookup("m"); ok {
		t.Error("a module is not a unit member")
	}

	one := compile(t, Options{SingleUnit: true}, files, "a.sv", "b.sv")
	ds := one.Diagnostics()
	if len(ds) != 2 {
		t.Fatalf("shared unit should report C and v: %v", ds)
	}
	for _, d := range ds {
		if d.Code != diag.CodeDuplicateDeclaration || d.Range.File != "b.sv" || d.Related[0].Range.File != "a.sv" {
			t.Errorf("bad diagnostic %+v", d)
		}
	}
	u1, _ := one.Unit("a.sv")
	u2, _ := one.Unit("b.sv")
	if u1 != u2 || u1.Name != "$unit" || len(u1.Files) != 2 || len(one.Units()) != 1 {
		t.Errorf("shared unit: %+v", u1)
	}
}

func TestUnitDuplicateKinds(t *testing.T) {
	c := compile(t, Options{}, map[string]string{
		"a.sv": `int x; int x;
typedef int t; typedef real t;
function void f(); endfunction task f(); endtask
typedef enum {A, B} e1; typedef enum {B, C} e2;
enum {ONE, TWO} anon; int ONE;
`,
	}, "a.sv")
	var msgs []string
	for _, d := range c.Diagnostics() {
		if d.Code != diag.CodeDuplicateDeclaration {
			t.Errorf("code %s", d.Code)
		}
		msgs = append(msgs, d.Message)
	}
	want := []string{`declaration "x" is already defined`, `declaration "t" is already defined`, `declaration "f" is already defined`,
		`declaration "B" is already defined`, `declaration "ONE" is already defined`}
	if strings.Join(msgs, "|") != strings.Join(want, "|") {
		t.Errorf("messages:\n got %q\nwant %q", msgs, want)
	}
}

func TestForwardAndOutOfBlockDeclarationsAreNotDuplicates(t *testing.T) {
	c := compile(t, Options{}, map[string]string{
		"a.sv": `typedef class C;
typedef class C;
class C; extern function int get(); extern task t(); endclass
class D; extern function int get(); endclass
function int C::get(); return 1; endfunction
function int D::get(); return 2; endfunction
task C::t(); endtask
function int get(); return 0; endfunction
import "DPI-C" function void dpi();
import "DPI-C" function void dpi();
typedef struct s_fwd; typedef enum e_fwd;
`,
	}, "a.sv")
	if ds := c.Diagnostics(); len(ds) != 0 {
		t.Errorf("unexpected diagnostics: %v", ds)
	}
	u, _ := c.Unit("a.sv")
	if got := strings.Join(u.Names(), ","); got != "C,D,get" {
		t.Errorf("unit names = %s", got)
	}
}

func TestTops(t *testing.T) {
	c := compile(t, Options{}, map[string]string{
		"a.sv": `module top; sub u0(); generate if (1) begin : g leaf u1(); end endgenerate endmodule
module sub; endmodule
module leaf; endmodule
module checker_m; endmodule
module bound_to; endmodule
bind bound_to checker_m chk ();
module other_top; endmodule
interface i; endinterface
program p; endprogram
module top2; i bus(); endmodule`,
	}, "a.sv")
	if got := names(c.Tops()); got != "top,bound_to,other_top,p,top2" {
		t.Errorf("Tops = %s", got)
	}
	// bound_to is only a bind target, not instantiated by anything: it stays a
	// top, while checker_m, which the bind instantiates, does not.
	c2 := compile(t, Options{}, map[string]string{"a.sv": "module a; b u(); endmodule\nmodule b; a v(); endmodule"}, "a.sv")
	if len(c2.Tops()) != 0 {
		t.Errorf("a cycle has no top: %v", names(c2.Tops()))
	}
}

func TestTopsIgnoreShadowedDuplicates(t *testing.T) {
	c := compile(t, Options{}, map[string]string{
		"a.sv": "module m; endmodule",
		"b.sv": "module m; endmodule",
	}, "a.sv", "b.sv")
	if got := names(c.Tops()); got != "m" {
		t.Errorf("Tops = %s, want one m", got)
	}
}

func TestAddReplacesAndRemoveDrops(t *testing.T) {
	c := compile(t, Options{}, map[string]string{"a.sv": "module m; endmodule", "b.sv": "module n; endmodule"}, "a.sv", "b.sv")
	if _, ok := c.Lookup("m"); !ok {
		t.Fatal("m")
	}
	c.AddFile(parseFile(t, "a.sv", "module m2; endmodule"))
	if _, ok := c.Lookup("m"); ok {
		t.Error("the old a.sv should be gone")
	}
	if _, ok := c.Lookup("m2"); !ok || len(c.Files()) != 2 {
		t.Errorf("replacement: %v files", len(c.Files()))
	}
	c.RemoveFile("b.sv")
	c.RemoveFile("nope.sv")
	if _, ok := c.Lookup("n"); ok || len(c.Files()) != 1 {
		t.Error("RemoveFile")
	}
	if _, ok := c.Unit("b.sv"); ok {
		t.Error("the removed file has no unit")
	}
	// Re-adding the same text must not report duplicates against itself.
	c.AddFile(parseFile(t, "a.sv", "module m2; endmodule"))
	if len(c.Diagnostics()) != 0 {
		t.Errorf("re-adding a file: %v", c.Diagnostics())
	}
}

func TestEmptyAndNilInputs(t *testing.T) {
	c := NewCompilation(Options{})
	c.AddFile(nil)
	if len(c.Definitions()) != 0 || len(c.Tops()) != 0 || len(c.Diagnostics()) != 0 || len(c.Units()) != 0 {
		t.Error("empty compilation")
	}
	c.AddFile(&ast.File{Path: "empty.sv"})
	if u, ok := c.Unit("empty.sv"); !ok || len(u.Names()) != 0 {
		t.Error("an empty file still has a unit")
	}
	var nilUnit *Unit
	if _, ok := nilUnit.Lookup("x"); ok || len(nilUnit.Names()) != 0 {
		t.Error("a nil Unit is empty")
	}
}

func TestDiagnosticsAreDeterministic(t *testing.T) {
	files := map[string]string{
		"a.sv": "module m; endmodule\nmodule m; endmodule\nmodule m; endmodule",
		"b.sv": "module m; endmodule",
	}
	var first []diag.Diagnostic
	for n := 0; n < 20; n++ {
		ds := compile(t, Options{}, files, "a.sv", "b.sv").Diagnostics()
		if first == nil {
			first = ds
			if len(ds) != 3 {
				t.Fatalf("got %d", len(ds))
			}
			continue
		}
		for i := range ds {
			if ds[i].String() != first[i].String() {
				t.Fatalf("run %d differs: %v vs %v", n, ds[i], first[i])
			}
		}
	}
}

// Every vendored file, added to one compilation in both unit modes, builds
// without panicking and yields definitions; the duplicates the many "top"
// modules cause are expected and reported, not fatal.
func TestCompilationOfTheCorpus(t *testing.T) {
	for _, single := range []bool{false, true} {
		c := NewCompilation(Options{SingleUnit: single})
		err := filepath.WalkDir("../testdata/sv-tests", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			toks, _ := preprocessor.Preprocess(path, string(data), nil)
			f, _ := parser.Parse(path, toks)
			c.AddFile(f)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Files()) < 900 || len(c.Definitions()) < 900 {
			t.Fatalf("single=%v: %d files, %d definitions", single, len(c.Files()), len(c.Definitions()))
		}
		if len(c.Diagnostics()) == 0 || len(c.Tops()) == 0 {
			t.Errorf("single=%v: expected duplicates and tops across the whole corpus", single)
		}
		for _, d := range c.Diagnostics() {
			if !diag.ValidCode(d.Code) || diag.Known[d.Code] == "" {
				t.Fatalf("diagnostic with unregistered code %q", d.Code)
			}
		}
	}
}
