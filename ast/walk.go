package ast

// Node is anything Walk visits and Dump prints: every Decl, plus the
// sub-nodes that carry a source position but are not declarations in their
// own right (Type, ParamOverride, Instance, PortConnection, ModportPort) and
// the File root.
//
// Like Decl it is sealed by an unexported method, so the set of node types
// stays this package's to define. Dim is deliberately not a Node: it has no
// position of its own, so Walk reaches its parsed bound expressions
// (LeftExpr, RightExpr) from the Dim's owner.
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
		walkValues(v, n.ExtendsTypes)
		walkExprs(v, n.ExtendsArgExprs)
		walkValues(v, n.ImplementsTypes)
		walkList(v, n.Body)
	case *Package:
		walkList(v, n.Body)
	case *Variable:
		walkType(v, &n.Type)
		walkDims(v, n.UnpackedDims)
		walkExprs(v, n.Delay)
		walkExpr(v, n.InitialExpr)
	case *Nettype:
		walkType(v, &n.Type)
	case *Parameter:
		walkType(v, &n.Type)
		walkDims(v, n.UnpackedDims)
		walkExpr(v, n.DefaultExpr)
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
		walkDims(v, n.UnpackedDims)
		walkExpr(v, n.DefaultExpr)
	case *PortDecl:
		walkType(v, &n.Type)
		walkDims(v, n.UnpackedDims)
	case *Modport:
		walkValues(v, n.Ports)
	case *Arg:
		walkType(v, &n.Type)
		walkDims(v, n.UnpackedDims)
		walkExpr(v, n.DefaultExpr)
	case *Function:
		walkType(v, &n.ReturnType)
		walkValues(v, n.Args)
		walkList(v, n.Decls)
		walkStmts(v, n.Stmts)
	case *Task:
		walkValues(v, n.Args)
		walkList(v, n.Decls)
		walkStmts(v, n.Stmts)
	case *Type:
		if n.Inline != nil {
			Walk(v, n.Inline)
		}
		if n.TypeRef != nil {
			Walk(v, n.TypeRef)
		}
		walkValues(v, n.QualifierOverrides)
		walkValues(v, n.ParamOverrides)
		walkDims(v, n.PackedDims)
	case *Instantiation:
		walkValues(v, n.ParamOverrides)
		walkValues(v, n.Instances)
	case *Instance:
		walkDims(v, n.UnpackedDims)
		walkValues(v, n.Connections)
	case *ParamOverride:
		walkExpr(v, n.ValueExpr)
	case *EnumMember:
		walkExpr(v, n.ValueExpr)
	case *PortConnection:
		walkExpr(v, n.ConnExpr)
	case *Constraint, *Import, *ModportPort:
		// Leaves.

	// Expressions.
	case *ScopedName:
		walkExpr(v, n.Scope)
	case *ParameterizedName:
		walkExpr(v, n.Scope)
		walkValues(v, n.Overrides)
	case *MemberSelect:
		walkExpr(v, n.X)
	case *Index:
		walkExpr(v, n.X)
		walkExpr(v, n.Index)
	case *RangeSelect:
		walkExpr(v, n.X)
		walkExpr(v, n.Left)
		walkExpr(v, n.Right)
	case *Paren:
		walkExpr(v, n.X)
	case *Unary:
		walkExpr(v, n.X)
	case *Binary:
		walkExpr(v, n.X)
		walkExpr(v, n.Y)
	case *Conditional:
		walkExpr(v, n.Cond)
		walkExpr(v, n.Then)
		walkExpr(v, n.Else)
	case *Inside:
		walkExpr(v, n.X)
		walkExprs(v, n.Ranges)
	case *ValueRange:
		walkExpr(v, n.Lo)
		walkExpr(v, n.Hi)
	case *Concat:
		walkExprs(v, n.Elems)
	case *Replicate:
		walkExpr(v, n.Count)
		walkExprs(v, n.Elems)
	case *AssignPattern:
		walkExpr(v, n.Type)
		walkExprs(v, n.Items)
	case *PatternItem:
		walkExpr(v, n.Key)
		walkExpr(v, n.Value)
	case *Cast:
		walkExpr(v, n.Type)
		walkExpr(v, n.X)
	case *Call:
		walkExpr(v, n.Func)
		walkExprs(v, n.Args)
		walkExpr(v, n.With)
		if n.Constraints != nil {
			Walk(v, n.Constraints)
		}
	case *NamedArg:
		walkExpr(v, n.Value)
	case *New:
		walkExpr(v, n.Scope)
		walkExprs(v, n.Args)
		walkExpr(v, n.Copy)
	case *NewArray:
		walkExpr(v, n.Size)
		walkExpr(v, n.Init)
	case *MinTypMax:
		walkExpr(v, n.Min)
		walkExpr(v, n.Typ)
		walkExpr(v, n.Max)
	case *Streaming:
		walkExpr(v, n.SliceSize)
		walkExprs(v, n.Elems)
	case *WithRange:
		walkExpr(v, n.X)
		walkExpr(v, n.Left)
		walkExpr(v, n.Right)
	case *Tagged:
		walkExpr(v, n.X)
	case *TypeRef:
		walkExpr(v, n.X)
	case *TypeExpr:
		walkType(v, &n.Type)
	case *TypeOrExpr:
		walkExpr(v, n.X)
	case *IncDec:
		walkExpr(v, n.X)
	case *AssignExpr:
		walkExpr(v, n.LHS)
		walkExpr(v, n.RHS)
	case *Dist:
		walkExpr(v, n.X)
		walkValues(v, n.Items)
	case *DistItem:
		walkExpr(v, n.Value)
		walkExpr(v, n.Weight)
	case *Ident, *Literal, *Empty, *This, *Super, *BadExpr, *ConstraintBlock:
		// Leaves.

	// Statements.
	case *Block:
		walkList(v, n.Decls)
		walkStmts(v, n.Stmts)
	case *Labeled:
		walkStmt(v, n.Body)
	case *If:
		walkExpr(v, n.Cond)
		walkStmt(v, n.Then)
		walkStmt(v, n.Else)
	case *Case:
		walkExpr(v, n.Subject)
		walkValues(v, n.Items)
	case *CaseItem:
		walkExprs(v, n.Exprs)
		walkStmt(v, n.Body)
	case *For:
		walkList(v, n.InitDecls)
		walkStmts(v, n.Init)
		walkExpr(v, n.Cond)
		walkStmts(v, n.Step)
		walkStmt(v, n.Body)
	case *Foreach:
		walkExpr(v, n.Array)
		walkStmt(v, n.Body)
	case *While:
		walkExpr(v, n.Cond)
		walkStmt(v, n.Body)
	case *DoWhile:
		walkStmt(v, n.Body)
		walkExpr(v, n.Cond)
	case *Repeat:
		walkExpr(v, n.Count)
		walkStmt(v, n.Body)
	case *Forever:
		walkStmt(v, n.Body)
	case *TimingControl:
		walkExpr(v, n.Repeat)
		walkExpr(v, n.Delay)
		walkValues(v, n.Events)
	case *EventTerm:
		walkExpr(v, n.X)
		walkExpr(v, n.Iff)
	case *TimedStmt:
		walkControl(v, n.Control)
		walkStmt(v, n.Body)
	case *Wait:
		walkExpr(v, n.Cond)
		walkStmt(v, n.Body)
	case *WaitOrder:
		walkExprs(v, n.Events)
		walkStmt(v, n.Then)
		walkStmt(v, n.Else)
	case *EventTrigger:
		walkControl(v, n.Control)
		walkExpr(v, n.Event)
	case *Assign:
		walkExpr(v, n.LHS)
		walkControl(v, n.Control)
		walkExpr(v, n.RHS)
	case *ProcAssign:
		walkExpr(v, n.LHS)
		walkExpr(v, n.RHS)
	case *ExprStmt:
		walkExpr(v, n.X)
	case *Return:
		walkExpr(v, n.Value)
	case *Disable:
		walkExpr(v, n.Target)
	case *ImmediateAssert:
		walkExpr(v, n.Cond)
		walkStmt(v, n.Then)
		walkStmt(v, n.Else)
	case *RandCase:
		walkValues(v, n.Items)
	case *RandCaseItem:
		walkExpr(v, n.Weight)
		walkStmt(v, n.Body)
	case *Initial:
		walkStmt(v, n.Body)
	case *Final:
		walkStmt(v, n.Body)
	case *Always:
		walkStmt(v, n.Body)
	case *ContinuousAssign:
		walkExprs(v, n.Delay)
		walkValues(v, n.Assigns)
	case *GenerateRegion:
		walkList(v, n.Body)
	case *GenerateBlock:
		walkList(v, n.Body)
	case *GenerateIf:
		walkExpr(v, n.Cond)
		Walk(v, n.Then)
		if n.Else != nil {
			Walk(v, n.Else)
		}
	case *GenerateFor:
		walkExpr(v, n.Init)
		walkExpr(v, n.Cond)
		walkStmt(v, n.Step)
		Walk(v, n.Body)
	case *GenerateCase:
		walkExpr(v, n.Expr)
		walkValues(v, n.Items)
	case *GenerateCaseItem:
		walkExprs(v, n.Exprs)
		Walk(v, n.Body)
	case *Null, *WaitFork, *Break, *Continue, *RawStmt, *BadStmt:
		// Leaves.
	}

	v.Visit(nil)
}

// walkExpr walks an optional expression child; nil (an absent optional
// field) is skipped.
func walkExpr(v Visitor, e Expr) {
	if e != nil {
		Walk(v, e)
	}
}

func walkExprs(v Visitor, list []Expr) {
	for _, e := range list {
		walkExpr(v, e)
	}
}

// walkStmt walks an optional statement child; nil is skipped.
func walkStmt(v Visitor, s Stmt) {
	if s != nil {
		Walk(v, s)
	}
}

func walkStmts(v Visitor, list []Stmt) {
	for _, s := range list {
		walkStmt(v, s)
	}
}

// walkControl walks an optional timing control. It takes the concrete pointer
// so a nil *TimingControl is not mistaken for a non-nil interface.
func walkControl(v Visitor, c *TimingControl) {
	if c != nil {
		Walk(v, c)
	}
}

// walkDims walks the parsed bound expressions of a dimension list. Dim is not
// a Node, so its owner walks it.
func walkDims(v Visitor, dims []Dim) {
	for i := range dims {
		walkExpr(v, dims[i].LeftExpr)
		walkExpr(v, dims[i].RightExpr)
	}
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
		len(t.PackedDims) == 0 && len(t.ParamOverrides) == 0 &&
		len(t.QualifierOverrides) == 0 && t.Inline == nil && t.TypeRef == nil && !t.Virtual
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
