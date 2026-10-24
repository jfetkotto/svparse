package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestImportSpecificMember(t *testing.T) {
	body := moduleBody(t, "import my_pkg::my_func;")
	imp := body[0].(*ast.Import)
	if imp.Package != "my_pkg" || imp.Member != "my_func" {
		t.Fatalf("unexpected import: %+v", imp)
	}
}

func TestImportWildcard(t *testing.T) {
	body := moduleBody(t, "import my_pkg::*;")
	imp := body[0].(*ast.Import)
	if imp.Package != "my_pkg" || imp.Member != "*" {
		t.Fatalf("unexpected import: %+v", imp)
	}
}

func TestImportMultiplePackagesOneStatement(t *testing.T) {
	body := moduleBody(t, "import pkg1::name1, pkg2::*;")
	if len(body) != 2 {
		t.Fatalf("expected 2 imports, got %+v", body)
	}
	a := body[0].(*ast.Import)
	b := body[1].(*ast.Import)
	if a.Package != "pkg1" || a.Member != "name1" {
		t.Fatalf("unexpected first import: %+v", a)
	}
	if b.Package != "pkg2" || b.Member != "*" {
		t.Fatalf("unexpected second import: %+v", b)
	}
}

func TestImportAtTopLevel(t *testing.T) {
	f, errs := parseSrc(t, "import my_pkg::*;\nmodule top; endmodule")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected import + module, got %+v", f.Decls)
	}
	if _, ok := f.Decls[0].(*ast.Import); !ok {
		t.Fatalf("expected an import first, got %T", f.Decls[0])
	}
}

func TestModuleHeaderImport(t *testing.T) {
	// "module m import pkg::*; (...);" (LRM 26.3) -- a package import
	// declaration directly in the module header, before the port list.
	// Previously parsePortList saw "import" (which it doesn't recognize
	// at all) instead of '(' and the whole port list was lost.
	c := moduleWithPorts(t, "module m import my_pkg::*; (input logic clk); endmodule")
	if len(c.Ports) != 1 || c.Ports[0].Name != "clk" {
		t.Fatalf("expected the port list to survive the header import, got %+v", c.Ports)
	}
	if len(c.Body) != 1 {
		t.Fatalf("expected the header import in the body, got %+v", c.Body)
	}
	imp, ok := c.Body[0].(*ast.Import)
	if !ok || imp.Package != "my_pkg" || imp.Member != "*" {
		t.Fatalf("unexpected header import: %+v", c.Body[0])
	}
}

func TestModuleHeaderImportMultiple(t *testing.T) {
	c := moduleWithPorts(t, "module m import a_pkg::*, b_pkg::thing; #(parameter W = 4) (input logic clk); endmodule")
	if len(c.Params) != 1 || len(c.Ports) != 1 {
		t.Fatalf("expected params+ports to survive, got params=%+v ports=%+v", c.Params, c.Ports)
	}
	if len(c.Body) != 2 {
		t.Fatalf("expected 2 header imports in the body, got %+v", c.Body)
	}
}

func TestModuleHeaderImportNoPorts(t *testing.T) {
	// No port list at all after the header import -- must not require one.
	c := moduleWithPorts(t, "module m import my_pkg::*; ; endmodule")
	if len(c.Ports) != 0 {
		t.Fatalf("expected no ports, got %+v", c.Ports)
	}
	if len(c.Body) != 1 {
		t.Fatalf("expected the header import in the body, got %+v", c.Body)
	}
}

func TestDPIImportStillTakesPrecedenceOverPackageImport(t *testing.T) {
	// The dispatch checks for a following string literal (DPI form)
	// before falling through to package-import parsing -- confirm both
	// still work correctly side by side.
	body := moduleBody(t, "import \"DPI-C\" function int c_func(int x);\nimport my_pkg::*;")
	if len(body) != 2 {
		t.Fatalf("expected 2 decls, got %+v", body)
	}
	if _, ok := body[0].(*ast.Function); !ok {
		t.Fatalf("expected the DPI import to parse as a Function, got %T", body[0])
	}
	if _, ok := body[1].(*ast.Import); !ok {
		t.Fatalf("expected the package import to parse as an Import, got %T", body[1])
	}
}
