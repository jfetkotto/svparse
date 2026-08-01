package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestInstantiationBasic(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf(.clk(clk), .rst_n(rst_n));")
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	inst := body[0].(*ast.Instantiation)
	if inst.ModuleType != "leaf" {
		t.Fatalf("unexpected instantiation: %+v", inst)
	}
	if len(inst.Instances) != 1 || inst.Instances[0].Name != "u_leaf" {
		t.Fatalf("unexpected instances: %+v", inst.Instances)
	}
	conns := inst.Instances[0].Connections
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections, got %+v", conns)
	}
	if conns[0].Name != "clk" || len(conns[0].Expr) != 1 || conns[0].Expr[0].Text != "clk" {
		t.Fatalf("unexpected clk connection: %+v", conns[0])
	}
	if conns[1].Name != "rst_n" || len(conns[1].Expr) != 1 || conns[1].Expr[0].Text != "rst_n" {
		t.Fatalf("unexpected rst_n connection: %+v", conns[1])
	}
}

func TestInstantiationImplicitDotName(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf(.clk, .rst_n);")
	inst := body[0].(*ast.Instantiation)
	conns := inst.Instances[0].Connections
	if !conns[0].Implicit || conns[0].Name != "clk" {
		t.Fatalf("unexpected clk connection: %+v", conns[0])
	}
	if !conns[1].Implicit || conns[1].Name != "rst_n" {
		t.Fatalf("unexpected rst_n connection: %+v", conns[1])
	}
}

func TestInstantiationWildcardDotStar(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf(.*);")
	inst := body[0].(*ast.Instantiation)
	conns := inst.Instances[0].Connections
	if len(conns) != 1 || !conns[0].Wildcard {
		t.Fatalf("unexpected connections: %+v", conns)
	}
}

func TestInstantiationPositionalConnection(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf(clk, rst_n);")
	inst := body[0].(*ast.Instantiation)
	conns := inst.Instances[0].Connections
	if conns[0].Name != "" || len(conns[0].Expr) != 1 || conns[0].Expr[0].Text != "clk" {
		t.Fatalf("unexpected positional connection: %+v", conns[0])
	}
}

func TestInstantiationEmptyPortList(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf();")
	inst := body[0].(*ast.Instantiation)
	if len(inst.Instances[0].Connections) != 0 {
		t.Fatalf("expected no connections, got %+v", inst.Instances[0].Connections)
	}
}

func TestInstantiationExplicitlyUnconnectedPort(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf(.unused());")
	inst := body[0].(*ast.Instantiation)
	conns := inst.Instances[0].Connections
	if conns[0].Name != "unused" || conns[0].Implicit || len(conns[0].Expr) != 0 {
		t.Fatalf("unexpected connection: %+v", conns[0])
	}
}

func TestInstantiationParamOverridesNamed(t *testing.T) {
	body := moduleBody(t, "leaf #(.WIDTH(8), .DEPTH(16)) u_leaf();")
	inst := body[0].(*ast.Instantiation)
	if len(inst.ParamOverrides) != 2 {
		t.Fatalf("expected 2 param overrides, got %+v", inst.ParamOverrides)
	}
	if inst.ParamOverrides[0].Name != "WIDTH" || tokenTexts(inst.ParamOverrides[0].Value)[0] != "8" {
		t.Fatalf("unexpected WIDTH override: %+v", inst.ParamOverrides[0])
	}
	if inst.ParamOverrides[1].Name != "DEPTH" || tokenTexts(inst.ParamOverrides[1].Value)[0] != "16" {
		t.Fatalf("unexpected DEPTH override: %+v", inst.ParamOverrides[1])
	}
}

func TestInstantiationParamOverridesPositional(t *testing.T) {
	body := moduleBody(t, "leaf #(8, 16) u_leaf();")
	inst := body[0].(*ast.Instantiation)
	if len(inst.ParamOverrides) != 2 {
		t.Fatalf("expected 2 param overrides, got %+v", inst.ParamOverrides)
	}
	if inst.ParamOverrides[0].Name != "" || tokenTexts(inst.ParamOverrides[0].Value)[0] != "8" {
		t.Fatalf("unexpected first override: %+v", inst.ParamOverrides[0])
	}
}

func TestInstantiationPortConnectionPositionPointsAtName(t *testing.T) {
	// "leaf u_leaf(.clk(sig));" -- "clk" starts at character 13; the
	// preceding "." (character 12) must not be what gets recorded, since
	// a consumer (e.g. find-references) needs the name's own position to
	// match a query or compute a correct rename edit range.
	body := moduleBody(t, "leaf u_leaf(.clk(sig));")
	inst := body[0].(*ast.Instantiation)
	conn := inst.Instances[0].Connections[0]
	if conn.Character != 13 {
		t.Fatalf("expected connection Position to point at \"clk\" (character 13), got %+v", conn.Position)
	}
}

func TestInstantiationImplicitConnectionPositionPointsAtName(t *testing.T) {
	// "leaf u_leaf(.clk);" -- same reasoning, for the ".name" implicit
	// shorthand form, which never reaches the "(expr)" branch at all.
	body := moduleBody(t, "leaf u_leaf(.clk);")
	inst := body[0].(*ast.Instantiation)
	conn := inst.Instances[0].Connections[0]
	if conn.Character != 13 {
		t.Fatalf("expected connection Position to point at \"clk\" (character 13), got %+v", conn.Position)
	}
}

func TestInstantiationParamOverridePositionPointsAtName(t *testing.T) {
	// "leaf #(.WIDTH(8)) u_leaf();" -- "WIDTH" starts at character 8.
	body := moduleBody(t, "leaf #(.WIDTH(8)) u_leaf();")
	inst := body[0].(*ast.Instantiation)
	ov := inst.ParamOverrides[0]
	if ov.Character != 8 {
		t.Fatalf("expected override Position to point at \"WIDTH\" (character 8), got %+v", ov.Position)
	}
}

func TestInstantiationMultipleInstancesSharedType(t *testing.T) {
	body := moduleBody(t, "leaf u0(.clk(clk)), u1(.clk(clk));")
	if len(body) != 1 {
		t.Fatalf("expected 1 decl (one Instantiation with 2 instances), got %+v", body)
	}
	inst := body[0].(*ast.Instantiation)
	if len(inst.Instances) != 2 || inst.Instances[0].Name != "u0" || inst.Instances[1].Name != "u1" {
		t.Fatalf("unexpected instances: %+v", inst.Instances)
	}
}

func TestInstantiationInstanceArray(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf[4](.clk(clk));")
	inst := body[0].(*ast.Instantiation)
	instance := inst.Instances[0]
	if len(instance.UnpackedDims) != 1 || instance.UnpackedDims[0].Left[0].Text != "4" {
		t.Fatalf("unexpected instance array dims: %+v", instance)
	}
}

func TestInstantiationConnectionExpressionWithNestedCall(t *testing.T) {
	body := moduleBody(t, "leaf u_leaf(.data(foo(1,2)));")
	inst := body[0].(*ast.Instantiation)
	conn := inst.Instances[0].Connections[0]
	got := tokenTexts(conn.Expr)
	want := []string{"foo", "(", "1", ",", "2", ")"}
	if len(got) != len(want) {
		t.Fatalf("Expr = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Expr = %v, want %v", got, want)
		}
	}
}

func TestVariableDeclarationUsingTypedefStillWorks(t *testing.T) {
	// The core disambiguation: no '(' anywhere means this is a variable
	// declaration using a user-defined type, not an instantiation.
	body := moduleBody(t, "my_type_t x;")
	v, ok := body[0].(*ast.Variable)
	if !ok || v.Type.Name != "my_type_t" || v.Name != "x" {
		t.Fatalf("expected a Variable using my_type_t, got %+v (%T)", body[0], body[0])
	}
}

func TestVariableDeclarationUsingTypedefWithInitializer(t *testing.T) {
	body := moduleBody(t, "my_type_t x = DEFAULT;")
	v := body[0].(*ast.Variable)
	if len(v.Initial) != 1 || v.Initial[0].Text != "DEFAULT" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestVariableDeclarationUsingTypedefMultipleNames(t *testing.T) {
	body := moduleBody(t, "my_type_t a, b;")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls, got %+v", body)
	}
	if body[0].(*ast.Variable).Name != "a" || body[1].(*ast.Variable).Name != "b" {
		t.Fatalf("unexpected variables: %+v", body)
	}
}

func TestVariableDeclarationUsingTypedefWithUnpackedArray(t *testing.T) {
	// "x[4]" with no '(' after -- an array of my_type_t, not an instance.
	body := moduleBody(t, "my_type_t x[4];")
	v := body[0].(*ast.Variable)
	if len(v.UnpackedDims) != 1 || v.UnpackedDims[0].Left[0].Text != "4" {
		t.Fatalf("unexpected variable: %+v", v)
	}
}

func TestInstantiationVsVariableArrayDisambiguation(t *testing.T) {
	// Identical prefix shape ("Foo bar[4]") but the presence or absence
	// of '(' after the array dims decides variable vs instantiation --
	// exercised back to back to make sure neither leaks into the other.
	body := moduleBody(t, "leaf bar[4](.clk(clk));\nmy_type_t baz[4];")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls, got %+v", body)
	}
	if _, ok := body[0].(*ast.Instantiation); !ok {
		t.Fatalf("expected an Instantiation first, got %T", body[0])
	}
	if _, ok := body[1].(*ast.Variable); !ok {
		t.Fatalf("expected a Variable second, got %T", body[1])
	}
}
