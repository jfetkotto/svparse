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
// identical shape (IsRand/IsRandC are set there too: a member may be
// qualified "rand" or "randc", LRM 7.2).
type Variable struct {
	declBase
	Type         Type
	Name         string
	UnpackedDims []Dim
	Initial      []preprocessor.Token // nil unless "= expr"
	InitialExpr  Expr                 // parsed Initial
	IsRand       bool
	IsRandC      bool
	IsStatic     bool

	// The following apply to a net declaration (LRM 6.7, 6.9.2) and are zero
	// for everything else.
	//
	// NetQualifier is "vectored" or "scalared" when written. DriveStrength is
	// the parenthesised strength pair, e.g. ["strong0", "pull1"], or a single
	// charge strength, e.g. ["small"]. Delay holds the one to three delay
	// values of "#10" or "#(1, 2, 3)".
	NetQualifier  string
	DriveStrength []string
	Delay         []Expr
}

// Parameter is a parameter or localparam declaration. UnpackedDims covers
// LRM 6.20.2's array-parameter shape ("localparam int A[1:0] = '{1,2};"),
// which also makes bit-/part-select of the parameter legal wherever it's
// referenced.
type Parameter struct {
	declBase
	IsLocal      bool
	IsSpecparam  bool // "specparam", a specify-block parameter (LRM 6.20.5)
	Type         Type // zero value if untyped ("parameter WIDTH = 8" -- Type.Name is "")
	Name         string
	UnpackedDims []Dim
	Default      []preprocessor.Token
	DefaultExpr  Expr // parsed Default; for a "parameter type", a type (*TypeExpr or *TypeOrExpr)
}

// Nettype is a user-defined net type declaration (LRM 6.6.7): "nettype
// data_type name [with resolve_function];". The alias form "nettype
// other_nettype name;" has the same shape, with Type naming the existing net
// type; telling the two apart takes name resolution.
type Nettype struct {
	declBase
	Name string
	Type Type

	// ResolveFunc is the name after "with", possibly package-qualified
	// ("pkg::resolve"), or "" if there is no resolution function.
	ResolveFunc string
}

// Typedef is a typedef. Underlying is nil for a forward declaration
// ("typedef class Foo;", "typedef struct Foo;", or a bare "typedef Foo;");
// otherwise it's a *Struct, *Union, *Enum, or *TypeAlias, whose own Name is
// empty -- it's anonymous, reachable only via this Typedef's Name.
type Typedef struct {
	declBase
	Name       string
	Underlying Decl

	// ForwardKind is the keyword of a forward declaration, "class", "struct",
	// "union" or "enum" (LRM 6.18), or "" for a bare "typedef Foo;" and for
	// every typedef that is not a forward declaration.
	ForwardKind string
}

// TypeAlias is a typedef's underlying type when it's a plain alias
// rather than an inline struct/union/enum ("typedef logic [7:0] byte_t;").
type TypeAlias struct {
	declBase
	Type Type
}

// Struct and Union share the same shape: a packed/unpacked flag and a
// member list (reusing Variable, which has the identical name+type+dims
// shape a struct/union field needs). For a typedef'd aggregate Position is
// the typedef's name; for an anonymous one written in place (Type.Inline) it
// spans from the keyword through the closing brace.
type Struct struct {
	declBase
	Packed  bool
	Members []Variable
}

type Union struct {
	declBase
	Packed  bool
	Tagged  bool // "union tagged", LRM 7.3.2
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

	ValueExpr Expr // parsed Value
}

// Port is one entry in a container's own header port list -- always
// ANSI-style syntactically (see parsePortList's doc comment for why only
// that header shape is supported, the same limitation sigils's own
// scanPortList already has), but its Direction/Type/UnpackedDims may
// arrive after the fact, from a non-ANSI body-level input/output/inout
// declaration (LRM 23.2.2.3) naming this same port -- see PortDecl and
// parseContainer's merge pass. A bare, not-yet-merged header entry has
// Direction == DirUnspecified and a zero Type, identical to an ANSI
// implicit-type port with no direction written; there is no way to tell
// the two apart from this struct alone once parsing is done.
type Port struct {
	declBase
	Direction    Direction
	Type         Type
	Name         string
	UnpackedDims []Dim
	Default      []preprocessor.Token
	DefaultExpr  Expr // parsed Default
}

// PortDecl is one non-ANSI port declaration (LRM 23.2.2.3): "direction
// [net_type|var] data_type_or_implicit list_of_port_identifiers;",
// naming a port the container's header list already declared bare and
// supplying the direction/type that header entry is still missing. One
// comma-separated name becomes one PortDecl, sharing the statement's
// Direction/Type, mirroring how a multi-name variable declaration becomes
// several Variable nodes.
//
// Deliberately not modeled as a Port itself: unlike Port (a name being
// declared for the first time, in the header), a PortDecl references a
// name declared elsewhere and is merged into that Port's own fields by
// parseContainer once parsing finishes -- it never survives into
// Container.Body as its own declaration.
type PortDecl struct {
	declBase
	Direction    Direction
	Type         Type
	Name         string
	UnpackedDims []Dim
}

// Modport is one interface modport declaration (LRM 25.5): "modport name
// (direction port [, ...]);" -- one comma-separated name in a possibly
// multi-name modport statement becomes one Modport, mirroring how a
// multi-name variable declaration becomes several Variable nodes. Only
// the simple "direction port_identifier" port form is modeled; an
// import/export task-based port, a clocking-block port, or a ".name(expr)"
// connection is dropped from Ports without error -- see the parser's
// parseModportPortList doc comment.
type Modport struct {
	declBase
	Name  string
	Ports []ModportPort
}

// ModportPort is one direction+identifier entry inside a modport's own
// port list. Not a Decl itself -- Position alone (not declBase) is enough
// for a consumer that wants its span, the same way Dim isn't a Decl
// either.
type ModportPort struct {
	Position
	Direction Direction
	Name      string
}

// Arg is one function/task argument.
type Arg struct {
	declBase
	Direction    Direction
	Type         Type
	Name         string
	UnpackedDims []Dim
	Default      []preprocessor.Token
	DefaultExpr  Expr // parsed Default
}

// Function and Task are near-identical: a name, an argument list, and
// whether this is a prototype (extern/pure virtual/DPI import -- no
// body). Function additionally has a return type.
//
// A body, when there is one, is split as in a Block: Decls are the local
// declarations (variables, typedefs, parameters) and Stmts the statements, in
// source order within each list. Args holds the arguments written in the
// header and, for the older style "function f; input int a; ...", the ones
// declared in the body, after the header's.
type Function struct {
	declBase
	Scope      string // class qualifier of an out-of-block definition, "C" in "C::m"; empty otherwise
	Name       string
	ReturnType Type
	Args       []Arg
	Prototype  bool
	Decls      []Decl
	Stmts      []Stmt
}

type Task struct {
	declBase
	Scope     string // class qualifier of an out-of-block definition; empty otherwise
	Name      string
	Args      []Arg
	Prototype bool
	Decls     []Decl // local declarations of the body
	Stmts     []Stmt // the body's statements
}
