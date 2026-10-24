package ast

import (
	"reflect"
	"testing"
)

func sampleModule() *File {
	return &File{Path: "m.sv", Decls: []Decl{
		&Container{
			Name:   "m",
			Params: []Parameter{{Name: "P", Type: Type{Name: "int"}}},
			Ports:  []Port{{Name: "a", Type: Type{Name: "logic"}}},
			Body: []Decl{
				&Variable{Name: "v"},  // zero Type: not visited
				&Typedef{Name: "fwd"}, // forward: no Underlying
				&Instantiation{
					ModuleType:     "leaf",
					ParamOverrides: []ParamOverride{{Name: "W"}},
					Instances: []Instance{{
						Name:        "u",
						Connections: []PortConnection{{Name: "x"}},
					}},
				},
			},
		},
	}}
}

func typeNames(visited []Node) []string {
	var out []string
	for _, n := range visited {
		out = append(out, reflect.TypeOf(n).Elem().Name())
	}
	return out
}

func TestWalkVisitsInSourceOrder(t *testing.T) {
	var visited []Node
	Inspect(sampleModule(), func(n Node) bool {
		if n != nil {
			visited = append(visited, n)
		}
		return true
	})
	want := []string{
		"File", "Container",
		"Parameter", "Type", // params, then ports, then body
		"Port", "Type",
		"Variable", // its zero Type is skipped
		"Typedef",
		"Instantiation", "ParamOverride", "Instance", "PortConnection",
	}
	if got := typeNames(visited); !reflect.DeepEqual(got, want) {
		t.Errorf("visit order\n got: %v\nwant: %v", got, want)
	}
}

func TestInspectCallsNilOncePerDescendedNode(t *testing.T) {
	var nodes, nils int
	Inspect(sampleModule(), func(n Node) bool {
		if n == nil {
			nils++
		} else {
			nodes++
		}
		return true
	})
	if nodes != nils {
		t.Errorf("%d nodes visited but %d nil calls: every descended node must be closed exactly once", nodes, nils)
	}
}

func TestInspectFalseSkipsChildren(t *testing.T) {
	var visited []Node
	var nils int
	Inspect(sampleModule(), func(n Node) bool {
		if n == nil {
			nils++
			return true
		}
		visited = append(visited, n)
		_, isContainer := n.(*Container)
		return !isContainer
	})
	if got, want := typeNames(visited), []string{"File", "Container"}; !reflect.DeepEqual(got, want) {
		t.Errorf("visited %v, want %v", got, want)
	}
	if nils != 1 {
		t.Errorf("got %d nil calls, want 1 (only the File was descended)", nils)
	}
}

func TestWalkHandsOutPointersIntoTheTree(t *testing.T) {
	f := sampleModule()
	Inspect(f, func(n Node) bool {
		switch n := n.(type) {
		case *Port:
			n.Name = "renamed"
		case *Instance:
			n.Name = "renamed_u"
		}
		return true
	})
	c := f.Decls[0].(*Container)
	if c.Ports[0].Name != "renamed" {
		t.Errorf("Port edit did not reach the tree: %q", c.Ports[0].Name)
	}
	if got := c.Body[2].(*Instantiation).Instances[0].Name; got != "renamed_u" {
		t.Errorf("Instance edit did not reach the tree: %q", got)
	}
}

func TestWalkTypedefDescendsIntoUnderlying(t *testing.T) {
	td := &Typedef{Name: "t", Underlying: &Struct{Members: []Variable{{Name: "a", Type: Type{Name: "bit"}}}}}
	var visited []Node
	Inspect(td, func(n Node) bool {
		if n != nil {
			visited = append(visited, n)
		}
		return true
	})
	if got, want := typeNames(visited), []string{"Typedef", "Struct", "Variable", "Type"}; !reflect.DeepEqual(got, want) {
		t.Errorf("visited %v, want %v", got, want)
	}
}

func TestWalkVisitsTypeParamOverrides(t *testing.T) {
	v := &Variable{Name: "o", Type: Type{Name: "cls", ParamOverrides: []ParamOverride{{Value: nil}, {Name: "N"}}}}
	var n int
	Inspect(v, func(node Node) bool {
		if _, ok := node.(*ParamOverride); ok {
			n++
		}
		return true
	})
	if n != 2 {
		t.Errorf("visited %d ParamOverrides under the Type, want 2", n)
	}
}

func TestWalkPositionsOfNonDeclNodes(t *testing.T) {
	// Every Node, not just every Decl, must report the position it carries.
	pos := Position{File: "x.sv", Line: 4, Character: 2, EndLine: 4, EndCharacter: 5}
	for _, n := range []Node{
		&Type{Position: pos}, &ParamOverride{Position: pos}, &Instance{Position: pos},
		&PortConnection{Position: pos}, &ModportPort{Position: pos},
	} {
		if got := n.Pos(); got != pos {
			t.Errorf("%T.Pos() = %+v, want %+v", n, got, pos)
		}
	}
	if got := (&File{Path: "f.sv"}).Pos(); got.File != "f.sv" {
		t.Errorf("File.Pos() = %+v", got)
	}
}
