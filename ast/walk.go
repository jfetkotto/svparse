package ast

// Node is anything Walk visits and Dump prints: every Decl, plus the
// sub-nodes that carry a source position but are not declarations in their
// own right (Type, ParamOverride, Instance, PortConnection, ModportPort) and
// the File root.
//
// Like Decl it is sealed by an unexported method, so the set of node types
// stays this package's to define. Dim is deliberately not a Node: it has no
// position of its own and nothing hangs off it yet; once expression trees
// exist (see the roadmap) Walk descends into them from the Dim's owner.
type Node interface {
	Pos() Position
	node()
}

func (declBase) node() {}

func (Type) node()                     {}
func (t Type) Pos() Position           { return t.Position }
func (ParamOverride) node()            {}
func (o ParamOverride) Pos() Position  { return o.Position }
func (Instance) node()                 {}
func (i Instance) Pos() Position       { return i.Position }
func (PortConnection) node()           {}
func (c PortConnection) Pos() Position { return c.Position }
func (ModportPort) node()              {}
func (m ModportPort) Pos() Position    { return m.Position }
func (*File) node()                    {}

// Pos returns a position naming only the file: a File has no extent of its
// own beyond its declarations'.
func (f *File) Pos() Position { return Position{File: f.Path} }

// A Visitor's Visit method is called by Walk for each node it reaches, in
// depth-first source order, mirroring go/ast.
//
// If Visit returns a non-nil visitor w, Walk descends into the node's
// children with w, then calls w.Visit(nil). If it returns nil, the node's
// children are skipped.
type Visitor interface {
	Visit(n Node) (w Visitor)
}

// Walk traverses the tree rooted at n depth-first, in source order.
//
// Nodes the tree stores by value in a slice (a Container's Ports, a Struct's
// Members, ...) are visited by pointer to the slice element, so a visitor
// always type-switches on pointer types (*Port, *Variable, ...), the same
// as for nodes stored in Decls and Body, and may modify the tree in place.
//
// A zero-valued Type (an untyped parameter, a port with no written type) is
// not visited: it records the absence of a type, not a type.
func Walk(v Visitor, n Node) {
	if v = v.Visit(n); v == nil {
		return
	}

	switch n := n.(type) {
	case *File:
		walkList(v, n.Decls)
	case *Container:
		walkValues(v, n.Params)
		walkValues(v, n.Ports)
		walkList(v, n.Body)
	case *Class:
		walkValues(v, n.Params)
		walkList(v, n.Body)
	case *Package:
		walkList(v, n.Body)
	case *Variable:
		walkType(v, &n.Type)
	case *Parameter:
		walkType(v, &n.Type)
	case *Typedef:
		if n.Underlying != nil {
			Walk(v, n.Underlying)
		}
	case *TypeAlias:
		walkType(v, &n.Type)
	case *Struct:
		walkValues(v, n.Members)
	case *Union:
		walkValues(v, n.Members)
	case *Enum:
		walkType(v, &n.BaseType)
		walkValues(v, n.Members)
	case *Port:
		walkType(v, &n.Type)
	case *PortDecl:
		walkType(v, &n.Type)
	case *Modport:
		walkValues(v, n.Ports)
	case *Arg:
		walkType(v, &n.Type)
	case *Function:
		walkType(v, &n.ReturnType)
		walkValues(v, n.Args)
	case *Task:
		walkValues(v, n.Args)
	case *Type:
		walkValues(v, n.ParamOverrides)
	case *Instantiation:
		walkValues(v, n.ParamOverrides)
		walkValues(v, n.Instances)
	case *Instance:
		walkValues(v, n.Connections)
	case *EnumMember, *Constraint, *Import, *ParamOverride, *PortConnection, *ModportPort:
		// Leaves.
	}

	v.Visit(nil)
}

func walkList(v Visitor, list []Decl) {
	for _, d := range list {
		Walk(v, d)
	}
}

// walkValues walks a slice of value-stored nodes by element address.
func walkValues[T any, P interface {
	*T
	Node
}](v Visitor, list []T) {
	for i := range list {
		Walk(v, P(&list[i]))
	}
}

func walkType(v Visitor, t *Type) {
	if !t.isZero() {
		Walk(v, t)
	}
}

func (t *Type) isZero() bool {
	return t.Position == (Position{}) && !t.Signed && !t.Unsigned &&
		t.PackageQualifier == "" && t.Modport == "" && t.Name == "" &&
		len(t.PackedDims) == 0 && len(t.ParamOverrides) == 0
}

type inspector func(Node) bool

func (f inspector) Visit(n Node) Visitor {
	if f(n) {
		return f
	}
	return nil
}

// Inspect traverses the tree rooted at n depth-first, calling f for each
// node. If f returns true, Inspect descends into the node's children, then
// calls f(nil) once they are done; if it returns false, the children are
// skipped.
func Inspect(n Node, f func(Node) bool) {
	Walk(inspector(f), n)
}
