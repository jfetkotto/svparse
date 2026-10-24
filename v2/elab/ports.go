package elab

import (
	"fmt"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
)

// ConnKind says how a port got its connection.
type ConnKind uint8

const (
	// ConnNone: the port is not connected, or connected to nothing with ".p()".
	ConnNone ConnKind = iota
	// ConnPositional: "m u (a, b)".
	ConnPositional
	// ConnNamed: ".p(expr)".
	ConnNamed
	// ConnImplicit: ".p", short for ".p(p)".
	ConnImplicit
	// ConnWildcard: connected by ".*" to the signal of the same name.
	ConnWildcard
)

// PortBinding is one port of an instance and what it is connected to.
type PortBinding struct {
	Port      *sema.Symbol
	Name      string
	Direction ast.Direction
	Kind      ConnKind
	// Expr is the connected expression, written in the parent's scope; nil if
	// the port is unconnected or is an interface port.
	Expr ast.Expr
	// Interface is the interface instance an interface port is connected to,
	// and Modport the modport selected, if any.
	Interface *Node
	Modport   string
	// Omitted is true when the instance does not mention the port at all (and
	// ".*" found no signal for it), as opposed to leaving it open on purpose
	// with ".p()". Both leave Kind at ConnNone. LRM 23.3.2 allows either; a
	// port left out by accident is a common mistake, so a lint rule reports it.
	Omitted bool
	// PortType and ExprType are the types on the two sides, resolved for this
	// instance (array bounds follow its parameters). ExprType is Invalid when
	// the expression could not be typed.
	PortType, ExprType sema.Type
}

// ImplicitNet is a net declared by use (LRM 6.10).
type ImplicitNet struct {
	Name  string
	At    ast.Position
	Width int // in bits; 1 if unknown
}

// portSymbols lists a definition's ports in declaration order.
func portSymbols(def *sema.Scope) []*sema.Symbol {
	var out []*sema.Symbol
	for _, sym := range def.Symbols() {
		if sym.Kind == sema.SymPort {
			out = append(out, sym)
		}
	}
	return out
}

func direction(sym *sema.Symbol) ast.Direction {
	if p, ok := sym.Decl.(*ast.Port); ok {
		return p.Direction
	}
	return ast.DirUnspecified
}

// isLvalue reports whether an expression names something that can be driven:
// a net or variable, or a select of one, or a concatenation of those.
func isLvalue(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident, *ast.ScopedName:
		return true
	case *ast.Paren:
		return isLvalue(x.X)
	case *ast.Index:
		return isLvalue(x.X)
	case *ast.RangeSelect:
		return isLvalue(x.X)
	case *ast.MemberSelect:
		return isLvalue(x.X)
	case *ast.Concat:
		for _, el := range x.Elems {
			if !isLvalue(el) {
				return false
			}
		}
		return len(x.Elems) > 0
	case *ast.BadExpr:
		return true
	}
	return false
}

// bindPorts connects the ports of one elaborated instance to the expressions of
// its instantiation (LRM 23.3.2): positional, named, ".name" and ".*", with
// the checks the language requires of each connection.
func (e *elaborator) bindPorts(parent *Node, sc *sema.Scope, inst *Node, in *ast.Instance, copies int) {
	if inst.Scope == nil || inst.Env == nil {
		return
	}
	ports := portSymbols(inst.Scope)
	bindings := make([]PortBinding, len(ports))
	byName := map[string]int{}
	for i, p := range ports {
		bindings[i] = PortBinding{Port: p, Name: p.Name, Direction: direction(p)}
		byName[p.Name] = i
	}

	positional, named := 0, 0
	for i := range in.Connections {
		c := &in.Connections[i]
		if c.Name == "" && !c.Wildcard {
			positional++
		} else if !c.Wildcard {
			named++
		}
	}
	if positional > 0 && named > 0 {
		e.errorf(diag.CodeMixedPortConnections, in.Pos(), "instance %q mixes positional and named port connections", in.Name)
	}

	connected := make([]bool, len(ports))
	wildcard := false
	pos := 0
	for i := range in.Connections {
		c := &in.Connections[i]
		switch {
		case c.Wildcard:
			wildcard = true
		case c.Name == "":
			if pos >= len(ports) {
				e.errorf(diag.CodeTooManyPorts, c.Pos(), "%q has %d ports, not %d", inst.Def.Name, len(ports), positional)
				pos++
				continue
			}
			b := &bindings[pos]
			connected[pos] = true
			if c.ConnExpr != nil {
				b.Kind, b.Expr = ConnPositional, c.ConnExpr
			}
			pos++
		default:
			idx, ok := byName[c.Name]
			if !ok {
				e.errorf(diag.CodeUnknownPort, c.Pos(), "module %q has no port %q", inst.Def.Name, c.Name)
				continue
			}
			if connected[idx] {
				e.errorf(diag.CodeDuplicatePortConnection, c.Pos(), "port %q of %q is connected more than once", c.Name, inst.Def.Name)
				continue
			}
			connected[idx] = true
			b := &bindings[idx]
			switch {
			case c.Implicit:
				b.Kind = ConnImplicit
				id := &ast.Ident{Name: c.Name}
				id.SetPos(c.Pos())
				b.Expr = id
			case c.ConnExpr != nil:
				b.Kind, b.Expr = ConnNamed, c.ConnExpr
			}
		}
	}
	if wildcard {
		for i := range bindings {
			if connected[i] {
				continue
			}
			name := bindings[i].Name
			if r := sc.Lookup(name); r.Symbol != nil || e.siblingInterface(parent, name) != nil {
				id := &ast.Ident{Name: name}
				id.SetPos(in.Pos())
				bindings[i].Kind, bindings[i].Expr = ConnWildcard, id
			}
		}
	}

	for i := range bindings {
		bindings[i].Omitted = !connected[i] && bindings[i].Kind == ConnNone
		e.checkConnection(parent, sc, inst, in, &bindings[i], copies)
	}
	inst.Ports = bindings
}

// siblingInterface finds an interface instance called name that the parent
// holds: one it instantiates, or one it received through an interface port.
func (e *elaborator) siblingInterface(parent *Node, name string) *Node {
	for _, c := range parent.Children {
		if c.Kind == KindInstance && c.Name == name && c.Def != nil && c.Def.Kind == sema.DefInterface {
			return c
		}
	}
	for _, b := range parent.Ports {
		if b.Name == name && b.Interface != nil {
			return b.Interface
		}
	}
	return nil
}

// checkConnection types one connection and applies the direction and width
// rules. copies is the size of the instance array the instance belongs to (1
// for a plain instance).
func (e *elaborator) checkConnection(parent *Node, sc *sema.Scope, inst *Node, in *ast.Instance, b *PortBinding, copies int) {
	pt, ds := e.model.TypeOfIn(b.Port, inst.Env)
	e.addAll(ds)
	b.PortType = pt
	b.ExprType = sema.Invalid
	if b.Expr == nil {
		return
	}
	if vi, ok := sema.Resolve(pt).(*sema.VirtualInterface); ok {
		e.bindInterface(parent, inst, b, vi)
		return
	}

	// A bare name that is declared nowhere is an implicit net (LRM 6.10).
	if id, ok := b.Expr.(*ast.Ident); ok && sc.Lookup(id.Name).Symbol == nil && e.siblingInterface(parent, id.Name) == nil {
		if e.opts.DefaultNettype == "none" {
			e.errorf(diag.CodeUndeclared, id.Pos(), "%q is not declared (`default_nettype none)", id.Name)
			return
		}
		w, _ := sema.Bits(pt)
		if w == 0 {
			w = 1
		}
		parent.ImplicitNets = append(parent.ImplicitNets, ImplicitNet{Name: id.Name, At: id.Pos(), Width: w})
		b.ExprType = pt
		return
	}

	be, ds := e.model.BindIn(sc, b.Expr, parent.Env)
	e.addAll(ds)
	b.ExprType = be.Type()
	if sema.IsInvalid(pt) || sema.IsInvalid(b.ExprType) {
		return
	}
	switch b.Direction {
	case ast.DirOutput:
		if !isLvalue(b.Expr) {
			e.errorf(diag.CodeInvalidOutputConnection, b.Expr.Pos(), "output port %q of %q is connected to an expression that cannot be driven", b.Name, inst.Def.Name)
			return
		}
		if sema.Assignment(b.ExprType, pt) == sema.Incompatible {
			e.errorf(diag.CodeIncompatibleTypes, b.Expr.Pos(), "output port %q (%s) cannot drive %s", b.Name, pt, b.ExprType)
			return
		}
	case ast.DirInout, ast.DirRef:
		if !isLvalue(b.Expr) {
			e.errorf(diag.CodeInvalidInoutConnection, b.Expr.Pos(), "%s port %q of %q needs a net or variable", b.Direction, b.Name, inst.Def.Name)
			return
		}
	default:
		if sema.Assignment(pt, b.ExprType) == sema.Incompatible {
			e.errorf(diag.CodeIncompatibleTypes, b.Expr.Pos(), "%s cannot be connected to input port %q (%s)", b.ExprType, b.Name, pt)
			return
		}
	}
	pw, ok1 := sema.Bits(pt)
	ew, ok2 := sema.Bits(b.ExprType)
	// An instance in an array of n takes a slice of a connection n times as wide
	// as its port (LRM 23.3.3.5).
	if copies > 1 && ew == pw*copies {
		return
	}
	// '0, '1, 'x and 'z fill whatever they are connected to (LRM 5.7.1), so
	// they have no width of their own to disagree with the port's.
	if lit, ok := unparen(b.Expr).(*ast.Literal); ok && lit.Kind == ast.LitUnbasedUnsized {
		return
	}
	if ok1 && ok2 && sema.IsIntegral(pt) && sema.IsIntegral(b.ExprType) && pw != ew {
		e.set.Add(diag.Diagnostic{
			Code: diag.CodePortWidthMismatch, Severity: diag.Warning, Range: b.Expr.Pos(),
			Message: fmt.Sprintf("port %q is %d bits but is connected to %d bits", b.Name, pw, ew),
		})
	}
}

// bindInterface connects an interface port: the expression must name an
// interface instance (or a modport of one) of the interface the port expects.
func (e *elaborator) bindInterface(parent *Node, inst *Node, b *PortBinding, want *sema.VirtualInterface) {
	var name, modport string
	switch x := b.Expr.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.MemberSelect:
		if id, ok := x.X.(*ast.Ident); ok {
			name, modport = id.Name, x.Member
		}
	}
	if name == "" {
		e.errorf(diag.CodeInterfacePortMismatch, b.Expr.Pos(), "interface port %q of %q must be connected to an interface instance", b.Name, inst.Def.Name)
		return
	}
	ifc := e.siblingInterface(parent, name)
	if ifc == nil {
		// An array element, "bus[1]", or a name this scope does not hold.
		e.errorf(diag.CodeInterfacePortMismatch, b.Expr.Pos(), "%q is not an interface instance", name)
		return
	}
	if want.Interface != "" && ifc.Def.Name != want.Interface {
		e.errorf(diag.CodeInterfacePortMismatch, b.Expr.Pos(), "port %q expects interface %q, not %q", b.Name, want.Interface, ifc.Def.Name)
		return
	}
	if modport == "" {
		modport = want.Modport
	}
	if modport != "" {
		if sym, ok := ifc.Scope.Own(modport); !ok || sym.Kind != sema.SymModport {
			e.errorf(diag.CodeInterfacePortMismatch, b.Expr.Pos(), "interface %q has no modport %q", ifc.Def.Name, modport)
			return
		}
	}
	b.Interface, b.Modport = ifc, modport
	b.Expr = nil
}

// implicitNets records the nets implied by continuous assignments (LRM 6.10):
// an undeclared name on the left of "assign". Port connections add theirs in
// bindPorts.
func (e *elaborator) implicitNets(n *Node, scope *sema.Scope, decls []ast.Decl) {
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.ContinuousAssign:
			for i := range d.Assigns {
				id, ok := d.Assigns[i].LHS.(*ast.Ident)
				if !ok || scope.Lookup(id.Name).Symbol != nil {
					continue
				}
				if e.opts.DefaultNettype == "none" {
					e.errorf(diag.CodeUndeclared, id.Pos(), "%q is not declared (`default_nettype none)", id.Name)
					continue
				}
				n.ImplicitNets = append(n.ImplicitNets, ImplicitNet{Name: id.Name, At: id.Pos(), Width: 1})
			}
		case *ast.GenerateRegion:
			e.implicitNets(n, scope, d.Body)
		}
	}
}

// unparen strips the parentheses around an expression.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.Paren)
		if !ok {
			return e
		}
		e = p.X
	}
}
