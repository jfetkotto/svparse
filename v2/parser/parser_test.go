package parser

import (
	"testing"
	"time"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

func parseSrc(t *testing.T, src string) (*ast.File, []Error) {
	t.Helper()
	toks, ppErrs := preprocessor.Preprocess("test.sv", src, nil)
	if len(ppErrs) != 0 {
		t.Fatalf("unexpected preprocessor errors: %+v", ppErrs)
	}
	return Parse("test.sv", toks)
}

func TestParseEmptyModule(t *testing.T) {
	f, errs := parseSrc(t, "module top; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected 1 top-level decl, got %d: %+v", len(f.Decls), f.Decls)
	}
	c, ok := f.Decls[0].(*ast.Container)
	if !ok {
		t.Fatalf("expected *ast.Container, got %T", f.Decls[0])
	}
	if c.ContainerKind != ast.KindModule || c.Name != "top" {
		t.Fatalf("unexpected container: %+v", c)
	}
	if len(c.Body) != 0 {
		t.Fatalf("expected an empty body, got %+v", c.Body)
	}
}

func TestParseInterfaceAndProgram(t *testing.T) {
	f, errs := parseSrc(t, "interface bus_if; endinterface\nprogram tb; endprogram")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected 2 decls, got %d", len(f.Decls))
	}
	iface := f.Decls[0].(*ast.Container)
	if iface.ContainerKind != ast.KindInterface || iface.Name != "bus_if" {
		t.Fatalf("unexpected interface: %+v", iface)
	}
	prog := f.Decls[1].(*ast.Container)
	if prog.ContainerKind != ast.KindProgram || prog.Name != "tb" {
		t.Fatalf("unexpected program: %+v", prog)
	}
}

func TestParseMacromoduleIsAModule(t *testing.T) {
	f, _ := parseSrc(t, "macromodule top; endmodule")
	c := f.Decls[0].(*ast.Container)
	if c.ContainerKind != ast.KindModule {
		t.Fatalf("expected macromodule to parse as KindModule, got %+v", c)
	}
}

func TestParseContainerWithLifetimeQualifier(t *testing.T) {
	// "module automatic m (...)" / "program automatic p" (LRM 23.2.1/
	// 24.3/26.2's optional lifetime) -- previously expectIdent choked on
	// the qualifier and the entire container was lost to recovery.
	f, errs := parseSrc(t, "module automatic m (input logic clk); endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	c := f.Decls[0].(*ast.Container)
	if c.Name != "m" || len(c.Ports) != 1 || c.Ports[0].Name != "clk" {
		t.Fatalf("unexpected container: %+v", c)
	}
}

func TestParseProgramWithLifetimeQualifier(t *testing.T) {
	f, errs := parseSrc(t, "program automatic p; endprogram")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	c := f.Decls[0].(*ast.Container)
	if c.ContainerKind != ast.KindProgram || c.Name != "p" {
		t.Fatalf("unexpected program: %+v", c)
	}
}

func TestParseInterfaceWithLifetimeQualifier(t *testing.T) {
	f, errs := parseSrc(t, "interface automatic bus_if; endinterface")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	c := f.Decls[0].(*ast.Container)
	if c.ContainerKind != ast.KindInterface || c.Name != "bus_if" {
		t.Fatalf("unexpected interface: %+v", c)
	}
}

func TestParseNestedContainers(t *testing.T) {
	// Not legal SV (modules can't nest), but the parser's job here is just
	// to prove the recursive body-parsing loop and end-keyword matching
	// work for nesting in general -- classes DO legally nest inside
	// modules via generate-like top-level declarations, and this is the
	// cheapest way to exercise nesting before class members exist.
	f, errs := parseSrc(t, "module top;\nclass foo;\nendclass\nendmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	c := f.Decls[0].(*ast.Container)
	if len(c.Body) != 1 {
		t.Fatalf("expected 1 nested decl, got %+v", c.Body)
	}
	cls, ok := c.Body[0].(*ast.Class)
	if !ok || cls.Name != "foo" {
		t.Fatalf("expected nested class foo, got %+v", c.Body[0])
	}
}

func TestParseClassVirtualAndInterfaceClass(t *testing.T) {
	f, errs := parseSrc(t, "virtual class A; endclass\ninterface class B; endclass\nvirtual interface class C; endclass")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	a := f.Decls[0].(*ast.Class)
	if !a.Virtual || a.IsInterfaceClass || a.Name != "A" {
		t.Fatalf("unexpected class A: %+v", a)
	}
	b := f.Decls[1].(*ast.Class)
	if b.Virtual || !b.IsInterfaceClass || b.Name != "B" {
		t.Fatalf("unexpected class B: %+v", b)
	}
	c := f.Decls[2].(*ast.Class)
	if !c.Virtual || !c.IsInterfaceClass || c.Name != "C" {
		t.Fatalf("unexpected class C: %+v", c)
	}
}

func TestParsePackage(t *testing.T) {
	f, errs := parseSrc(t, "package pkg; endpackage")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	pkg, ok := f.Decls[0].(*ast.Package)
	if !ok || pkg.Name != "pkg" {
		t.Fatalf("expected package pkg, got %+v", f.Decls[0])
	}
}

func TestParsePackageWithLifetimeQualifier(t *testing.T) {
	f, errs := parseSrc(t, "package static pkg; endpackage")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	pkg, ok := f.Decls[0].(*ast.Package)
	if !ok || pkg.Name != "pkg" {
		t.Fatalf("expected package pkg, got %+v", f.Decls[0])
	}
}

func TestParsePositionsAreReal(t *testing.T) {
	f, _ := parseSrc(t, "\nmodule top;\nendmodule\n")
	c := f.Decls[0].(*ast.Container)
	if c.Line != 1 || c.Character != 7 { // "module " is 7 UTF-16 columns
		t.Fatalf("expected top's name at (1, 7), got (%d, %d)", c.Line, c.Character)
	}
	if c.EndLine != 2 {
		t.Fatalf("expected endmodule on line 2, got EndLine=%d", c.EndLine)
	}
	if c.File != "test.sv" {
		t.Fatalf("expected File=test.sv, got %q", c.File)
	}
}

func TestParseMismatchedEndKeywordDoesNotConsumeIt(t *testing.T) {
	// "endclass" while looking for "endmodule" is left unconsumed --
	// top's body ends early (implicitly unterminated), but the stray
	// endclass doesn't vanish or corrupt anything after it.
	f, errs := parseSrc(t, "module top;\nendclass\nclass foo; endclass")
	if len(errs) == 0 {
		t.Fatalf("expected at least one error for the mismatched endclass")
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected top AND the later class foo to both be recovered, got %+v", f.Decls)
	}
	if _, ok := f.Decls[1].(*ast.Class); !ok {
		t.Fatalf("expected the second decl to be class foo, got %T", f.Decls[1])
	}
}

func TestParseUnterminatedContainerAtEOFRecordsError(t *testing.T) {
	f, errs := parseSrc(t, "module top;")
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected the module to still be recorded despite being unterminated, got %+v", f.Decls)
	}
}

func TestParseContainerHeaderUnterminatedAtEOFRecordsError(t *testing.T) {
	// Unlike TestParseUnterminatedContainerAtEOFRecordsError (EOF right at
	// the header's own terminating ';', a clean cut with nothing left
	// over), this drops the ';' entirely -- skipHeaderToSemiStrict must
	// notice and error rather than silently returning at EOF the way it
	// used to.
	_, errs := parseSrc(t, "module top")
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing header ';'")
	}
}

func TestParseUnrecognizedTopLevelTokenRecordsErrorAndRecovers(t *testing.T) {
	// A bare number can't start any declaration this parser recognizes
	// (unlike a bare identifier, which is now a legitimate, if perhaps
	// malformed, variable/instantiation start -- see
	// TestParseIdentifierWithTrailingGarbageRecordsError below for that
	// case instead).
	f, errs := parseSrc(t, "42;\nmodule top; endmodule")
	if len(errs) == 0 {
		t.Fatalf("expected at least one error")
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected recovery to reach the module afterward, got %+v", f.Decls)
	}
	if _, ok := f.Decls[0].(*ast.Container); !ok {
		t.Fatalf("expected the module to be parsed after recovering, got %T", f.Decls[0])
	}
}

func TestParseIdentifierWithTrailingGarbageRecordsError(t *testing.T) {
	// "bogus" reads as a plausible (if ultimately malformed) variable
	// declaration's type now that bare identifiers are recognized -- the
	// leftover "here" (no separating comma) is what actually makes this
	// malformed, and must be reported, not silently dropped.
	f, errs := parseSrc(t, "bogus nonsense here;\nmodule top; endmodule")
	if len(errs) == 0 {
		t.Fatalf("expected at least one error for the trailing \"here\"")
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected the malformed declaration plus the module, got %+v", f.Decls)
	}
	if _, ok := f.Decls[1].(*ast.Container); !ok {
		t.Fatalf("expected the module to still be parsed afterward, got %T", f.Decls[1])
	}
}

func TestParseEmptyInputProducesNoDeclsNoErrors(t *testing.T) {
	f, errs := parseSrc(t, "")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 0 {
		t.Fatalf("expected no decls, got %+v", f.Decls)
	}
}

// Regression test for a genuine infinite loop found while building this
// package: preprocessor.Preprocess's output, unlike lexer.Lex's, does
// not end with a KindEOF token (its own EOF sentinels are internal
// boundary markers, stripped from the merged output). peekAt used to
// clamp past-the-end lookups to the last real token instead of
// synthesizing EOF, so once the cursor ran past the end of a
// preprocessor-sourced token slice, peek() kept returning the same real
// (non-EOF) token forever -- parseDecl failed on it, recover "advanced"
// past the logical end without peek() ever reflecting that, and the
// loop never terminated. This doesn't need every parser test to catch
// it (they all go through preprocessor.Preprocess already, via
// parseSrc, so they'd all still hang if this regressed) -- it exists to
// name the failure mode explicitly and keep it from being deleted as
// "redundant" later.
func TestParseTerminatesOnPreprocessorOutputWithNoTrailingEOF(t *testing.T) {
	toks, ppErrs := preprocessor.Preprocess("test.sv", "module top; endmodule", nil)
	if len(ppErrs) != 0 {
		t.Fatalf("unexpected preprocessor errors: %+v", ppErrs)
	}
	done := make(chan struct{})
	go func() {
		Parse("test.sv", toks)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Parse did not terminate within 2s -- likely regressed to the infinite loop this test guards against")
	}
}

func TestParseStrayTopLevelSemicolonIsToleratedAsNoOp(t *testing.T) {
	f, errs := parseSrc(t, "module top; endmodule;\nmodule other; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected top AND other to both be parsed, got %+v", f.Decls)
	}
}

func TestParseModuleWithEndLabelDoesNotConsumeFollowingDeclaration(t *testing.T) {
	f, errs := parseSrc(t, "module top;\nendmodule : top\nmodule other; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected top AND other to both be parsed, got %+v", f.Decls)
	}
	if f.Decls[1].(*ast.Container).Name != "other" {
		t.Fatalf("expected the second decl to be module other, got %+v", f.Decls[1])
	}
}

func TestParseClassPackageInterfaceProgramWithEndLabel(t *testing.T) {
	f, errs := parseSrc(t, "class foo;\nendclass : foo\npackage pkg;\nendpackage : pkg\ninterface bus;\nendinterface : bus\nprogram tb;\nendprogram : tb")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 4 {
		t.Fatalf("expected 4 decls, got %+v", f.Decls)
	}
}

func TestFunctionAndTaskWithEndLabel(t *testing.T) {
	body := moduleBody(t, `function void foo();
endfunction : foo
task bar();
endtask : bar
logic done;`)
	if len(body) != 3 {
		t.Fatalf("expected foo, bar, and done all parsed, got %+v", body)
	}
	if v, ok := body[2].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done after the labeled ends, got %+v", body[2])
	}
}

func TestParseConstructorEndLabelIsNewKeyword(t *testing.T) {
	// "endfunction : new" -- the label repeats the constructor's name,
	// "new", which is lexed as a keyword everywhere else and must still be
	// accepted here, not just a plain identifier.
	body := moduleBody(t, `class foo;
function new ();
endfunction : new
endclass
logic done;`)
	if len(body) != 2 {
		t.Fatalf("expected the class and done both parsed, got %+v", body)
	}
	if v, ok := body[1].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done after the labeled constructor end, got %+v", body[1])
	}
}

func TestParseSkipsAttributeInstanceBeforeModule(t *testing.T) {
	f, errs := parseSrc(t, "(* optimize_power *)\nmodule top; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %+v", f.Decls)
	}
	c, ok := f.Decls[0].(*ast.Container)
	if !ok || c.Name != "top" {
		t.Fatalf("expected module top, got %+v", f.Decls[0])
	}
}

func TestParseSkipsAttributeInstanceWithValueAndNestedParens(t *testing.T) {
	body := moduleBody(t, `(* fsm_state=1 *) logic [7:0] a;
(* attr = foo(bar) *) logic [7:0] b;`)
	if len(body) != 2 {
		t.Fatalf("expected 2 variables, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "a" {
		t.Fatalf("unexpected first variable: %+v", body[0])
	}
	if v, ok := body[1].(*ast.Variable); !ok || v.Name != "b" {
		t.Fatalf("unexpected second variable: %+v", body[1])
	}
}

func TestParseSkipsMultipleConsecutiveAttributeInstances(t *testing.T) {
	f, errs := parseSrc(t, "(* attr_a *) (* attr_b=1 *)\nmodule top; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if _, ok := f.Decls[0].(*ast.Container); !ok {
		t.Fatalf("expected module top after two attribute groups, got %+v", f.Decls)
	}
}

func TestParseNeverPanicsOnArbitraryInput(t *testing.T) {
	inputs := []string{
		"", "module", "class", "endmodule", "endclass endclass endclass",
		"virtual", "virtual virtual virtual", "interface", "interface interface",
		"module module module module;", "package package;",
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parsing %q panicked: %v", in, r)
				}
			}()
			toks, _ := preprocessor.Preprocess("test.sv", in, nil)
			Parse("test.sv", toks)
		}()
	}
}

// Every error must be attributable to a real file and position. An empty
// comma group (a stray comma, an everyday mid-edit state) gives its
// sub-parser no tokens of its own to point at, and used to produce
// File "" line 0 character 0 -- a diagnostic on line 1 of no file.
func TestErrorsFromEmptyGroupsAreAttributed(t *testing.T) {
	for name, src := range map[string]string{
		"port list":       "module top(input logic clk, , output logic rst);\nendmodule\n",
		"declarator":      "module top;\n  logic a, , b;\nendmodule\n",
		"empty decl":      "module top;\n  logic ;\nendmodule\n",
		"argument list":   "module top;\n  function void f(int a, , int b);\n  endfunction\nendmodule\n",
		"enum body":       "package p;\n  typedef enum int { A, , B } e;\nendpackage\n",
		"param port list": "module top #(parameter W = 1, , parameter X = 2) ();\nendmodule\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, errs := parseSrc(t, src)
			if len(errs) == 0 {
				t.Fatalf("expected at least one error")
			}
			for _, e := range errs {
				if e.File == "" {
					t.Errorf("error not attributed to any file: %+v", e)
				}
			}
		})
	}
}
