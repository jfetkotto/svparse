package lint

import (
	"strings"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/elab"
	"github.com/jfetkotto/svparse/sema"
)

// Usage records, for every symbol of the design, whether anything reads it and
// whether anything writes it. "Anything" is the source text of every design
// element: expressions, assignment targets, instance port connections, output
// arguments of calls and hierarchical references from other modules.
//
// It errs towards "used": a name it cannot place exactly is counted as both
// read and written, so a rule built on it reports less rather than wrongly.
type Usage struct {
	read, write map[*sema.Symbol]bool
}

// Read reports whether the symbol's value is used anywhere.
func (u *Usage) Read(s *sema.Symbol) bool { return u.read[s] }

// Written reports whether anything drives the symbol, an initializer included.
func (u *Usage) Written(s *sema.Symbol) bool { return u.write[s] }

const (
	dirRead  = 1
	dirWrite = 2
)

func buildUsage(comp *sema.Compilation, model *sema.Model, design *elab.Design) *Usage {
	u := &Usage{read: map[*sema.Symbol]bool{}, write: map[*sema.Symbol]bool{}}
	// How each connection expression is used, from the port directions of the
	// instances that were elaborated. A connection never elaborated (in a
	// generate branch not taken) is not in the map and counts as both.
	dirs := map[ast.Expr]int{}
	if design != nil {
		design.Walk(func(n *elab.Node) bool {
			for _, b := range n.Ports {
				if b.Expr == nil {
					continue
				}
				switch b.Direction {
				case ast.DirOutput:
					dirs[b.Expr] |= dirWrite
				case ast.DirInout, ast.DirRef:
					dirs[b.Expr] |= dirRead | dirWrite
				default:
					dirs[b.Expr] |= dirRead
				}
			}
			return true
		})
	}
	for _, f := range comp.Files() {
		us, _ := model.UnitScope(f.Path)
		ast.Walk(&usageWalker{u: u, comp: comp, model: model, scope: us, dirs: dirs}, f)
	}
	return u
}

type usageWalker struct {
	u     *Usage
	comp  *sema.Compilation
	model *sema.Model
	scope *sema.Scope
	dirs  map[ast.Expr]int
}

func (w *usageWalker) mark(sym *sema.Symbol, read, write bool) {
	if sym == nil {
		return
	}
	if read {
		w.u.read[sym] = true
	}
	if write {
		w.u.write[sym] = true
	}
}

func (w *usageWalker) at(s *sema.Scope) *usageWalker {
	n := *w
	n.scope = s
	return &n
}

// ref marks what a name expression refers to: an identifier, a hierarchical
// name through instances, or a package or class member.
func (w *usageWalker) ref(e ast.Expr, read, write bool) {
	if w.scope == nil {
		return
	}
	switch e := e.(type) {
	case *ast.Ident:
		if !strings.HasPrefix(e.Name, "$") {
			w.mark(w.scope.Lookup(e.Name).Symbol, read, write)
		}
	case *ast.ScopedName:
		if path, ok := scopedPath(e); ok {
			w.mark(w.scope.LookupQualified(path).Symbol, read, write)
		}
	case *ast.MemberSelect:
		path, ok := dottedPath(e)
		if !ok {
			return
		}
		if first := w.scope.Lookup(path[0]).Symbol; first != nil && isValue(first.Kind) {
			w.mark(first, read, write) // a field of a variable is a use of the variable
			return
		}
		sym, _ := w.scope.LookupPath(path)
		w.mark(sym, read, write)
	}
}

func isValue(k sema.SymKind) bool {
	switch k {
	case sema.SymVariable, sema.SymNet, sema.SymPort, sema.SymArg, sema.SymParameter, sema.SymLoopVar, sema.SymGenvar:
		return true
	}
	return false
}

func dottedPath(e ast.Expr) ([]string, bool) {
	switch e := e.(type) {
	case *ast.Ident:
		return []string{e.Name}, true
	case *ast.MemberSelect:
		p, ok := dottedPath(e.X)
		return append(p, e.Member), ok
	}
	return nil, false
}

func scopedPath(e *ast.ScopedName) ([]string, bool) {
	var path []string
	var rec func(x ast.Expr) bool
	rec = func(x ast.Expr) bool {
		switch x := x.(type) {
		case *ast.Ident:
			path = append(path, x.Name)
			return true
		case *ast.ParameterizedName:
			if x.Scope != nil && !rec(x.Scope) {
				return false
			}
			path = append(path, x.Name)
			return true
		case *ast.ScopedName:
			if !rec(x.Scope) {
				return false
			}
			path = append(path, x.Name)
			return true
		}
		return false
	}
	return path, rec(e)
}

// lvalue marks the target of an assignment. The signals being assigned are
// written (and read too if read is set, as for "x += 1"); the index
// expressions inside the target are ordinary reads.
func (w *usageWalker) lvalue(e ast.Expr, read bool) {
	switch e := e.(type) {
	case *ast.Paren:
		w.lvalue(e.X, read)
	case *ast.Index:
		w.lvalue(e.X, read)
		ast.Walk(w, e.Index)
	case *ast.RangeSelect:
		w.lvalue(e.X, read)
		ast.Walk(w, e.Left)
		ast.Walk(w, e.Right)
	case *ast.WithRange:
		w.lvalue(e.X, read)
		ast.Walk(w, e.Left)
		if e.Right != nil {
			ast.Walk(w, e.Right)
		}
	case *ast.Concat:
		for _, el := range e.Elems {
			w.lvalue(el, read)
		}
	case *ast.Ident, *ast.ScopedName:
		w.ref(e, read, true)
	case *ast.MemberSelect:
		if _, ok := dottedPath(e); ok {
			w.ref(e, read, true)
		} else {
			w.lvalue(e.X, read)
		}
	case nil:
	default:
		ast.Walk(w, e)
	}
}

// both marks every plain name in an expression as read and written, for places
// where the direction is unknown.
func (w *usageWalker) both(e ast.Expr) {
	ast.Inspect(e, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.Ident, *ast.ScopedName, *ast.MemberSelect:
			w.ref(n.(ast.Expr), true, true)
			return false
		}
		return true
	})
}

func (w *usageWalker) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case nil:
		return nil
	case *ast.Ident, *ast.ScopedName:
		w.ref(n.(ast.Expr), true, false)
		return nil
	case *ast.MemberSelect:
		if _, ok := dottedPath(n); ok {
			w.ref(n, true, false)
			return nil
		}
	case *ast.Variable:
		if n.InitialExpr != nil && w.scope != nil {
			if sym, ok := w.scope.Own(n.Name); ok {
				w.mark(sym, false, true) // the initializer drives it
			}
		}
	case *ast.Assign:
		w.lvalue(n.LHS, n.Op != "=" && n.Op != "<=")
		ast.Walk(w, n.RHS)
		if n.Control != nil {
			ast.Walk(w, n.Control)
		}
		return nil
	case *ast.ProcAssign:
		w.lvalue(n.LHS, false)
		if n.RHS != nil {
			ast.Walk(w, n.RHS)
		}
		return nil
	case *ast.AssignExpr:
		w.lvalue(n.LHS, n.Op != "=")
		ast.Walk(w, n.RHS)
		return nil
	case *ast.IncDec:
		w.lvalue(n.X, true)
		return nil
	case *ast.ContinuousAssign:
		for i := range n.Assigns {
			w.lvalue(n.Assigns[i].LHS, false)
			ast.Walk(w, n.Assigns[i].RHS)
		}
		for _, d := range n.Delay {
			ast.Walk(w, d)
		}
		return nil
	case *ast.Call:
		w.call(n)
		return nil
	case *ast.Instantiation:
		w.wildcards(n)
	case *ast.PortConnection:
		w.connection(n)
		return nil
	case *ast.Foreach:
		w.ref(n.Array, true, false)
	}
	if s, ok := w.model.ScopeOf(n); ok {
		return w.at(s)
	}
	return w
}

// call marks the arguments of a call. An argument bound to an output, inout or
// ref formal is written; for a callee not resolved (a system task like
// $value$plusargs, a method) any plain name argument is taken as both.
func (w *usageWalker) call(c *ast.Call) {
	ast.Walk(w, c.Func)
	var formals []ast.Arg
	known := false
	if id, ok := c.Func.(*ast.Ident); ok && w.scope != nil && !strings.HasPrefix(id.Name, "$") {
		if sym := w.scope.Lookup(id.Name).Symbol; sym != nil {
			switch d := sym.Decl.(type) {
			case *ast.Function:
				formals, known = d.Args, true
			case *ast.Task:
				formals, known = d.Args, true
			}
		}
	}
	for i, a := range c.Args {
		value := a
		var formal *ast.Arg
		switch a := a.(type) {
		case *ast.NamedArg:
			value = a.Value
			for k := range formals {
				if formals[k].Name == a.Name {
					formal = &formals[k]
				}
			}
		default:
			if i < len(formals) {
				formal = &formals[i]
			}
		}
		if value == nil {
			continue
		}
		switch {
		case known && formal != nil && (formal.Direction == ast.DirOutput):
			w.lvalue(value, false)
		case known && formal != nil && (formal.Direction == ast.DirInout || formal.Direction == ast.DirRef):
			w.lvalue(value, true)
		case known:
			ast.Walk(w, value)
		case isSystemCall(c) && !writingSystemCalls[c.Func.(*ast.Ident).Name]:
			ast.Walk(w, value) // $display and the rest only read
		default:
			w.both(value)
		}
	}
	if c.With != nil {
		ast.Walk(w, c.With)
	}
	if c.Constraints != nil {
		ast.Walk(w, c.Constraints)
	}
}

// writingSystemCalls are the system tasks and functions that store into one of
// their arguments.
var writingSystemCalls = map[string]bool{
	"$value$plusargs": true, "$sscanf": true, "$fscanf": true, "$fgets": true, "$fread": true,
	"$cast": true, "$readmemh": true, "$readmemb": true, "$sformat": true, "$swrite": true,
	"$swriteb": true, "$swriteh": true, "$swriteo": true, "$random": true, "$ungetc": true,
	"$fgetc": true, "$fseek": true, "$fscanf ": true,
}

func isSystemCall(c *ast.Call) bool {
	id, ok := c.Func.(*ast.Ident)
	return ok && strings.HasPrefix(id.Name, "$")
}

// connection marks one port connection by the direction of its port.
func (w *usageWalker) connection(c *ast.PortConnection) {
	if c.Wildcard {
		return // handled with the instantiation
	}
	if c.ConnExpr == nil {
		if c.Implicit && w.scope != nil {
			w.mark(w.scope.Lookup(c.Name).Symbol, true, true)
		}
		return
	}
	d, elaborated := w.dirs[c.ConnExpr]
	if !elaborated {
		w.both(c.ConnExpr)
		return
	}
	if d&dirRead != 0 {
		ast.Walk(w, c.ConnExpr)
	}
	if d&dirWrite != 0 {
		w.lvalue(c.ConnExpr, false)
	}
}

// wildcards marks the signals an instance's ".*" connects: every name in scope
// that is also a port name of the instantiated module.
func (w *usageWalker) wildcards(inst *ast.Instantiation) {
	hasWild := false
	for i := range inst.Instances {
		for j := range inst.Instances[i].Connections {
			if inst.Instances[i].Connections[j].Wildcard {
				hasWild = true
			}
		}
	}
	if !hasWild || w.scope == nil {
		return
	}
	def, ok := w.comp.Lookup(inst.ModuleType)
	if !ok {
		return
	}
	ds, ok := w.model.DefinitionScope(def)
	if !ok {
		return
	}
	for _, p := range ds.Symbols() {
		if p.Kind == sema.SymPort {
			w.mark(w.scope.Lookup(p.Name).Symbol, true, true)
		}
	}
}
