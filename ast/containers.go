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
// three, since their shape doesn't actually differ, matching how sigils's
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

	// Extends is the base class's bare name (a package qualifier and any
	// parameter list are dropped), "" if there is no extends clause; for an
	// interface class that extends several, the first. ExtendsTypes has the
	// full references, "pkg::Base #(5)" included, one per base.
	Extends      string
	ExtendsTypes []Type

	ExtendsArgs     []preprocessor.Token
	ExtendsArgExprs []Expr // parsed ExtendsArgs, one per argument (as in a Call's Args)

	// Implements lists the implemented interface classes' bare names, and
	// ImplementsTypes their full references, in the same order.
	Implements      []string
	ImplementsTypes []Type

	Body []Decl
}

// Package is a package declaration: a flat body of imports, variables,
// typedefs, functions/tasks, and parameters.
type Package struct {
	declBase
	Name string
	Body []Decl
}

// Constraint is a class constraint block (LRM 18.5). Items are its parsed
// constraint items. Prototype is true for an "extern constraint name;" or "pure
// constraint name;" declaration (LRM 18.5.1/18.5.2) -- no body at all, mirroring
// how ast.Function/Task use Prototype for their own extern/pure-virtual/DPI-import
// forms.
type Constraint struct {
	declBase
	Name      string
	Prototype bool
	Items     []ConstraintItem // nil for a prototype
}
