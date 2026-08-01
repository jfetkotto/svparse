package ast

import "github.com/jfetkotto/svparse/preprocessor"

// ContainerKind distinguishes the three container shapes that are
// otherwise structurally identical (ports, parameter ports, a body).
type ContainerKind int

const (
	KindModule ContainerKind = iota
	KindInterface
	KindProgram
)

// Container is a module, interface, or program -- one Go type for all
// three, since their shape doesn't actually differ, matching how nols's
// own sv.Declaration uses a Kind field rather than a type per kind.
type Container struct {
	declBase
	ContainerKind ContainerKind
	Name          string
	Params        []Parameter
	Ports         []Port
	Body          []Decl
}

// Class is a class declaration. Extends/ExtendsArgs describe an "extends
// Base(args)" clause (ExtendsArgs is the raw, unparsed constructor-call
// argument list to super, empty unless explicit args were written);
// Implements lists any interface classes implemented. IsInterfaceClass
// distinguishes "interface class Foo;" (which shares this same shape --
// extends/params/body -- rather than a container's ports/body shape)
// from an ordinary class.
type Class struct {
	declBase
	Virtual          bool
	IsInterfaceClass bool
	Name             string
	Params           []Parameter
	Extends          string
	ExtendsArgs      []preprocessor.Token
	Implements       []string
	Body             []Decl
}

// Package is a package declaration: a flat body of imports, variables,
// typedefs, functions/tasks, and parameters.
type Package struct {
	declBase
	Name string
	Body []Decl
}

// Constraint is a class constraint block. Its body is recognized and
// skipped (balanced braces), never parsed -- constraint-expression
// grammar is entirely out of scope. Prototype is true for an "extern
// constraint name;" or "pure constraint name;" declaration (LRM
// 18.5.1/18.5.2) -- no body at all, mirroring how ast.Function/Task use
// Prototype for their own extern/pure-virtual/DPI-import forms.
type Constraint struct {
	declBase
	Name      string
	Prototype bool
}
