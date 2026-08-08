package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func classDecl(t *testing.T, src string) *ast.Class {
	t.Helper()
	f, errs := parseSrc(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %+v", f.Decls)
	}
	cls, ok := f.Decls[0].(*ast.Class)
	if !ok {
		t.Fatalf("expected *ast.Class, got %T", f.Decls[0])
	}
	return cls
}

func TestClassExtends(t *testing.T) {
	cls := classDecl(t, "class packet extends base_packet; endclass")
	if cls.Extends != "base_packet" {
		t.Fatalf("unexpected extends: %+v", cls)
	}
}

func TestClassExtendsQualified(t *testing.T) {
	cls := classDecl(t, "class packet extends pkg::base_packet; endclass")
	if cls.Extends != "base_packet" {
		t.Fatalf("expected the qualifier stripped, base name kept, got %+v", cls)
	}
}

func TestClassExtendsWithConstructorArgs(t *testing.T) {
	cls := classDecl(t, "class packet extends base_packet(1, 2); endclass")
	if cls.Extends != "base_packet" {
		t.Fatalf("unexpected extends: %+v", cls)
	}
	got := tokenTexts(cls.ExtendsArgs)
	want := []string{"1", ",", "2"}
	if len(got) != len(want) {
		t.Fatalf("ExtendsArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtendsArgs = %v, want %v", got, want)
		}
	}
}

func TestClassImplements(t *testing.T) {
	cls := classDecl(t, "class foo implements iface_a, iface_b; endclass")
	if len(cls.Implements) != 2 || cls.Implements[0] != "iface_a" || cls.Implements[1] != "iface_b" {
		t.Fatalf("unexpected implements: %+v", cls.Implements)
	}
}

func TestClassExtendsAndImplements(t *testing.T) {
	cls := classDecl(t, "class foo extends base implements iface_a; endclass")
	if cls.Extends != "base" || len(cls.Implements) != 1 || cls.Implements[0] != "iface_a" {
		t.Fatalf("unexpected class: %+v", cls)
	}
}

func TestClassParams(t *testing.T) {
	cls := classDecl(t, "class fifo #(int DEPTH = 16); endclass")
	if len(cls.Params) != 1 || cls.Params[0].Name != "DEPTH" {
		t.Fatalf("unexpected params: %+v", cls.Params)
	}
}

func TestClassParamsAndExtends(t *testing.T) {
	cls := classDecl(t, "class fifo #(int DEPTH = 16) extends base_fifo; endclass")
	if len(cls.Params) != 1 || cls.Extends != "base_fifo" {
		t.Fatalf("unexpected class: %+v", cls)
	}
}

func TestClassWithVariableMember(t *testing.T) {
	cls := classDecl(t, "class packet;\nlogic [7:0] payload;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	v := cls.Body[0].(*ast.Variable)
	if v.Name != "payload" {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithRandMember(t *testing.T) {
	cls := classDecl(t, "class packet;\nrand bit [7:0] payload;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if !v.IsRand || v.IsRandC || v.Name != "payload" {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithRandCMember(t *testing.T) {
	cls := classDecl(t, "class packet;\nrandc bit [7:0] id;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if !v.IsRandC || v.IsRand {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithVirtualInterfaceMember(t *testing.T) {
	// "virtual my_if vif;" (LRM 25.9) -- the "interface" keyword is
	// optional; must not be mistaken for "virtual function/task" or
	// "virtual [interface] class".
	cls := classDecl(t, "class driver;\nvirtual my_if vif;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	v, ok := cls.Body[0].(*ast.Variable)
	if !ok || v.Name != "vif" || v.Type.Name != "my_if" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithVirtualInterfaceKeywordMember(t *testing.T) {
	cls := classDecl(t, "class driver;\nvirtual interface my_if vif;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	v, ok := cls.Body[0].(*ast.Variable)
	if !ok || v.Name != "vif" || v.Type.Name != "my_if" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithVirtualInterfaceModportMember(t *testing.T) {
	// "virtual my_if.tb vif;" -- a virtual interface handle restricted to
	// one modport. The ".tb" isn't tracked on ast.Type (consumed, not
	// separately recorded), but it must not corrupt the declarator name.
	cls := classDecl(t, "class driver;\nvirtual my_if.tb vif;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	v, ok := cls.Body[0].(*ast.Variable)
	if !ok || v.Name != "vif" || v.Type.Name != "my_if" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestModuleWithVirtualInterfaceVariable(t *testing.T) {
	// The same handle declared at module scope, not just as a class
	// property.
	body := moduleBody(t, "virtual my_if vif;")
	if len(body) != 1 {
		t.Fatalf("expected 1 declaration, got %+v", body)
	}
	v, ok := body[0].(*ast.Variable)
	if !ok || v.Name != "vif" || v.Type.Name != "my_if" {
		t.Fatalf("unexpected declaration: %+v", body[0])
	}
}

func TestClassWithRandUserDefinedType(t *testing.T) {
	// "rand" is itself an unambiguous enough signal that the identifier
	// type doesn't need the general instantiation-disambiguation logic.
	cls := classDecl(t, "class env;\nrand packet_t pkt;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if !v.IsRand || v.Type.Name != "packet_t" || v.Name != "pkt" {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithStaticMember(t *testing.T) {
	cls := classDecl(t, "class counter;\nstatic int count;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if !v.IsStatic || v.IsRand || v.Name != "count" {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithLocalMember(t *testing.T) {
	cls := classDecl(t, "class packet;\nlocal int a_loc;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if v.Name != "a_loc" {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithProtectedMember(t *testing.T) {
	cls := classDecl(t, "class packet;\nprotected int a_prot;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if v.Name != "a_prot" {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithProtectedVirtualMethod(t *testing.T) {
	// The standard UVM method-visibility pattern -- qualifiers in any
	// order must not corrupt the parse (parseTypeBase used to consume a
	// stray qualifier keyword as though it were the property's type).
	cls := classDecl(t, "class driver;\nprotected virtual function void f();\nendfunction\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	fn, ok := cls.Body[0].(*ast.Function)
	if !ok || fn.Name != "f" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithVirtualProtectedMethod(t *testing.T) {
	cls := classDecl(t, "class driver;\nvirtual protected function void f();\nendfunction\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	fn, ok := cls.Body[0].(*ast.Function)
	if !ok || fn.Name != "f" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithProtectedStaticFunction(t *testing.T) {
	cls := classDecl(t, "class driver;\nprotected static function void f();\nendfunction\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	fn, ok := cls.Body[0].(*ast.Function)
	if !ok || fn.Name != "f" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithLocalStaticVariable(t *testing.T) {
	// "local static int count;" -- previously corrupted the class body
	// through end-of-file (the "int" type name got mistaken for the
	// declarator name during a botched recovery).
	cls := classDecl(t, "class counter;\nlocal static int count;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	v, ok := cls.Body[0].(*ast.Variable)
	if !ok || v.Name != "count" || !v.IsStatic {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithStaticProtectedVariable(t *testing.T) {
	// "static protected int count;" -- previously parsed but silently
	// dropped IsStatic (the "static" case only ever looked at the token
	// immediately after itself).
	cls := classDecl(t, "class counter;\nstatic protected int count;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if v.Name != "count" || !v.IsStatic {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithStaticVirtualMethod(t *testing.T) {
	cls := classDecl(t, "class driver;\nstatic virtual function void f();\nendfunction\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	fn, ok := cls.Body[0].(*ast.Function)
	if !ok || fn.Name != "f" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithProtectedVirtualInterfaceMember(t *testing.T) {
	// A qualifier preceding a virtual interface handle -- the
	// qualifier-run dispatch must still route to parseVirtualInterfaceDecl,
	// not treat "my_if" as a plain variable type.
	cls := classDecl(t, "class driver;\nprotected virtual my_if vif;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	v, ok := cls.Body[0].(*ast.Variable)
	if !ok || v.Name != "vif" || v.Type.Name != "my_if" {
		t.Fatalf("unexpected member: %+v", cls.Body[0])
	}
}

func TestClassWithLocalRandVariable(t *testing.T) {
	// rand/randc combined with a visibility qualifier, in either order --
	// both flags must survive regardless of which came first.
	cls := classDecl(t, "class packet;\nlocal rand bit [7:0] payload;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if v.Name != "payload" || !v.IsRand {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithRandLocalVariable(t *testing.T) {
	cls := classDecl(t, "class packet;\nrand local bit [7:0] payload;\nendclass")
	v := cls.Body[0].(*ast.Variable)
	if v.Name != "payload" || !v.IsRand {
		t.Fatalf("unexpected member: %+v", v)
	}
}

func TestClassWithStaticMethod(t *testing.T) {
	// "static function ..." (LRM 8.10, a class-scope method belonging to
	// the class rather than an instance) is written BEFORE "function",
	// distinct from the function/task lifetime qualifier of the same
	// spelling written AFTER it (already covered by
	// TestFunctionAutomaticLifetime and friends).
	cls := classDecl(t, "class counter;\nstatic function int next_id();\nendfunction\nendclass")
	fn := cls.Body[0].(*ast.Function)
	if fn.Name != "next_id" || fn.ReturnType.Name != "int" {
		t.Fatalf("unexpected static method: %+v", fn)
	}
}

func TestClassWithStaticTask(t *testing.T) {
	cls := classDecl(t, "class counter;\nstatic task reset();\nendtask\nendclass")
	task := cls.Body[0].(*ast.Task)
	if task.Name != "reset" {
		t.Fatalf("unexpected static task: %+v", task)
	}
}

func TestClassWithConstraint(t *testing.T) {
	cls := classDecl(t, `class packet;
		rand bit [7:0] payload;
		constraint c_payload { payload inside {[0:255]}; }
	endclass`)
	if len(cls.Body) != 2 {
		t.Fatalf("expected 2 members, got %+v", cls.Body)
	}
	c, ok := cls.Body[1].(*ast.Constraint)
	if !ok || c.Name != "c_payload" {
		t.Fatalf("unexpected second member: %+v", cls.Body[1])
	}
}

func TestClassWithStaticConstraint(t *testing.T) {
	cls := classDecl(t, "class foo;\nstatic constraint c1 { x > 0; }\nendclass")
	c := cls.Body[0].(*ast.Constraint)
	if c.Name != "c1" {
		t.Fatalf("unexpected constraint: %+v", c)
	}
}

func TestClassWithExternConstraintPrototype(t *testing.T) {
	// "extern constraint valid;" (LRM 18.5.1) -- no braced body, matching
	// an out-of-class-body constraint defined elsewhere (see
	// TestOutOfClassConstraintBody).
	cls := classDecl(t, "class c;\nextern constraint valid;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	c, ok := cls.Body[0].(*ast.Constraint)
	if !ok || c.Name != "valid" || !c.Prototype {
		t.Fatalf("unexpected constraint: %+v", cls.Body[0])
	}
}

func TestClassWithPureConstraintPrototype(t *testing.T) {
	// "pure constraint valid;" (LRM 18.5.2, an abstract class's
	// constraint prototype) -- unlike "pure virtual function/task", there
	// is no "virtual" keyword in this form at all.
	cls := classDecl(t, "virtual class c;\npure constraint valid;\nendclass")
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	c, ok := cls.Body[0].(*ast.Constraint)
	if !ok || c.Name != "valid" || !c.Prototype {
		t.Fatalf("unexpected constraint: %+v", cls.Body[0])
	}
}

func TestOutOfClassConstraintBody(t *testing.T) {
	// "constraint Cls::name { ... }" (the constraint counterpart to an
	// out-of-class method body matching an extern prototype) -- the class
	// qualifier must be stripped from the name, not become the
	// constraint's own name.
	f, errs := parseSrc(t, "class c;\nextern constraint valid;\nendclass\nconstraint c::valid { x < 10; }")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected 2 top-level decls, got %+v", f.Decls)
	}
	body, ok := f.Decls[1].(*ast.Constraint)
	if !ok || body.Name != "valid" || body.Prototype {
		t.Fatalf("unexpected out-of-class constraint body: %+v", f.Decls[1])
	}
}

func TestClassConstraintWithNestedBraces(t *testing.T) {
	// The nested {[0:255]} aggregate/range must not be mistaken for the
	// constraint body's own closing brace.
	cls := classDecl(t, `class packet;
		rand bit [7:0] payload;
		constraint c_payload { payload inside {[0:255]}; }
		function void dummy(); endfunction
	endclass`)
	if len(cls.Body) != 3 {
		t.Fatalf("expected 3 members (constraint's nested braces didn't confuse parsing past it), got %+v", cls.Body)
	}
	if _, ok := cls.Body[2].(*ast.Function); !ok {
		t.Fatalf("expected the function after the constraint to parse correctly, got %+v", cls.Body[2])
	}
}

func TestClassWithMultipleConstraints(t *testing.T) {
	cls := classDecl(t, `class packet;
		constraint c1 { x > 0; }
		constraint c2 { y < 10; }
	endclass`)
	if len(cls.Body) != 2 {
		t.Fatalf("expected 2 constraints, got %+v", cls.Body)
	}
	if cls.Body[0].(*ast.Constraint).Name != "c1" || cls.Body[1].(*ast.Constraint).Name != "c2" {
		t.Fatalf("unexpected constraints: %+v", cls.Body)
	}
}

func TestClassWithFunctionAndTaskMembers(t *testing.T) {
	cls := classDecl(t, `class packet;
		function new();
		endfunction
		task run();
		endtask
	endclass`)
	if len(cls.Body) != 2 {
		t.Fatalf("expected 2 members, got %+v", cls.Body)
	}
	if cls.Body[0].(*ast.Function).Name != "new" {
		t.Fatalf("unexpected constructor: %+v", cls.Body[0])
	}
	if cls.Body[1].(*ast.Task).Name != "run" {
		t.Fatalf("unexpected task: %+v", cls.Body[1])
	}
}

func TestClassWithVirtualFunctionMemberHasABody(t *testing.T) {
	cls := classDecl(t, `class packet;
		virtual function void display();
			$display("hi");
		endfunction
	endclass`)
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	fn := cls.Body[0].(*ast.Function)
	if fn.Name != "display" || fn.Prototype {
		t.Fatalf("unexpected virtual function: %+v", fn)
	}
}

func TestClassWithVirtualTaskMemberHasABody(t *testing.T) {
	cls := classDecl(t, `class packet;
		virtual task run();
		endtask
	endclass`)
	task := cls.Body[0].(*ast.Task)
	if task.Name != "run" || task.Prototype {
		t.Fatalf("unexpected virtual task: %+v", task)
	}
}

func TestClassWithPureVirtualFunctionMemberIsAPrototype(t *testing.T) {
	cls := classDecl(t, `class packet;
		pure virtual function void display();
	endclass`)
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member, got %+v", cls.Body)
	}
	fn := cls.Body[0].(*ast.Function)
	if fn.Name != "display" || !fn.Prototype {
		t.Fatalf("unexpected pure virtual function: %+v", fn)
	}
}

func TestClassWithPureVirtualTaskMemberIsAPrototype(t *testing.T) {
	cls := classDecl(t, `class packet;
		pure virtual task run();
	endclass`)
	task := cls.Body[0].(*ast.Task)
	if task.Name != "run" || !task.Prototype {
		t.Fatalf("unexpected pure virtual task: %+v", task)
	}
}

// "'{" is a brace opener that "}" closes, so a skip that counts "}" as a
// closer must count "'{" as an opener too -- otherwise an assignment-pattern
// literal in a constraint body closes the block early and everything after
// it is reparsed as though it were a class member.
func TestConstraintBodyWithAssignmentPattern(t *testing.T) {
	f, errs := parseSrc(t, "class c;\n  constraint pat { arr == '{1, 2, 3}; }\n  int after;\nendclass")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	cls := f.Decls[0].(*ast.Class)
	if len(cls.Body) != 2 {
		t.Fatalf("expected constraint + int after, got %d members: %+v", len(cls.Body), cls.Body)
	}
	con, ok := cls.Body[0].(*ast.Constraint)
	if !ok || con.Name != "pat" {
		t.Fatalf("expected constraint pat, got %+v", cls.Body[0])
	}
	v, ok := cls.Body[1].(*ast.Variable)
	if !ok || v.Name != "after" {
		t.Fatalf("expected variable after, got %+v", cls.Body[1])
	}
}
