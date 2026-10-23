package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

// lastSubroutineDump parses src, which must be error-free, and returns the Dump
// of the last function or task in it (the out-of-block method, in a file that
// declares a prototype first).
func lastSubroutineDump(t *testing.T, src string) string {
	t.Helper()
	f, errs := parseDecls(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	var last ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.Function, *ast.Task:
			last = n
		}
		return true
	})
	if last == nil {
		t.Fatal("no function or task found")
	}
	return ast.Dump(last)
}

func TestFunctionAndTaskBodies(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"function with locals and statements",
			"module m; function automatic int f(input int x); int t; t = x * 2; if (t > 9) return 9; return t; endfunction endmodule",
			"(function f <int>\n  (arg input x <int>)\n  (variable t <int>)\n  (assign = t (* x 2))\n  (if cond=(> t 9)\n    (return 9))\n  (return t))\n"},
		{"task with timing",
			"module m; task wait_then(input int n); repeat (n) @(posedge clk); done = 1; endtask endmodule",
			"(task wait_then\n  (arg input n <int>)\n  (repeat count=n\n    (timed @(posedge clk)\n      (null)))\n  (assign = done 1))\n"},
		{"empty body", "module m; function void f(); endfunction endmodule", "(function f <void>)\n"},
		{"body in a begin block",
			"module m; function int f(); begin return 1; end endfunction endmodule",
			"(function f <int>\n  (block begin\n    (return 1)))\n"},
		{"constructor calling super",
			"class c extends b; function new(int v); super.new(v); this.v = v; endfunction endclass",
			"(function new\n  (arg v <int>)\n  (expr (call (. super new) v))\n  (assign = (. this v) v))\n"},
		{"out-of-block method",
			"class c; extern function void m(); endclass function void c::m(); x = 1; endfunction",
			"(function m <void>\n  (assign = x 1))\n"},
		{"local typedef and parameter",
			"module m; function int f(); typedef int word; localparam K = 3; word w; w = K; return w; endfunction endmodule",
			"(function f <int>\n  (typedef word\n    (type-alias <int>))\n  (localparam K = 3)\n  (variable w <word>)\n  (assign = w K)\n  (return w))\n"},
	}
	for _, c := range cases {
		if got := lastSubroutineDump(t, c.src); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

func TestNonANSIArgumentsDeclaredInTheBodyBecomeArgs(t *testing.T) {
	// "function f; input int a, b; output int o; ..." declares its arguments in
	// the body. They must come back as Args, after any written in the header.
	f, errs := parseDecls(t, `module m;
  function int old_style;
    input int a, b;
    input [3:0] c;
    output int o;
    old_style = a + b;
  endfunction
  function int mixed(input int x);
    input int y;
    mixed = x + y;
  endfunction
endmodule`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	got := map[string][]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if fn, ok := n.(*ast.Function); ok {
			for _, a := range fn.Args {
				got[fn.Name] = append(got[fn.Name], string(a.Direction)+" "+a.Name)
			}
			if len(fn.Stmts) != 1 {
				t.Errorf("%s: %d statements, want 1 (the declarations are arguments, not statements)", fn.Name, len(fn.Stmts))
			}
		}
		return true
	})
	if strings.Join(got["old_style"], ",") != "input a,input b,input c,output o" {
		t.Errorf("old_style args = %v", got["old_style"])
	}
	if strings.Join(got["mixed"], ",") != "input x,input y" {
		t.Errorf("mixed args = %v: the header's come first", got["mixed"])
	}
}

func TestBodyErrorsAreSuppressedNotReported(t *testing.T) {
	// A malformed statement in a body used to be invisible (the body was
	// skipped), so it must not start producing diagnostics now. The errors are
	// kept on the parser for a later change to report.
	toks, _ := lexExpr("module m; function int f(); x = ; y = 2 z = 3; return ); endfunction logic after; endmodule")
	p := &parser{toks: toks}
	f := &ast.File{}
	f.Decls, _, _ = p.parseBody("")
	if len(p.errs) != 0 {
		t.Errorf("body errors leaked into the reported errors: %+v", p.errs)
	}
	if len(p.suppressed) == 0 {
		t.Error("expected the body's errors to be kept as suppressed")
	}
	// And the declarations after the function are untouched.
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok && v.Name == "after" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("the declaration after the function was lost")
	}
}

func TestBodyParsingCannotReadPastTheEndKeyword(t *testing.T) {
	// The body parser is limited to the tokens skipBody delimited, so an
	// unterminated statement cannot swallow what follows the function.
	for _, src := range []string{
		"module m; function f(); if (a) endfunction logic after; endmodule",
		"module m; function f(); begin endfunction logic after; endmodule",
		"module m; task t(); fork a = 1; endtask logic after; endmodule",
		"module m; function f(); case (a) endfunction logic after; endmodule",
		"module m; function f(); for (i = 0; i < 3 endfunction logic after; endmodule",
	} {
		f, _ := parseDecls(t, src)
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			if v, ok := n.(*ast.Variable); ok && v.Name == "after" {
				found = true
			}
			return true
		})
		if !found {
			t.Errorf("%q: the declaration after the subroutine was lost", src)
		}
	}
}

func TestSubroutineBodyIsFilledAndOwned(t *testing.T) {
	// Expressions inside a body get their parsed siblings, and declarators that
	// share a type each own their copy, like anywhere else in the tree.
	f, _ := parseDecls(t, "module m; function int f(); cls #(4) a, b; int c = 1 + 2; return c; endfunction endmodule")
	var c *ast.Variable
	counts := map[ast.Node]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		counts[n]++
		if v, ok := n.(*ast.Variable); ok && v.Name == "c" {
			c = v
		}
		return true
	})
	if c == nil || c.InitialExpr == nil || strings.TrimSpace(ast.Dump(c.InitialExpr)) != "(+ 1 2)" {
		t.Errorf("local variable initializer not parsed: %+v", c)
	}
	for n, k := range counts {
		if k > 1 {
			t.Errorf("%T visited %d times", n, k)
		}
	}
}

func TestInlineConstraintBlocks(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"with block", "obj.randomize() with { x > 0; x < y; };",
			"(expr (call (. obj randomize) (constraints \"x > 0 ; x < y ;\")))\n"},
		{"with iterator list and block", "ok = obj.randomize() with (v) { v inside {1, 2}; };",
			"(assign = ok (call (. obj randomize) with (paren v) (constraints \"v inside { 1 , 2 } ;\")))\n"},
		{"without call parentheses", "obj.randomize with { x == 1; };",
			"(expr (call (. obj randomize) (constraints \"x == 1 ;\")))\n"},
		{"scoped call", "std::randomize(a, b) with { a < b; };",
			"(expr (call (:: std randomize) a b (constraints \"a < b ;\")))\n"},
		{"in a condition", "if (!obj.randomize() with { x > 0; }) x = 1;",
			"(if cond=(unary ! (call (. obj randomize) (constraints \"x > 0 ;\")))\n  (assign = x 1))\n"},
		{"empty block", "obj.randomize() with { };", "(expr (call (. obj randomize) (constraints \"\")))\n"},
		{"dist inside a block keeps its braces", "obj.randomize() with { b dist { 3 := 1, 10 := 0 }; };",
			"(expr (call (. obj randomize) (constraints \"b dist { 3 := 1 , 10 := 0 } ;\")))\n"},
	}
	for _, c := range cases {
		s, errs := parseStmtSrc(c.src)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors %+v", c.name, errs)
			continue
		}
		if got := ast.Dump(s); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
		// And it prints back to something that reparses to the same tree.
		printed := ast.StmtString(s)
		s2, errs2 := parseStmtSrc(printed)
		if len(errs2) != 0 || ast.Dump(s2) != ast.Dump(s) {
			t.Errorf("%s: printed as %q, which reparses differently (%v)", c.name, printed, errs2)
		}
	}
}

func TestUnterminatedConstraintBlockDoesNotRunOn(t *testing.T) {
	// A constraint block holds no begin/end, so a block end stops the scan: the
	// missing '}' cannot swallow the rest of the function.
	f, _ := parseDecls(t, "module m; function f(); obj.randomize() with { x > 0; endfunction logic after; endmodule")
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.Variable); ok && v.Name == "after" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("the declaration after the function was lost")
	}
}

func TestForInitMayDeclareSeveralTypes(t *testing.T) {
	// LRM 12.7.1: a for initialisation holds declarations separated by commas,
	// each with its own type: "int i = 0, bit c = 1".
	s, errs := parseStmtSrc("for (int i = 0, j = 5, bit c = 1'b1, my_t d = 3; i < j; i++) ;")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	f := s.(*ast.For)
	var got []string
	for _, d := range f.InitDecls {
		v := d.(*ast.Variable)
		got = append(got, v.Type.Name+" "+v.Name)
	}
	if strings.Join(got, ", ") != "int i, int j, bit c, my_t d" {
		t.Errorf("declarations = %v", got)
	}
	// Each variable owns its type: nothing is shared between i and j.
	counts := map[ast.Node]int{}
	ast.Inspect(s, func(n ast.Node) bool {
		if n != nil {
			counts[n]++
		}
		return true
	})
	for n, k := range counts {
		if k > 1 {
			t.Errorf("%T visited %d times", n, k)
		}
	}
}
