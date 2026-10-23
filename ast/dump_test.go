package ast

import (
	"testing"

	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

func tk(texts ...string) []preprocessor.Token {
	toks := make([]preprocessor.Token, len(texts))
	for i, s := range texts {
		toks[i] = preprocessor.Token{Token: token.Token{Text: s}}
	}
	return toks
}

func TestDumpNodes(t *testing.T) {
	cases := []struct {
		name string
		node Node
		want string
	}{
		{"port-decl", &PortDecl{Direction: DirInout, Name: "p", Type: Type{Name: "wire"}, UnpackedDims: []Dim{{Left: tk("3")}}},
			"(port-decl inout p <wire> [3])\n"},
		{"unknown container kind never panics", &Container{ContainerKind: 9, Name: "x"},
			"(container?9 x)\n"},
		{"union", &Union{Packed: true, Members: []Variable{{Name: "u", Type: Type{Name: "int", Signed: true}}}},
			"(union packed\n  (variable u <int signed>))\n"},
		{"task with qualified parameterized arg type",
			&Task{Name: "t", Prototype: true, Args: []Arg{{Direction: DirRef, Name: "r", Type: Type{
				PackageQualifier: "p", Name: "c",
				ParamOverrides: []ParamOverride{{Name: "W", Value: tk("8")}, {Value: tk("2")}},
			}}}},
			`(task t prototype
  (arg ref r <p::c #(.W "8", "2")>))
`},
		{"type with modport", &Type{Name: "bus", Modport: "mp", Unsigned: true},
			"(type bus.mp unsigned)\n"},
		{"unsized and queue dims", &Variable{Name: "q", UnpackedDims: []Dim{{Left: tk("$")}, {}}},
			"(variable q [$] [])\n"},
		{"range dims keep token spacing normalised", &Variable{Name: "w", Type: Type{Name: "logic", PackedDims: []Dim{{Left: tk("N", "-", "1"), Right: tk("0")}}}},
			"(variable w <logic [N - 1:0]>)\n"},
		{"class flags", &Class{Name: "c", Virtual: true, IsInterfaceClass: true, Extends: "b", ExtendsArgs: tk("1"), Implements: []string{"i", "j"}},
			`(class c virtual interface extends=b extends-args="1" implements=i,j)` + "\n"},
		{"untyped localparam", &Parameter{IsLocal: true, Name: "M", Default: tk("1")},
			`(localparam M = "1")` + "\n"},
		{"port with no direction or type", &Port{Name: "a"},
			"(port a)\n"},
		{"forward typedef", &Typedef{Name: "fwd"},
			"(typedef fwd forward)\n"},
		{"implicit connection", &PortConnection{Name: "a", Implicit: true},
			"(connection .a implicit)\n"},
		{"wildcard connection", &PortConnection{Wildcard: true},
			"(connection .*)\n"},
		{"named empty connection", &PortConnection{Name: "a"},
			"(connection .a)\n"},
	}
	for _, c := range cases {
		if got := Dump(c.node); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

func TestDumpWithPositions(t *testing.T) {
	imp := &Import{declBase: declBase{Position{Line: 1, Character: 2, EndLine: 3, EndCharacter: 4}}, Package: "p", Member: "m"}
	if got, want := DumpWithPositions(imp), "(import p::m @1:2-3:4)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Dump(imp), "(import p::m)\n"; got != want {
		t.Errorf("Dump must omit positions: got %q, want %q", got, want)
	}
}
