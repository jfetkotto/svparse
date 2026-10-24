package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestModportDeclarationsAreParsed(t *testing.T) {
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
	var modports []*ast.Modport
	for _, d := range iface.Body {
		switch d := d.(type) {
		case *ast.Variable:
			names = append(names, d.Name)
		case *ast.Modport:
			modports = append(modports, d)
		}
	}
	if len(names) != 3 || names[0] != "req" || names[1] != "gnt" || names[2] != "data" {
		t.Fatalf("expected the surrounding variable declarations to survive the modports, got %+v", names)
	}
	if len(modports) != 2 {
		t.Fatalf("expected 2 modport decls, got %+v", modports)
	}

	master := modports[0]
	if master.Name != "master" {
		t.Fatalf("expected first modport named %q, got %q", "master", master.Name)
	}
	wantMaster := []ast.ModportPort{
		{Direction: ast.DirOutput, Name: "req"},
		{Direction: ast.DirInput, Name: "gnt"},
	}
	assertModportPorts(t, master.Ports, wantMaster)

	slave := modports[1]
	if slave.Name != "slave" {
		t.Fatalf("expected second modport named %q, got %q", "slave", slave.Name)
	}
	wantSlave := []ast.ModportPort{
		{Direction: ast.DirInput, Name: "req"},
		{Direction: ast.DirOutput, Name: "gnt"},
	}
	assertModportPorts(t, slave.Ports, wantSlave)
}

func TestModportWithMultipleGroupsOnOneStatement(t *testing.T) {
	// A single "modport" statement can name several modports at once,
	// comma-separated -- each becomes its own *ast.Modport.
	f, errs := parseSrc(t, `interface bus_if;
		logic req, gnt;
		modport master (output req, input gnt), slave (input req, output gnt);
	endinterface`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	iface := f.Decls[0].(*ast.Container)

	var modports []*ast.Modport
	for _, d := range iface.Body {
		if mp, ok := d.(*ast.Modport); ok {
			modports = append(modports, mp)
		}
	}
	if len(iface.Body) != 4 { // req, gnt, master, slave
		t.Fatalf("expected 4 body decls (2 variables + 2 modports), got %+v", iface.Body)
	}
	if len(modports) != 2 || modports[0].Name != "master" || modports[1].Name != "slave" {
		t.Fatalf("expected modports named master, slave, got %+v", modports)
	}
}

func TestModportDirectionAppliesToRunOfIdentifiers(t *testing.T) {
	// LRM 25.5: one direction keyword covers every following identifier
	// until the next direction keyword appears.
	f, errs := parseSrc(t, `interface bus_if;
		logic req, ready, gnt, data;
		modport master (input req, ready, output gnt, data);
	endinterface`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	iface := f.Decls[0].(*ast.Container)

	var mp *ast.Modport
	for _, d := range iface.Body {
		if m, ok := d.(*ast.Modport); ok {
			mp = m
		}
	}
	if mp == nil {
		t.Fatalf("expected a modport decl, got body %+v", iface.Body)
	}
	want := []ast.ModportPort{
		{Direction: ast.DirInput, Name: "req"},
		{Direction: ast.DirInput, Name: "ready"},
		{Direction: ast.DirOutput, Name: "gnt"},
		{Direction: ast.DirOutput, Name: "data"},
	}
	assertModportPorts(t, mp.Ports, want)
}

func TestModportUnsupportedEntryShapeSkippedWithoutError(t *testing.T) {
	// Clocking-block and import/export-task modport ports (LRM 25.5's other
	// modport_ports_declaration forms) are out of scope for this parser --
	// skipped without recording an error, same "recognized syntax, out of
	// declaration-grade scope" treatment other constructs get.
	f, errs := parseSrc(t, `interface bus_if;
		modport tb (clocking cb);
		modport dut (import some_task, output req);
	endinterface`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	iface := f.Decls[0].(*ast.Container)
	if len(iface.Body) != 2 {
		t.Fatalf("expected 2 modport decls, got %+v", iface.Body)
	}

	tb := iface.Body[0].(*ast.Modport)
	if tb.Name != "tb" || len(tb.Ports) != 0 {
		t.Fatalf("expected tb modport with no recognized ports, got %+v", tb)
	}

	dut := iface.Body[1].(*ast.Modport)
	if dut.Name != "dut" {
		t.Fatalf("expected modport named dut, got %q", dut.Name)
	}
	assertModportPorts(t, dut.Ports, []ast.ModportPort{{Direction: ast.DirOutput, Name: "req"}})
}

func TestModportMalformedRecoversToNextDeclaration(t *testing.T) {
	f, errs := parseSrc(t, `interface bus_if;
		modport master output req, gnt);
		logic data;
	endinterface`)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing '('")
	}
	iface := f.Decls[0].(*ast.Container)

	var names []string
	for _, d := range iface.Body {
		if v, ok := d.(*ast.Variable); ok {
			names = append(names, v.Name)
		}
	}
	if len(names) != 1 || names[0] != "data" {
		t.Fatalf("expected the declaration after the malformed modport to survive, got %+v", names)
	}
}

func assertModportPorts(t *testing.T, got []ast.ModportPort, want []ast.ModportPort) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d modport ports, got %+v", len(want), got)
	}
	for i, g := range got {
		w := want[i]
		if g.Direction != w.Direction || g.Name != w.Name {
			t.Fatalf("port %d: expected {%s %s}, got {%s %s}", i, w.Direction, w.Name, g.Direction, g.Name)
		}
	}
}
