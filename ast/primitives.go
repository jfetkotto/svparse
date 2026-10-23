package ast

// GateInstantiation is a built-in gate or switch instantiation (LRM 28.3):
// "and #(1,2) (strong0, weak1) g1 (o, a, b), g2[3:0] (o2, a2, b2);". Gate is
// the primitive keyword ("and", "bufif0", "pullup", ...). Its Position is the
// gate keyword.
type GateInstantiation struct {
	declBase
	Gate          string
	DriveStrength []string
	Delay         []Expr
	Instances     []GateInstance
}

// GateInstance is one instance of a GateInstantiation. Name is "" for an
// unnamed instance (LRM 28.3.2 makes the name optional), in which case
// Position is the opening parenthesis rather than a name. The first terminal
// is the output for most gates (LRM 28.4).
type GateInstance struct {
	Position
	Name         string
	UnpackedDims []Dim
	Terminals    []Expr
}

// UDP is a user-defined primitive (LRM 29). Ports are the header ports, with
// the direction and type of non-ANSI declarations merged in as for a module.
// Body holds what is left of the declarations before the table (reg
// declarations, the sequential "initial" statement). Sequential is true when
// any table row has a current-state field. EndLine and EndCharacter locate
// the "endprimitive" keyword, as for a Container.
type UDP struct {
	declBase
	Name         string
	Ports        []Port
	Body         []Decl
	Rows         []UDPRow
	Sequential   bool
	EndLine      int
	EndCharacter int
}

// UDPRow is one table entry (LRM 29.3): "inputs : [current :] next;". Inputs
// holds one level or edge symbol per input, an edge written "(01)" kept as
// "(01)". Current is "" for a combinational row. Position spans the row up to
// but not including its semicolon.
type UDPRow struct {
	Position
	Inputs  []string
	Current string
	Next    string
}
