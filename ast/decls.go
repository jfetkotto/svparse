package ast

import "github.com/jfetkotto/svparse/preprocessor"

// Direction is a port or function/task argument's direction.
// DirUnspecified means no direction was written -- the LRM gives this a
// resolved meaning (inheriting the previous port/arg's direction, with
// its own rule for the very first one), but the parser deliberately
// doesn't resolve that here: it records what was actually written, and
// leaves inheritance to a consumer that wants it.
type Direction string

const (
	DirUnspecified Direction = ""
	DirInput       Direction = "input"
	DirOutput      Direction = "output"
	DirInout       Direction = "inout"
	DirRef         Direction = "ref"
)

// Variable is one declared net/variable name -- "logic [7:0] a, b, c;"
// becomes three Variable nodes sharing an equal Type, one per name,
// mirroring how sigils's own lexical scanner already treats multi-name
// declarations. Also doubles as a struct/union member, which has the
// identical shape (IsRand/IsRandC are always false there -- SV's rand/
// randc qualifiers only apply to class properties).
type Variable struct {
	declBase
	Type         Type
	Name         string
	UnpackedDims []Dim
	Initial      []preprocessor.Token // nil unless "= expr"
	IsRand       bool
	IsRandC      bool
	IsStatic     bool
}

// Parameter is a parameter or localparam declaration. UnpackedDims covers
// LRM 6.20.2's array-parameter shape ("localparam int A[1:0] = '{1,2};"),
// which also makes bit-/part-select of the parameter legal wherever it's
// referenced.
type Parameter struct {
	declBase
	IsLocal      bool
	Type         Type // zero value if untyped ("parameter WIDTH = 8" -- Type.Name is "")
	Name         string
	UnpackedDims []Dim
	Default      []preprocessor.Token
}

// Typedef is a typedef. Underlying is nil for a forward declaration
// ("typedef class Foo;" or a bare "typedef Foo;"); otherwise it's a
// *Struct, *Union, *Enum, or *TypeAlias, whose own Name is empty -- it's
// anonymous, reachable only via this Typedef's Name.
type Typedef struct {
	declBase
	Name       string
	Underlying Decl
}

// TypeAlias is a typedef's underlying type when it's a plain alias
// rather than an inline struct/union/enum ("typedef logic [7:0] byte_t;").
type TypeAlias struct {
	declBase
	Type Type
}

// Struct and Union share the same shape: a packed/unpacked flag and a
// member list (reusing Variable, which has the identical name+type+dims
// shape a struct/union field needs).
type Struct struct {
	declBase
	Packed  bool
	Members []Variable
}

type Union struct {
	declBase
	Packed  bool
	Members []Variable
}

// Enum is an enumerated type. BaseType is the zero value if unwritten
// (defaults to int per the LRM -- the parser records what was written,
// not the resolved default).
type Enum struct {
	declBase
	BaseType Type
	Members  []EnumMember
}

type EnumMember struct {
	declBase
	Name  string
	Value []preprocessor.Token // nil unless "= expr"
}

// Port is one ANSI port list entry (see the parser's design doc for why
// only ANSI-style port lists are supported -- the same limitation
// sigils's own scanPortList already has).
type Port struct {
	declBase
	Direction    Direction
	Type         Type
	Name         string
	UnpackedDims []Dim
	Default      []preprocessor.Token
}

// Arg is one function/task argument.
type Arg struct {
	declBase
	Direction    Direction
	Type         Type
	Name         string
	UnpackedDims []Dim
	Default      []preprocessor.Token
}

// Function and Task are near-identical: a name, an argument list, and
// whether this is a prototype (extern/pure virtual/DPI import -- no
// body). Function additionally has a return type. Neither node's body
// is parsed -- see the parser's skipBody design.
type Function struct {
	declBase
	Name       string
	ReturnType Type
	Args       []Arg
	Prototype  bool
}

type Task struct {
	declBase
	Name      string
	Args      []Arg
	Prototype bool
}
