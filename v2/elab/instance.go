package elab

import (
	"strconv"
	"strings"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/sema"
)

// override is a value given to a parameter from outside: by an instantiation's
// "#(...)" list or by a defparam. It is evaluated where it was written (scope,
// env), not in the instance it applies to.
type override struct {
	expr  ast.Expr // nil for ".P()", which leaves the default
	scope *sema.Scope
	env   *sema.Env
	at    ast.Position
}

// defparam is a pending "defparam a.b.P = value" (LRM 23.10.1), with the path
// relative to the node it applies below.
type defparam struct {
	path  []string
	expr  ast.Expr
	scope *sema.Scope
	env   *sema.Env
	at    ast.Position
}

// instance elaborates one instance of def. decl is the *ast.Instance it comes
// from (nil for a top), ov the overrides of its instantiation and forwarded
// the defparams that reach it from above, each with the path starting at the
// instance's own parameters. It returns nil only if elaboration was stopped.
func (e *elaborator) instance(def *sema.Definition, name string, parent *Node, decl ast.Node, ov map[string]override, forwarded []defparam, connect func(*Node)) *Node {
	if e.cancelled() {
		return nil
	}
	at := def.Pos()
	if decl != nil {
		at = decl.Pos()
	}
	if !e.reserve(at) {
		return nil
	}
	n := &Node{Kind: KindInstance, Name: name, Parent: parent, Def: def, Decl: decl}
	if parent != nil {
		parent.Children = append(parent.Children, n)
	}
	scope, ok := e.model.DefinitionScope(def)
	if !ok {
		return n
	}
	n.Scope = scope

	depth := 0
	for p := parent; p != nil; p = p.Parent {
		depth++
	}
	if depth >= e.opts.MaxDepth {
		e.errorf(diag.CodeInstantiationDepth, at, "the hierarchy is nested more than %d deep at %s", e.opts.MaxDepth, n.Path())
		return n
	}

	overrides := map[string]override{}
	for k, v := range ov {
		overrides[k] = v
	}
	for _, dp := range forwarded {
		if len(dp.path) == 1 {
			overrides[dp.path[0]] = override{expr: dp.expr, scope: dp.scope, env: dp.env, at: dp.at} // a defparam beats a #() override
		} else {
			n.defparams = append(n.defparams, dp)
		}
	}
	e.resolveParams(n, scope, overrides)

	if e.recursive(n) {
		e.errorf(diag.CodeRecursiveInstantiation, at, "module %q instantiates itself with the same parameters at %s", def.Name, n.Path())
		return n
	}
	if connect != nil {
		connect(n) // before the body, so interface ports are known to the children
	}
	if cont, ok := def.Decl.(*ast.Container); ok {
		e.collectDefparams(n, scope, cont.Body)
		e.implicitNets(n, scope, cont.Body)
		e.items(n, scope, cont.Body)
	}
	return n
}

// signature identifies an instance by definition and parameter values.
func signature(n *Node) string {
	var sb strings.Builder
	sb.WriteString(n.Def.Name)
	for _, p := range n.Params {
		sb.WriteString("|" + p.Name + "=")
		if p.IsType {
			if p.TypeValue != nil {
				sb.WriteString(p.TypeValue.String())
			}
		} else if p.Valid {
			sb.WriteString(p.Value.String())
		} else {
			sb.WriteString("?")
		}
	}
	return sb.String()
}

// recursive reports whether an ancestor is an instance of the same definition
// with the same parameter values: instantiating it again would never end.
func (e *elaborator) recursive(n *Node) bool {
	sig := signature(n)
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Kind == KindInstance && p.Def == n.Def && signature(p) == sig {
			return true
		}
	}
	return false
}

// paramSymbols lists the parameters declared directly in a scope, in order.
func paramSymbols(s *sema.Scope) []*sema.Symbol {
	var out []*sema.Symbol
	for _, sym := range s.Symbols() {
		if sym.Kind == sema.SymParameter || sym.Kind == sema.SymTypeParam {
			out = append(out, sym)
		}
	}
	return out
}

// isLocal reports whether a parameter cannot be overridden: a localparam, or a
// parameter in the body of a module that has a parameter port list (LRM 6.20.1).
func isLocal(scope *sema.Scope, sym *sema.Symbol) bool {
	p, ok := sym.Decl.(*ast.Parameter)
	if !ok {
		return false
	}
	if p.IsLocal {
		return true
	}
	if cont, ok := scope.Node.(*ast.Container); ok && len(cont.Params) > 0 {
		for i := range cont.Params {
			if &cont.Params[i] == p {
				return false
			}
		}
		return true // declared in the body
	}
	return false
}

// overridable lists the parameter names a positional override list maps to.
func overridable(scope *sema.Scope) []string {
	var out []string
	for _, sym := range paramSymbols(scope) {
		if !isLocal(scope, sym) {
			out = append(out, sym.Name)
		}
	}
	return out
}

// resolveParams evaluates every parameter of n and records it in n.Params. The
// overrides are evaluated where they were written, then all go into the node's
// environment before any default is computed, so a default that depends on an
// overridden parameter sees the override.
func (e *elaborator) resolveParams(n *Node, scope *sema.Scope, overrides map[string]override) {
	params := map[*sema.Symbol]sema.Const{}
	typeOverrides := map[*sema.Symbol]sema.Type{}
	bySym := map[string]*sema.Symbol{}
	syms := paramSymbols(scope)
	for _, sym := range syms {
		bySym[sym.Name] = sym
	}
	// Deterministic order: the declaration order of the parameters overridden.
	overridden := map[*sema.Symbol]bool{}
	for _, sym := range syms {
		ov, ok := overrides[sym.Name]
		if !ok {
			continue
		}
		if isLocal(scope, sym) {
			e.errorf(diag.CodeLocalParameterOverride, ov.at, "parameter %q of %s cannot be overridden", sym.Name, n.Def.Name)
			continue
		}
		if ov.expr == nil {
			continue
		}
		overridden[sym] = true
		if sym.Kind == sema.SymTypeParam || e.model.IsTypeExpr(ov.scope, ov.expr) {
			t, ds := e.model.ResolveTypeExpr(ov.scope, ov.expr)
			e.addAll(ds)
			typeOverrides[sym] = t
			continue
		}
		var c sema.Const
		var ds []diag.Diagnostic
		if p, ok := sym.Decl.(*ast.Parameter); ok && (p.Type.Name != "" || len(p.Type.PackedDims) > 0) {
			t, tds := e.model.TypeOf(sym)
			e.addAll(tds)
			c, ds = e.model.EvalAssign(ov.scope, t, ov.expr, ov.env)
		} else {
			c, ds = e.model.Eval(ov.scope, ov.expr, ov.env)
		}
		e.addAll(ds)
		if len(ds) == 0 {
			params[sym] = c
		}
	}
	for name, ov := range overrides {
		if _, ok := bySym[name]; !ok {
			e.errorf(diag.CodeUnknownParameter, ov.at, "module %q has no parameter %q", n.Def.Name, name)
		}
	}

	n.Env = e.newEnv(params, nil)
	if n.Parent != nil {
		n.Env.Vars = copyVars(n.Parent.Env)
	}
	e.evalParams(n, scope, syms, overridden, typeOverrides)
}

func copyVars(env *sema.Env) map[*sema.Symbol]sema.Const {
	if env == nil || len(env.Vars) == 0 {
		return nil
	}
	out := make(map[*sema.Symbol]sema.Const, len(env.Vars))
	for k, v := range env.Vars {
		out[k] = v
	}
	return out
}

// evalParams fills n.Params from the symbols, evaluating each in n.Env.
func (e *elaborator) evalParams(n *Node, scope *sema.Scope, syms []*sema.Symbol, overridden map[*sema.Symbol]bool, typeOverrides map[*sema.Symbol]sema.Type) {
	for _, sym := range syms {
		p := Param{Name: sym.Name, Sym: sym, Local: isLocal(scope, sym), Overridden: overridden[sym]}
		if sym.Kind == sema.SymTypeParam {
			p.IsType = true
			if t, ok := typeOverrides[sym]; ok {
				p.TypeValue, p.Valid = t, !sema.IsInvalid(t)
			} else if decl, ok := sym.Decl.(*ast.Parameter); ok && decl.DefaultExpr != nil {
				t, ds := e.model.ResolveTypeExpr(sym.Scope, decl.DefaultExpr)
				e.addAll(ds)
				p.TypeValue, p.Valid = t, !sema.IsInvalid(t)
			}
		} else {
			c, ds := e.model.ParamValue(sym, n.Env)
			e.addAll(ds)
			p.Value, p.Valid = c, len(ds) == 0
		}
		n.Params = append(n.Params, p)
	}
}

// collectDefparams records the defparams written directly in a body (or in its
// generate regions), to apply to the instances below n.
func (e *elaborator) collectDefparams(n *Node, scope *sema.Scope, decls []ast.Decl) {
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.Defparam:
			for i := range d.Assigns {
				a := &d.Assigns[i]
				path, ok := dotted(a.LHS)
				if !ok || len(path) < 2 {
					continue
				}
				if n.Parent == nil && path[0] == n.Name && len(path) > 2 {
					path = path[1:] // "top.u.P" written inside top
				}
				n.defparams = append(n.defparams, defparam{path: path, expr: a.RHS, scope: scope, env: n.Env, at: a.Pos()})
			}
		case *ast.GenerateRegion:
			e.collectDefparams(n, scope, d.Body)
		}
	}
}

func dotted(x ast.Expr) ([]string, bool) {
	switch x := x.(type) {
	case *ast.Ident:
		return []string{x.Name}, true
	case *ast.MemberSelect:
		p, ok := dotted(x.X)
		return append(p, x.Member), ok
	}
	return nil, false
}

// forwardedFor returns the defparams of n (and its ancestors', already
// forwarded) that apply to the child with the given base name, with the path
// starting below it.
func forwardedFor(n *Node, child string) []defparam {
	var out []defparam
	for _, dp := range n.defparams {
		if dp.path[0] == child {
			out = append(out, defparam{path: dp.path[1:], expr: dp.expr, scope: dp.scope, env: dp.env, at: dp.at})
		}
	}
	return out
}

// instantiate elaborates the instances of one instantiation statement.
func (e *elaborator) instantiate(n *Node, sc *sema.Scope, inst *ast.Instantiation) {
	def, ok := e.comp.Lookup(inst.ModuleType)
	if !ok {
		e.errorf(diag.CodeUnknownModule, inst.Pos(), "module, interface or program %q is not defined", inst.ModuleType)
		return
	}
	if def.Kind == sema.DefUDP {
		return // a primitive instance has no hierarchy below it
	}
	defScope, ok := e.model.DefinitionScope(def)
	if !ok {
		return
	}
	names := overridable(defScope)
	ov := map[string]override{}
	for i := range inst.ParamOverrides {
		po := &inst.ParamOverrides[i]
		name := po.Name
		if name == "" {
			if i >= len(names) {
				e.errorf(diag.CodeTooManyParameters, po.Pos(), "%q has %d overridable parameters, not %d", def.Name, len(names), len(inst.ParamOverrides))
				break
			}
			name = names[i]
		}
		ov[name] = override{expr: po.ValueExpr, scope: sc, env: n.Env, at: po.Pos()}
	}
	for i := range inst.Instances {
		if e.cancelled() {
			return
		}
		in := &inst.Instances[i]
		els := e.elements(n, sc, in)
		for _, el := range els {
			e.instance(def, el.name, n, in, ov, forwardedFor(n, in.Name), func(child *Node) { e.bindPorts(n, sc, child, in, len(els)) })
		}
	}
}

type element struct {
	name string
}

// elements expands an instance array "u[3:0][1:0]" into its elements, named
// "u[3][1]", ...; a plain instance is one element.
func (e *elaborator) elements(n *Node, sc *sema.Scope, in *ast.Instance) []element {
	if len(in.UnpackedDims) == 0 {
		return []element{{in.Name}}
	}
	names := []string{in.Name}
	for i := range in.UnpackedDims {
		d := &in.UnpackedDims[i]
		idx, ok := e.indexes(sc, n.Env, d, in.Pos())
		if !ok {
			return nil
		}
		var next []string
		for _, base := range names {
			for _, k := range idx {
				next = append(next, base+"["+strconv.Itoa(k)+"]")
				if len(next) > e.opts.MaxInstances {
					e.errorf(diag.CodeInstanceLimit, in.Pos(), "instance array %q is larger than the instance limit", in.Name)
					return nil
				}
			}
		}
		names = next
	}
	out := make([]element, len(names))
	for i, nm := range names {
		out[i] = element{nm}
	}
	return out
}

// indexes lists the indices of one array dimension, in declaration order.
func (e *elaborator) indexes(sc *sema.Scope, env *sema.Env, d *ast.Dim, at ast.Position) ([]int, bool) {
	if d.LeftExpr == nil {
		e.errorf(diag.CodeNotConstant, at, "an instance array needs a size")
		return nil, false
	}
	evalInt := func(x ast.Expr) (int64, bool) {
		c, ds := e.model.Eval(sc, x, env)
		e.addAll(ds)
		v, ok := c.Int64()
		if !ok && len(ds) == 0 {
			e.errorf(diag.CodeNotConstant, x.Pos(), "an array bound must be a constant integer")
		}
		return v, ok
	}
	l, ok := evalInt(d.LeftExpr)
	if !ok {
		return nil, false
	}
	r, size := int64(0), int64(0)
	if d.RightExpr == nil {
		r, l, size = l-1, 0, l // "[N]" is [0:N-1]
	} else {
		if r, ok = evalInt(d.RightExpr); !ok {
			return nil, false
		}
		size = abs(l-r) + 1
	}
	if size < 0 || size > int64(e.opts.MaxInstances) {
		e.errorf(diag.CodeInstanceLimit, at, "instance array of %d elements exceeds the limit", size)
		return nil, false
	}
	out := make([]int, 0, size)
	step := int64(1)
	if l > r {
		step = -1
	}
	for k := l; ; k += step {
		out = append(out, int(k))
		if k == r {
			break
		}
	}
	return out, true
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
