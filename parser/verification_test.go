package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestCovergroupIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		covergroup cg @(posedge clk);
			coverpoint state {
				bins idle = {IDLE};
				bins run = {RUN};
			}
		endgroup
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestCovergroupWithBlockLabel(t *testing.T) {
	body := moduleBody(t, `covergroup cg @(posedge clk);
	endgroup : cg
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestPropertyDeclarationIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		property p1;
			@(posedge clk) a |-> b;
		endproperty
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestSequenceDeclarationIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		sequence s1;
			@(posedge clk) a ##1 b;
		endsequence
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestClockingBlockIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		clocking cb @(posedge clk);
			default input #1step output #0;
			input a;
			output b;
		endclocking
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestDefaultClockingReferenceIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		default clocking cb;
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestDefaultDisableIffIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		default disable iff (rst);
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestCheckerDeclarationIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		checker my_checker(input logic a, input logic b);
			always_comb assert (a == b);
		endchecker
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

func TestSpecifyBlockIsSkippedCleanly(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		specify
			(a => y) = 3;
		endspecify
		logic after_marker;`)
	assertSurroundingVariables(t, body, "before_marker", "after_marker")
}

// assertSurroundingVariables checks that body contains exactly the given
// variable names, in order -- used across this file to confirm a skipped
// verification construct didn't disturb the ordinary declarations before
// and after it.
func assertSurroundingVariables(t *testing.T, body []ast.Decl, names ...string) {
	t.Helper()
	var got []string
	for _, d := range body {
		if v, ok := d.(*ast.Variable); ok {
			got = append(got, v.Name)
		}
	}
	if len(got) != len(names) {
		t.Fatalf("expected variables %v, got %v (full body: %+v)", names, got, body)
	}
	for i, name := range names {
		if got[i] != name {
			t.Fatalf("expected variables %v, got %v", names, got)
		}
	}
}
