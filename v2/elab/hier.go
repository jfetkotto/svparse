package elab

import (
	"strings"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
)

// HierRef is the result of resolving a hierarchical name.
type HierRef struct {
	// Node is the instance or generate block the name reaches, or, when it ends
	// at a signal, the one that declares the signal.
	Node *Node
	// Sym is the declaration the name ends at (a signal, parameter, ...), or nil
	// if it ends at an instance or block.
	Sym *sema.Symbol
	// Rest holds the components after a symbol with no hierarchy of its own: the
	// field names of "s.a.b".
	Rest []string
}

// ResolveHier resolves a hierarchical name "a.b.c", given as []string{"a", "b",
// "c"} with array and loop indices kept in the component ("g[1]", "u[0]"), as
// seen from n (LRM 23.6, 23.8). The first component is searched upward: among n's
// own children and declarations, then its parent's, out to the tops; "$root"
// starts at the tops. Each later component is looked up inside the node the
// previous one reached.
func (n *Node) ResolveHier(path []string) (HierRef, bool) {
	if n == nil || len(path) == 0 {
		return HierRef{}, false
	}
	if path[0] == "$root" {
		// Only the top n belongs to is reachable from a node; Design.ResolveHier
		// reaches the others.
		root := n
		for root.Parent != nil {
			root = root.Parent
		}
		if len(path) < 2 || root.Name != path[1] {
			return HierRef{}, false
		}
		return resolveInside(root, path[2:])
	}
	var cur *Node
	rest := path
	for scope := n; scope != nil && cur == nil; scope = scope.Parent {
		switch {
		case scope.Name == path[0] && scope.Kind == KindInstance:
			cur, rest = scope, path[1:]
		case scope.Child(path[0]) != nil:
			cur, rest = scope.Child(path[0]), path[1:]
		case scope.Scope != nil:
			if sym, ok := scope.Scope.Own(path[0]); ok && !isHierarchical(sym.Kind) {
				return HierRef{Node: scope, Sym: sym, Rest: path[1:]}, true
			}
		}
	}
	if cur == nil {
		return HierRef{}, false
	}
	return resolveInside(cur, rest)
}

// isHierarchical reports whether a symbol names a place with a scope of its own,
// which the node tree represents as a child instead.
func isHierarchical(k sema.SymKind) bool {
	return k == sema.SymInstance || k == sema.SymGenerateBlock
}

// resolveInside follows the remaining components inside a node.
func resolveInside(cur *Node, rest []string) (HierRef, bool) {
	for len(rest) > 0 {
		if c := cur.Child(rest[0]); c != nil {
			cur, rest = c, rest[1:]
			continue
		}
		if cur.Scope != nil {
			if sym, ok := cur.Scope.Own(rest[0]); ok && !isHierarchical(sym.Kind) {
				return HierRef{Node: cur, Sym: sym, Rest: rest[1:]}, true
			}
		}
		return HierRef{}, false
	}
	return HierRef{Node: cur}, true
}

// ResolveHier resolves an absolute hierarchical name starting at a top.
func (d *Design) ResolveHier(path []string) (HierRef, bool) {
	if len(path) == 0 {
		return HierRef{}, false
	}
	for _, t := range d.Tops {
		if t.Name == path[0] {
			return resolveInside(t, path[1:])
		}
	}
	return HierRef{}, false
}

// applyBinds elaborates every bind directive (LRM 23.11): its instantiation is
// added to each instance the directive targets, as if instantiated at the end
// of that instance's body, with its connections written in the target's scope.
func (e *elaborator) applyBinds(d *Design) {
	var binds []*ast.Bind
	for _, f := range e.comp.Files() {
		ast.Inspect(f, func(n ast.Node) bool {
			if b, ok := n.(*ast.Bind); ok && b.Inst != nil {
				binds = append(binds, b)
			}
			return true
		})
	}
	for _, b := range binds {
		if e.cancelled() {
			return
		}
		targets := e.bindTargets(d, b)
		if len(targets) == 0 {
			e.errorf(diag.CodeUnknownBindTarget, b.Pos(), "bind target %q matches no module or instance", b.Target)
			continue
		}
		for _, t := range targets {
			if t.Scope == nil || t.Env == nil {
				continue
			}
			e.instantiate(t, t.Scope, b.Inst)
		}
	}
}

// bindTargets finds the nodes a bind directive adds its instance to. The target
// is a module name (all its instances) or a hierarchical instance name; with
// "bind m : a, b ..." it is the instances a and b inside each instance of m.
func (e *elaborator) bindTargets(d *Design, b *ast.Bind) []*Node {
	var base []*Node
	d.Walk(func(n *Node) bool {
		if n.Kind == KindInstance && n.Def != nil && n.Def.Name == b.Target {
			base = append(base, n)
		}
		return true
	})
	if len(base) == 0 {
		if ref, ok := d.ResolveHier(splitPath(b.Target)); ok && ref.Sym == nil && ref.Node.Kind == KindInstance {
			base = []*Node{ref.Node}
		}
	}
	if len(b.Instances) == 0 {
		return base
	}
	var out []*Node
	for _, n := range base {
		for _, name := range b.Instances {
			if t := n.Find(strings.TrimSpace(name)); t != nil && t.Kind == KindInstance {
				out = append(out, t)
			}
		}
	}
	return out
}
