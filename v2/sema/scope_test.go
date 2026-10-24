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

func model(t *testing.T, src string) (*Model, *Compilation) {
	t.Helper()
	c := NewCompilation(Options{})
	c.AddFile(parseFile(t, "t.sv", src))
	return c.Model(), c
}

func defScope(t *testing.T, m *Model, c *Compilation, name string) *Scope {
	t.Helper()
	if d, ok := c.Lookup(name); ok {
		if s, ok := m.DefinitionScope(d); ok {
			return s
		}
	}
	if d, ok := c.Package(name); ok {
		if s, ok := m.DefinitionScope(d); ok {
			return s
		}
	}
	t.Fatalf("no scope for %q", name)
	return nil
}

func mustFind(t *testing.T, s *Scope, name string, kind SymKind) *Symbol {
	t.Helper()
	r := s.Lookup(name)
	if r.Symbol == nil {
		t.Fatalf("%q not found (ambiguous: %d)", name, len(r.Ambiguous))
	}
	if r.Symbol.Kind != kind {
		t.Fatalf("%q is a %v, want %v", name, r.Symbol.Kind, kind)
	}
	return r.Symbol
}

func TestModuleScopeSymbols(t *testing.T) {
	m, c := model(t, `typedef enum {IDLE, RUN} state_t;
module top #(parameter W = 8, parameter type T = int) (input clk, output [W-1:0] q);
  wire w; logic v; genvar g;
  state_t st;
  enum {A, B} anon;
  localparam L = 1;
  function int f(int a); return a; endfunction
  task t1(); endtask
  leaf u0(), u1();
  module inner; endmodule
  modport mp (input clk);
  typedef int word_t;
  nettype real nt;
  let mx(a, b) = a > b ? a : b;
endmodule
module leaf; endmodule`)
	s := defScope(t, m, c, "top")
	want := map[string]SymKind{
		"W": SymParameter, "T": SymTypeParam, "clk": SymPort, "q": SymPort, "w": SymNet, "v": SymVariable,
		"g": SymGenvar, "st": SymVariable, "anon": SymVariable, "A": SymEnumMember, "B": SymEnumMember,
		"L": SymParameter, "f": SymFunction, "t1": SymTask, "u0": SymInstance, "u1": SymInstance,
		"inner": SymModule, "mp": SymModport, "word_t": SymTypedef, "nt": SymNettype, "mx": SymLet,
	}
	for name, kind := range want {
		sym, ok := s.Own(name)
		if !ok || sym.Kind != kind || sym.Scope != s {
			t.Errorf("%s: %v, want %v", name, sym, kind)
		}
	}
	if len(s.Symbols()) != len(want) {
		t.Errorf("scope has %d symbols, want %d", len(s.Symbols()), len(want))
	}
	if sym, _ := s.Own("u0"); sym.Target != "leaf" {
		t.Errorf("instance target = %q", sym.Target)
	}
	if sym := mustFind(t, s, "IDLE", SymEnumMember); sym.Scope.Kind != ScopeUnit {
		t.Error("an enum constant belongs to the scope of its typedef (the unit)")
	}
	if s.Kind != ScopeModule || s.Name != "top" || s.Def == nil || s.Path() != "top" {
		t.Errorf("scope: %v %q %q", s.Kind, s.Name, s.Path())
	}
	inner, _ := s.Own("inner")
	if inner.Child == nil || inner.Child.Parent != s || inner.Child.Path() != "top.inner" {
		t.Error("nested module scope")
	}
	if m.Diagnostics() != nil && len(m.Diagnostics()) != 0 {
		t.Errorf("unexpected diagnostics: %v", m.Diagnostics())
	}
}

func TestLookupWalksOutwardAndShadows(t *testing.T) {
	m, c := model(t, `int g_unit; int shadow;
module top;
  int shadow; int m_var;
  function void f(int a);
    int local_v;
    begin : named
      int deep;
    end
  endfunction
endmodule`)
	top := defScope(t, m, c, "top")
	fsym, _ := top.Own("f")
	f := fsym.Child
	if r := f.Lookup("a"); !r.Found() || r.Symbol.Kind != SymArg {
		t.Errorf("a: %+v", r)
	}
	if mustFind(t, f, "m_var", SymVariable).Scope != top {
		t.Error("function should see module variables")
	}
	if mustFind(t, f, "g_unit", SymVariable).Scope.Kind != ScopeUnit {
		t.Error("function should see $unit")
	}
	if mustFind(t, f, "shadow", SymVariable).Scope != top {
		t.Error("the module's shadow hides the unit's")
	}
	if r := top.Lookup("local_v"); r.Found() {
		t.Error("function locals are not visible outside")
	}
	named, ok := f.Own("named")
	if !ok || named.Kind != SymBlock || named.Child.Path() != "top.f.named" {
		t.Fatalf("named block: %+v", named)
	}
	if mustFind(t, named.Child, "deep", SymVariable).Scope != named.Child {
		t.Error("block local")
	}
	mustFind(t, named.Child, "local_v", SymVariable)
	if _, ok := top.Own("deep"); ok {
		t.Error("block locals do not leak")
	}
	if got, ok := m.ScopeOf(fsym.Decl); !ok || got != f {
		t.Error("ScopeOf(function)")
	}
	if r := top.Lookup("nothing"); r.Found() || len(r.Ambiguous) != 0 {
		t.Error("unknown name")
	}
}

func TestPortRedeclarationAndDuplicates(t *testing.T) {
	m, c := model(t, `module top (q, d);
  output q; input d;
  reg q;
  int x; int x;
  function void f(); endfunction
  task f(); endtask
  leaf u(); leaf u();
endmodule
module leaf; endmodule`)
	top := defScope(t, m, c, "top")
	q, _ := top.Own("q")
	if q.Kind != SymPort || q.Redeclared == nil {
		t.Errorf("q should stay a port that records its variable: %+v", q)
	}
	ds := m.Diagnostics()
	var msgs []string
	for _, d := range ds {
		if d.Code != diag.CodeDuplicateDeclaration || len(d.Related) != 1 {
			t.Errorf("bad diagnostic %+v", d)
		}
		msgs = append(msgs, d.Message)
	}
	want := []string{`variable "x" is already declared in this scope`, `task "f" is already declared in this scope`, `instance "u" is already declared in this scope`}
	if strings.Join(msgs, "|") != strings.Join(want, "|") {
		t.Errorf("messages:\n got %q\nwant %q", msgs, want)
	}
}

func TestGenerateScopes(t *testing.T) {
	m, c := model(t, `module top #(N = 2);
  genvar i;
  generate
    for (i = 0; i < N; i++) begin : loop
      wire w;
    end
    if (N > 1) begin
      wire in_if;
    end else begin : named_else
      wire in_else;
    end
    for (genvar j = 0; j < N; j++) begin
      wire in_for;
    end
    case (N)
      1: begin : one wire c1; end
      default: wire in_bare;
    endcase
  endgenerate
  wire after;
endmodule`)
	top := defScope(t, m, c, "top")
	paths := map[string]string{}
	var walk func(*Scope)
	walk = func(s *Scope) {
		for _, sym := range s.Symbols() {
			if sym.Child != nil {
				paths[sym.Name] = sym.Child.Path()
				walk(sym.Child)
			}
		}
	}
	walk(top)
	// Unnamed blocks are numbered by generate construct, whether or not the
	// other constructs are named (LRM 27.6): loop is construct 1, the if is 2.
	for name, want := range map[string]string{
		"loop": "top.loop", "genblk2": "top.genblk2", "named_else": "top.named_else", "genblk3": "top.genblk3",
		"one": "top.one", "genblk4": "top.genblk4",
	} {
		if paths[name] != want {
			t.Errorf("%s: path %q, want %q (all: %v)", name, paths[name], want, paths)
		}
	}
	if _, ok := top.Own("in_if"); ok {
		t.Error("names in a generate block are local to it")
	}
	if _, ok := top.Own("after"); !ok {
		t.Error("names after the generate construct belong to the module")
	}
	sym, rest := top.LookupPath([]string{"loop", "w"})
	if sym == nil || sym.Name != "w" || rest != nil {
		t.Errorf("loop.w = %v, %v", sym, rest)
	}
	// "genvar j" belongs to the loop block, which sees the module's i.
	g1 := top.Lookup("genblk3").Symbol.Child
	if mustFind(t, g1, "j", SymGenvar).Scope != g1 {
		t.Error("loop genvar scope")
	}
	mustFind(t, g1, "N", SymParameter)
}

const pkgSource = `package base_pkg; typedef int base_t; int shared; class BaseC; int bm; endclass endpackage
package a_pkg; int ax; int shared; typedef int at; endpackage
package b_pkg; int bx; int shared; endpackage
package re_pkg; import a_pkg::*; export a_pkg::ax; endpackage
package all_pkg; import a_pkg::*; export *::*; endpackage
package chain_pkg; import re_pkg::*; export re_pkg::*; endpackage
package hidden_pkg; import a_pkg::*; int own; endpackage
package cyc1; import cyc2::*; export cyc2::*; endpackage
package cyc2; import cyc1::*; export cyc1::*; endpackage
`

func TestPackageImports(t *testing.T) {
	m, c := model(t, pkgSource+`
module explicit_m; import a_pkg::ax; import b_pkg::shared; int ax_use; endmodule
module wild_m; import a_pkg::*; import b_pkg::bx; endmodule
module amb_m; import a_pkg::*; import b_pkg::*; int local_shared; endmodule
module local_wins; import a_pkg::*; int shared; endmodule
module nested_m; import a_pkg::*; function void f(); endfunction endmodule
module export_m; import re_pkg::*; import all_pkg::*; import chain_pkg::*; import hidden_pkg::*; endmodule
`)
	if r := defScope(t, m, c, "explicit_m").Lookup("ax"); !r.Found() || r.Via != "a_pkg" || r.Symbol.Scope.Name != "a_pkg" {
		t.Errorf("explicit import: %+v", r)
	}
	if r := defScope(t, m, c, "explicit_m").Lookup("shared"); !r.Found() || r.Via != "b_pkg" {
		t.Errorf("explicit import of shared: %+v", r)
	}
	if r := defScope(t, m, c, "explicit_m").Lookup("bx"); r.Found() {
		t.Error("an unimported name is not visible")
	}
	w := defScope(t, m, c, "wild_m")
	if r := w.Lookup("ax"); !r.Found() || r.Via != "a_pkg" {
		t.Errorf("wildcard: %+v", r)
	}
	if r := w.Lookup("bx"); !r.Found() {
		t.Error("explicit import alongside a wildcard")
	}
	if r := w.Lookup("at"); !r.Found() || !r.Symbol.Kind.IsType() {
		t.Errorf("imported typedef: %+v", r)
	}
	// Two wildcard imports that both supply "shared" make a use ambiguous.
	r := defScope(t, m, c, "amb_m").Lookup("shared")
	if r.Found() || len(r.Ambiguous) != 2 {
		t.Errorf("ambiguity: %+v", r)
	}
	if r := defScope(t, m, c, "amb_m").Lookup("ax"); !r.Found() {
		t.Error("unambiguous names still resolve")
	}
	if r := defScope(t, m, c, "local_wins").Lookup("shared"); !r.Found() || r.Via != "" || r.Symbol.Scope.Name != "local_wins" {
		t.Errorf("a local declaration hides a wildcard import: %+v", r)
	}
	f := defScope(t, m, c, "nested_m").Lookup("f").Symbol.Child
	if r := f.Lookup("ax"); !r.Found() {
		t.Error("an inner scope sees its enclosing scope's imports")
	}
	// pkg::name sees a package's own names and what it exports, not what it imports.
	e := defScope(t, m, c, "export_m")
	if r := e.LookupQualified([]string{"re_pkg", "ax"}); !r.Found() || r.Symbol.Scope.Name != "a_pkg" {
		t.Errorf("exported name: %+v", r)
	}
	if r := e.LookupQualified([]string{"re_pkg", "at"}); r.Found() {
		t.Error("an imported name that is not exported is not visible through the package")
	}
	if r := e.LookupQualified([]string{"all_pkg", "at"}); !r.Found() {
		t.Error("export *::* re-exports everything imported")
	}
	if r := e.LookupQualified([]string{"chain_pkg", "ax"}); !r.Found() {
		t.Error("export through a chain of packages")
	}
	if r := e.LookupQualified([]string{"hidden_pkg", "ax"}); r.Found() {
		t.Error("hidden_pkg exports nothing")
	}
	if r := e.LookupQualified([]string{"hidden_pkg", "own"}); !r.Found() {
		t.Error("a package's own name")
	}
	if r := e.LookupQualified([]string{"cyc1", "nothing"}); r.Found() {
		t.Error("cyclic exports must terminate")
	}
	if r := e.LookupQualified([]string{"no_such_pkg", "x"}); r.Found() {
		t.Error("unknown package")
	}
}

func TestClassMembersAndInheritance(t *testing.T) {
	m, c := model(t, pkgSource+`
class Root; int r_field; typedef int r_t; static int r_static; endclass
class Mid extends Root; int m_field; function void mf(); r_field = 1; endfunction endclass
typedef Mid MidAlias;
class Leaf extends MidAlias; int l_field; function void lf(); endfunction extern function void ext(); endclass
function void Leaf::ext(); endfunction
class FromPkg extends base_pkg::BaseC; endclass
class Cyc1 extends Cyc2; endclass
class Cyc2 extends Cyc1; endclass
class ParamBase #(type T = int) extends T; endclass
module top; endmodule`)
	top := defScope(t, m, c, "top")
	leaf := mustFind(t, top, "Leaf", SymClass).Child
	lf := leaf.syms["lf"].Child
	for _, name := range []string{"l_field", "m_field", "r_field", "r_t", "r_static", "mf"} {
		if r := lf.Lookup(name); !r.Found() {
			t.Errorf("method of Leaf should see inherited %s", name)
		}
	}
	if r := lf.Lookup("r_field"); r.Symbol.Scope.Name != "Root" {
		t.Errorf("r_field is declared in %s", r.Symbol.Scope.Name)
	}
	if r := top.Lookup("r_field"); r.Found() {
		t.Error("class members are not visible outside the class")
	}
	// Out-of-block definition: its scope nests in the class.
	ext := leaf.syms["ext"]
	if ext == nil || ext.Child == nil {
		t.Fatal("extern prototype symbol")
	}
	var oob *Scope
	for _, d := range c.Files()[0].Decls {
		if fn, ok := d.(*ast.Function); ok && fn.Scope == "Leaf" {
			oob, _ = m.ScopeOf(fn)
		}
	}
	if oob == nil || oob.Parent != leaf {
		t.Fatalf("out-of-block method scope parent = %v", oob)
	}
	if r := oob.Lookup("m_field"); !r.Found() {
		t.Error("out-of-block method sees inherited members")
	}
	if _, ok := top.Own("ext"); ok {
		t.Error("an out-of-block definition is not a module-level name")
	}
	// Qualified access.
	if r := top.LookupQualified([]string{"Leaf", "r_static"}); !r.Found() || r.Symbol.Scope.Name != "Root" {
		t.Errorf("Leaf::r_static: %+v", r)
	}
	if r := top.LookupQualified([]string{"Leaf", "r_t"}); !r.Found() || r.Symbol.Kind != SymTypedef {
		t.Errorf("Leaf::r_t: %+v", r)
	}
	if r := top.LookupQualified([]string{"MidAlias", "m_field"}); !r.Found() {
		t.Errorf("a typedef of a class qualifies like the class: %+v", r)
	}
	if r := top.LookupQualified([]string{"Leaf", "missing"}); r.Found() {
		t.Error("missing member")
	}
	if r := top.LookupQualified([]string{"FromPkg", "bm"}); !r.Found() || r.Symbol.Scope.Name != "BaseC" {
		t.Errorf("base class through a package qualifier: %+v", r)
	}
	if r := top.LookupQualified([]string{"base_pkg", "BaseC", "bm"}); !r.Found() {
		t.Errorf("pkg::Class::member: %+v", r)
	}
	if r := top.LookupQualified([]string{"Cyc1", "nothing"}); r.Found() {
		t.Error("cyclic inheritance must terminate")
	}
	if r := top.LookupQualified([]string{"ParamBase", "nothing"}); r.Found() {
		t.Error("a type-parameter base has no members")
	}
	if r := top.LookupQualified([]string{"Leaf"}); r.Found() {
		t.Error("a qualified lookup needs a qualifier")
	}
}

func TestQualifierPrecedence(t *testing.T) {
	m, c := model(t, `package shadow; int x; endpackage
class shadow; static int y; endclass
int unit_v;
module top; int unit_v; endmodule
module other; int x; endmodule`)
	top := defScope(t, m, c, "top")
	// A class named like the package hides the package for "shadow::".
	if r := top.LookupQualified([]string{"shadow", "y"}); !r.Found() || r.Symbol.Kind != SymVariable {
		t.Errorf("class hides package: %+v", r)
	}
	if r := top.LookupQualified([]string{"shadow", "x"}); r.Found() {
		t.Error("the package is hidden by the class")
	}
	// $unit:: skips the module's own declaration.
	if r := top.LookupQualified([]string{"$unit", "unit_v"}); !r.Found() || r.Symbol.Scope.Kind != ScopeUnit {
		t.Errorf("$unit::unit_v: %+v", r)
	}
	if r := top.LookupQualified([]string{"$unit", "x"}); r.Found() {
		t.Error("$unit:: does not see module names")
	}
}

func TestHierarchicalPaths(t *testing.T) {
	m, c := model(t, `typedef struct { int field; } s_t;
module top;
  s_t st; int tv;
  mid m0();
  generate if (1) begin : gb
    leaf g_leaf();
  end endgenerate
endmodule
module mid; leaf l0(); int mv; endmodule
module leaf; int lv; endmodule
module lone; int z; endmodule`)
	top := defScope(t, m, c, "top")
	cases := []struct {
		path []string
		name string
		rest []string
	}{
		{[]string{"m0", "mv"}, "mv", nil},
		{[]string{"m0", "l0", "lv"}, "lv", nil},
		{[]string{"gb", "g_leaf", "lv"}, "lv", nil},
		{[]string{"st", "field"}, "st", []string{"field"}},
		{[]string{"top", "tv"}, "tv", nil},
		{[]string{"$root", "top", "m0", "mv"}, "mv", nil},
		{[]string{"m0", "nothing"}, "m0", []string{"nothing"}},
		{[]string{"m0", "l0", "lv", "bit0"}, "lv", []string{"bit0"}},
	}
	for _, tc := range cases {
		sym, rest := top.LookupPath(tc.path)
		if sym == nil || sym.Name != tc.name || strings.Join(rest, ".") != strings.Join(tc.rest, ".") {
			t.Errorf("%v: got %v rest %v, want %s rest %v", tc.path, sym, rest, tc.name, tc.rest)
		}
	}
	if sym, rest := top.LookupPath([]string{"nowhere", "x"}); sym != nil || len(rest) != 2 {
		t.Errorf("unknown root: %v %v", sym, rest)
	}
	if sym, _ := top.LookupPath([]string{"lone", "z"}); sym == nil || sym.Name != "z" {
		t.Errorf("a top-level module is reachable by name: %v", sym)
	}
	if sym, rest := top.LookupPath(nil); sym != nil || rest != nil {
		t.Error("empty path")
	}
	if sym, _ := top.LookupPath([]string{"$root"}); sym != nil {
		t.Error("$root alone")
	}
}

func TestBlockAndLoopScopes(t *testing.T) {
	m, c := model(t, `module top;
  initial begin : init_blk
    int in_blk;
    for (int i = 0; i < 3; i++) begin
      int in_for_body;
    end
    foreach (arr[a, b]) ;
    begin int unnamed_local; end
    fork : par int fk; join
  end
  int arr[2][2];
endmodule`)
	top := defScope(t, m, c, "top")
	blk := mustFind(t, top, "init_blk", SymBlock).Child
	mustFind(t, blk, "in_blk", SymVariable)
	if _, ok := top.Own("in_blk"); ok {
		t.Error("block local leaked")
	}
	var forScope, foreachScope, unnamed *Scope
	ast.Inspect(c.Files()[0], func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.For:
			forScope, _ = m.ScopeOf(n)
		case *ast.Foreach:
			foreachScope, _ = m.ScopeOf(n)
		case *ast.Block:
			if n.Label == "" && len(n.Decls) == 1 && n.Decls[0].(*ast.Variable).Name == "unnamed_local" {
				unnamed, _ = m.ScopeOf(n)
			}
		}
		return true
	})
	if forScope == nil || forScope.Parent != blk {
		t.Fatalf("for scope %v", forScope)
	}
	mustFind(t, forScope, "i", SymVariable)
	if _, ok := blk.Own("i"); ok {
		t.Error("loop variable leaked")
	}
	if foreachScope == nil {
		t.Fatal("foreach scope")
	}
	mustFind(t, foreachScope, "a", SymLoopVar)
	mustFind(t, foreachScope, "b", SymLoopVar)
	mustFind(t, foreachScope, "arr", SymVariable)
	if unnamed == nil || unnamed.Name != "" {
		t.Error("an unnamed block with declarations opens an unnamed scope")
	}
	if par := mustFind(t, blk, "par", SymBlock); par.Child == nil {
		t.Error("named fork")
	}
}

func TestVerificationScopes(t *testing.T) {
	m, c := model(t, `module top;
  logic clk, a, b;
  covergroup cg (int w) @(posedge clk);
    cp_a : coverpoint a;
    x : cross cp_a, cp_b;
  endgroup
  property p (int n); int loc; a |-> b; endproperty
  sequence s (x); b; endsequence
  clocking cb @(posedge clk); input a; output b; endclocking
endmodule`)
	top := defScope(t, m, c, "top")
	cg := mustFind(t, top, "cg", SymCovergroup).Child
	mustFind(t, cg, "w", SymArg)
	mustFind(t, cg, "cp_a", SymCoverpoint)
	mustFind(t, cg, "x", SymCoverpoint)
	mustFind(t, cg, "clk", SymVariable) // enclosing scope
	p := mustFind(t, top, "p", SymProperty).Child
	mustFind(t, p, "n", SymArg)
	mustFind(t, p, "loc", SymVariable)
	mustFind(t, top, "s", SymSequence)
	cb := mustFind(t, top, "cb", SymClocking).Child
	mustFind(t, cb, "a", SymClockingSignal)
	if sym, rest := top.LookupPath([]string{"cb", "b"}); sym == nil || sym.Kind != SymClockingSignal || rest != nil {
		t.Errorf("cb.b = %v %v", sym, rest)
	}
}

func TestClassifyName(t *testing.T) {
	m, c := model(t, `typedef int word_t;
package p; typedef int pt; int pv; class K; typedef int kt; static int kv; endclass endpackage
class C #(type T = int); T field; endclass
module top #(parameter W = 4, parameter type PT = int);
  int var_x;
  localparam X = $bits(word_t) + $bits(var_x) + $bits(p::pt) + $bits(PT) + $bits(W) + $bits(p::K::kt) + $bits(p::pv) + $bits(nothing);
endmodule`)
	top := defScope(t, m, c, "top")
	got := map[string]NameClass{}
	ast.Inspect(c.Files()[0], func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok {
			if id, ok := call.Func.(*ast.Ident); ok && id.Name == "$bits" && len(call.Args) == 1 {
				got[ast.ExprString(call.Args[0])] = top.ClassifyName(call.Args[0])
			}
		}
		return true
	})
	want := map[string]NameClass{
		"word_t": NameType, "var_x": NameValue, "p::pt": NameType, "PT": NameType, "W": NameValue,
		"p::K::kt": NameType, "p::pv": NameValue, "nothing": NameUnknown,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: class %v, want %v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("classified %d names, want %d: %v", len(got), len(want), got)
	}
	if top.ClassifyName(&ast.Literal{Text: "1"}) != NameValue {
		t.Error("a literal is a value")
	}
	if top.ClassifyName(&ast.TypeExpr{}) != NameType {
		t.Error("a TypeExpr is a type")
	}
	if top.ClassifyName(&ast.Index{X: &ast.Ident{Name: "word_t"}}) != NameValue {
		t.Error("an index expression is a value")
	}
	if top.ClassifyName(&ast.TypeOrExpr{X: &ast.Ident{Name: "word_t"}}) != NameType {
		t.Error("TypeOrExpr is looked through")
	}
	if cl := top.ClassifyName(&ast.ParameterizedName{Name: "C"}); cl != NameType {
		t.Errorf("C#(...) = %v", cl)
	}
}

func TestModelFollowsFileChanges(t *testing.T) {
	c := NewCompilation(Options{})
	c.AddFile(parseFile(t, "a.sv", "module a; int one; endmodule"))
	m1 := c.Model()
	if c.Model() != m1 {
		t.Error("the model is cached")
	}
	c.AddFile(parseFile(t, "a.sv", "module a; int two; endmodule"))
	m2 := c.Model()
	if m2 == m1 {
		t.Fatal("the model must be rebuilt after a file changes")
	}
	d, _ := c.Lookup("a")
	s, _ := m2.DefinitionScope(d)
	if _, ok := s.Own("two"); !ok {
		t.Error("new declarations")
	}
	if _, ok := s.Own("one"); ok {
		t.Error("stale declarations")
	}
	if _, ok := m2.UnitScope("a.sv"); !ok {
		t.Error("UnitScope")
	}
	if _, ok := m2.UnitScope("missing.sv"); ok {
		t.Error("UnitScope of an unknown file")
	}
}

// Every symbol must be found again by looking its own name up from the scope
// that declares it, across all the vendored files: the basic consistency of
// the scope tree. Out-of-order duplicates keep the first declaration.
func TestCorpusScopeInvariants(t *testing.T) {
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
		m := c.Model()
		checked := 0
		var visit func(s *Scope, depth int)
		visit = func(s *Scope, depth int) {
			if depth > 200 {
				t.Fatal("scope tree too deep or cyclic")
			}
			for _, sym := range s.Symbols() {
				checked++
				if sym.Scope != s {
					t.Fatalf("%s: symbol %q records the wrong scope", s.Path(), sym.Name)
				}
				r := s.Lookup(sym.Name)
				if r.Symbol != sym && len(r.Ambiguous) == 0 {
					t.Fatalf("%s: looking up %q finds %v", s.Path(), sym.Name, r.Symbol)
				}
				if sym.Child != nil {
					if sym.Child.Parent != s && sym.Child.Parent != nil && sym.Child.Parent.Kind != ScopeClass {
						t.Fatalf("%s: child scope of %q has a different parent", s.Path(), sym.Name)
					}
					visit(sym.Child, depth+1)
				}
			}
		}
		for _, u := range c.Units() {
			us := m.units[u]
			visit(us, 0)
		}
		for _, def := range c.Definitions() {
			if s, ok := m.DefinitionScope(def); ok {
				visit(s, 0)
			}
		}
		if checked < 150 {
			t.Errorf("single=%v: only %d symbols checked", single, checked)
		}
		for _, d := range m.Diagnostics() {
			if !diag.ValidCode(d.Code) || diag.Known[d.Code] == "" {
				t.Fatalf("unregistered code %q", d.Code)
			}
		}
		// Qualified and path lookups on arbitrary input must not panic.
		for _, def := range c.Definitions() {
			if s, ok := m.DefinitionScope(def); ok {
				s.LookupQualified([]string{def.Name, "x"})
				s.LookupPath([]string{def.Name, "a", "b"})
			}
		}
	}
}

// FuzzModel builds the scope tree of arbitrary source and queries every name
// in it in every way; nothing may panic or loop.
func FuzzModel(f *testing.F) {
	f.Add("package p; class C; endclass endpackage module m; import p::*; class D extends C; endclass leaf u(); endmodule")
	f.Add("class A extends B; endclass class B extends A; endclass package x; import y::*; export *::*; endpackage package y; import x::*; export x::*; endpackage")
	f.Add("module m; generate for (genvar i=0;i<2;i++) begin : g leaf u(); end endgenerate initial begin : b int v; end endmodule module leaf; endmodule")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		file, _ := parser.Parse("f.sv", toks)
		c := NewCompilation(Options{})
		c.AddFile(file)
		m := c.Model()
		var names []string
		var visit func(s *Scope, depth int)
		visit = func(s *Scope, depth int) {
			if depth > 500 {
				return
			}
			for _, sym := range s.Symbols() {
				names = append(names, sym.Name)
				if sym.Child != nil {
					visit(sym.Child, depth+1)
				}
			}
		}
		for _, d := range c.Definitions() {
			if s, ok := m.DefinitionScope(d); ok {
				visit(s, 0)
				names = append(names, d.Name)
			}
		}
		for _, d := range c.Definitions() {
			s, ok := m.DefinitionScope(d)
			if !ok {
				continue
			}
			for _, a := range names {
				s.Lookup(a)
				s.LookupPath([]string{a})
				for _, b := range names {
					s.LookupQualified([]string{a, b})
					s.LookupPath([]string{a, b})
					if len(names) > 40 {
						break
					}
				}
			}
		}
	})
}

// The branches of one generate construct are alternatives: they may share a
// name, and unnamed ones share a number. Two separate constructs may not.
func TestGenerateBranchesMayShareNames(t *testing.T) {
	m, c := model(t, `module ok #(parameter P = 1);
  if (P) begin : g wire a; end else begin : g wire b; end
  case (P) 0: begin : h end 1: begin : h end default: begin : h end endcase
  if (P) begin wire c; end else begin wire d; end
endmodule
module bad;
  if (1) begin : g end
  if (0) begin : g end
endmodule`)
	ok := defScope(t, m, c, "ok")
	for _, name := range []string{"g", "h", "genblk3"} {
		if sym := mustFind(t, ok, name, SymGenerateBlock); sym.Child == nil {
			t.Errorf("%s has no scope", name)
		}
	}
	var dups []string
	for _, d := range m.Diagnostics() {
		dups = append(dups, d.Message)
	}
	if len(dups) != 1 || !strings.Contains(dups[0], `"g"`) {
		t.Errorf("want exactly the clash between two constructs, got %v", dups)
	}
}
