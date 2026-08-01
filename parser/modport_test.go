package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestModportDeclarationsAreSkippedCleanly(t *testing.T) {
	f, errs := parseSrc(t, `interface bus_if;
		logic req, gnt;
		modport master (output req, input gnt);
		modport slave  (input req, output gnt);
		logic data;
	endinterface`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	iface := f.Decls[0].(*ast.Container)

	var names []string
	for _, d := range iface.Body {
		if v, ok := d.(*ast.Variable); ok {
			names = append(names, v.Name)
		}
	}
	if len(names) != 3 || names[0] != "req" || names[1] != "gnt" || names[2] != "data" {
		t.Fatalf("expected the surrounding variable declarations to survive the modports, got %+v", names)
	}
}

func TestModportWithMultipleGroupsOnOneStatement(t *testing.T) {
	// A single "modport" statement can name several modports at once,
	// comma-separated -- still just skipped to the terminating ';'.
	f, errs := parseSrc(t, `interface bus_if;
		logic req, gnt;
		modport master (output req, input gnt), slave (input req, output gnt);
	endinterface`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	iface := f.Decls[0].(*ast.Container)
	if len(iface.Body) != 2 {
		t.Fatalf("expected 2 variable decls, got %+v", iface.Body)
	}
}
