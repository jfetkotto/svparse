package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestGatePrimitiveInstantiationIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		and g1 (y, a, b);
		nand g2 (y2, a, b);
		logic after_marker;`)

	var names []string
	for _, d := range body {
		if v, ok := d.(*ast.Variable); ok {
			names = append(names, v.Name)
		}
	}
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the gate instances, got %+v", body)
	}
}

func TestDefparamIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		defparam u1.WIDTH = 8;
		logic after_marker;`)

	var names []string
	for _, d := range body {
		if v, ok := d.(*ast.Variable); ok {
			names = append(names, v.Name)
		}
	}
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the defparam, got %+v", body)
	}
}

func namesOfSurvivingVariables(body []ast.Decl) []string {
	var names []string
	for _, d := range body {
		if v, ok := d.(*ast.Variable); ok {
			names = append(names, v.Name)
		}
	}
	return names
}

func TestBindIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		bind sub my_checker chk (.*);
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the bind, got %+v", body)
	}
}

func TestBindWithParamOverridesIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		bind sub my_checker #(.P(1)) chk (.*);
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the bind, got %+v", body)
	}
}

func TestTimeunitIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		timeunit 1ns;
		timeprecision 1ps;
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive timeunit/timeprecision, got %+v", body)
	}
}

func TestExportIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		export foo::*;
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the export, got %+v", body)
	}
}

func TestLetIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		let max2(a,b) = (a > b) ? a : b;
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the let, got %+v", body)
	}
}

func TestAliasIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		alias w1 = w2;
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the alias, got %+v", body)
	}
}

func TestGlobalClockingIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		global clocking @(posedge clk); endclocking
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the global clocking block, got %+v", body)
	}
}

func TestGlobalClockingWithName(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		global clocking cb @(posedge clk); endclocking : cb
		logic after_marker;`)
	names := namesOfSurvivingVariables(body)
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the named global clocking block, got %+v", body)
	}
}
