package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// CheckNames binds every expression of the design elements (modules,
// interfaces, programs, packages) and returns the names that do not resolve
// (LRM 23.9, 26): an identifier used in an expression, statement, port
// connection, range or initialiser that no enclosing scope, import or package
// declares, and calls of functions that are not declared. The result is sorted
// by position.
//
// It reports undeclared names (and a few expressions that are never legal, see
// diag.CodeInvalidExpression). Other type and width problems are for the
// callers that need them (Bind returns them). Class bodies, constraints,
// randsequences and the bodies of properties and sequences are looked into;
// a class whose base cannot be found is skipped, since it inherits members
// that cannot be seen. Covergroups, clocking blocks and primitives are not
// looked into yet because their scopes need more than the name tables.
//
// A bare identifier is not reported where the language declares it implicitly
// (LRM 6.10): the target of a continuous assignment and a signal wired to an
// instance or gate terminal become one-bit nets (see the elaborator's
// ImplicitNets for those).
func (m *Model) CheckNames() []diag.Diagnostic { return m.CheckNamesIn(nil) }

// CheckNamesIn is CheckNames for the files whose path is in files (nil means
// all of them): an editor that has changed one file of a large design needs
// the answer for that file only. A name is still resolved against the whole
// compilation; what is skipped is binding the expressions of the other files.
func (m *Model) CheckNamesIn(files map[string]bool) []diag.Diagnostic {
	c := &nameChecker{m: m, implicit: map[*Scope]map[string]bool{}}
	for _, f := range m.comp.files {
		if files != nil && !files[f.Path] {
			continue
		}
		us, _ := m.UnitScope(f.Path)
		ast.Walk(&nameWalker{c: c, scope: us, collect: true}, f)
	}
	for _, f := range m.comp.files {
		if files != nil && !files[f.Path] {
			continue
		}
		us, _ := m.UnitScope(f.Path)
		ast.Walk(&nameWalker{c: c, scope: us}, f)
	}
	return c.set.Sorted()
}

type nameChecker struct {
	m   *Model
	set diag.Set
	// implicit holds, per design element, the names its connections and
	// continuous assignments declare implicitly (LRM 6.10), which later
	// expressions may use.
	implicit map[*Scope]map[string]bool
	// ignore holds names that are in scope without a declaration the scope
	// tables know: inside a randsequence, the productions, their arguments and
	// the variables of its code blocks (LRM 18.17).
	ignore map[string]bool
	// suppress is positive while walking a class whose base is unknown.
	suppress int
}

// element is the design element scope s is part of.
func element(s *Scope) *Scope {
	for ; s != nil; s = s.Parent {
		switch s.Kind {
		case ScopeModule, ScopeInterface, ScopeProgram:
			return s
		}
	}
	return nil
}

// note records name as implicitly declared in s's design element, if nothing
// declares it.
func (c *nameChecker) note(s *Scope, e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
		if s == nil || s.Lookup(e.Name).Symbol != nil {
			return
		}
		el := element(s)
		if el == nil {
			return
		}
		if c.implicit[el] == nil {
			c.implicit[el] = map[string]bool{}
		}
		c.implicit[el][e.Name] = true
	case *ast.Paren:
		c.note(s, e.X)
	case *ast.Concat:
		for _, el := range e.Elems {
			c.note(s, el)
		}
	}
}

// bind resolves every name of e in scope s.
func (c *nameChecker) bind(s *Scope, e ast.Expr) {
	if s == nil || e == nil {
		return
	}
	_, ds := c.m.Bind(s, e)
	for _, d := range ds {
		if d.Code == diag.CodeInvalidExpression {
			c.set.Add(d)
			continue
		}
		if c.suppress > 0 {
			continue
		}
		if d.Code != diag.CodeUndeclared {
			continue
		}
		if id := identAt(e, d.Range); id != nil && (c.implicit[element(s)][id.Name] || c.ignore[id.Name]) {
			continue
		}
		c.set.Add(d)
	}
}

// noSpecparams reports a specparam used in the value of a parameter: the two
// live in different worlds (LRM 6.20.5).
func (c *nameChecker) noSpecparams(s *Scope, p *ast.Parameter) {
	if s == nil || p.DefaultExpr == nil {
		return
	}
	ast.Inspect(p.DefaultExpr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if sym := s.Lookup(id.Name).Symbol; sym != nil {
				if sp, ok := sym.Decl.(*ast.Parameter); ok && sp.IsSpecparam {
					c.set.Add(diag.Diagnostic{Code: diag.CodeInvalidExpression, Severity: diag.Error, Range: id.Pos(),
						Message: "specparam " + id.Name + " cannot be used in the value of a parameter (LRM 6.20.5)"})
				}
			}
		}
		return true
	})
}

// typeName reports a type written by name that nothing declares.
func (c *nameChecker) typeName(s *Scope, t *ast.Type) {
	if s == nil || t.Name == "" || t.Inline != nil {
		return
	}
	_, ds := c.m.ResolveType(s, t)
	for _, d := range ds {
		if d.Code == diag.CodeUndeclared {
			c.set.Add(d)
		}
	}
}

// typedef checks what a typedef names: an alias needs its type to exist, and a
// bare forward declaration ("typedef T;") needs a type T declared in the same
// scope (LRM 6.18).
func (c *nameChecker) typedef(s *Scope, t *ast.Typedef) {
	if s == nil {
		return
	}
	switch u := t.Underlying.(type) {
	case *ast.TypeAlias:
		c.typeName(s, &u.Type)
	case nil:
		if t.ForwardKind == "" && s.Lookup(t.Name).Symbol == nil {
			c.set.Add(diag.Diagnostic{Code: diag.CodeUndeclared, Severity: diag.Error, Range: t.Pos(),
				Message: "forward typedef " + t.Name + " is never defined (LRM 6.18)"})
		}
	}
}

// edgeTerm binds one entry of an event expression and refuses an edge of a
// real value (LRM 6.12).
func (c *nameChecker) edgeTerm(s *Scope, t *ast.EventTerm) {
	c.bind(s, t.X)
	c.bind(s, t.Iff)
	if t.Edge == "" || t.X == nil || s == nil {
		return
	}
	if be, _ := c.m.Bind(s, t.X); be != nil && IsReal(be.Type()) {
		c.set.Add(diag.Diagnostic{Code: diag.CodeInvalidExpression, Severity: diag.Error, Range: t.X.Pos(),
			Message: "a real value has no " + t.Edge + " (LRM 6.12)"})
	}
}

// identAt finds the identifier of e that starts at pos.
func identAt(e ast.Expr, pos ast.Position) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if p := id.Pos(); p.Line == pos.Line && p.Character == pos.Character {
				found = id
			}
		}
		return found == nil
	})
	return found
}

// nameWalker walks the design. In collect mode it only gathers the implicit
// net declarations; in the second pass it binds every expression.
type nameWalker struct {
	c       *nameChecker
	scope   *Scope
	collect bool
}

func (w *nameWalker) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case nil:
		return nil
	case *ast.GenerateFor:
		// The loop header sees the loop's own genvar, which lives in the body's
		// scope (LRM 27.4).
		hs := w.scope
		if bs, ok := w.c.m.ScopeOf(n.Body); ok && n.Body != nil {
			hs = bs
		}
		hw := &nameWalker{c: w.c, scope: hs, collect: w.collect}
		if n.Init != nil {
			ast.Walk(hw, n.Init)
		}
		if n.Cond != nil {
			ast.Walk(hw, n.Cond)
		}
		if n.Step != nil {
			ast.Walk(hw, n.Step)
		}
		if n.Body != nil {
			ast.Walk(w, n.Body)
		}
		return nil
	case *ast.GateInstantiation:
		if w.collect {
			for i := range n.Instances {
				for _, t := range n.Instances[i].Terminals {
					w.c.note(w.scope, t)
				}
			}
		}
		return nil
	case *ast.ConstraintBlock, *ast.UDP,
		*ast.Defparam, *ast.Bind, *ast.Let, *ast.Alias, *ast.Export:
		return nil
	case *ast.ContinuousAssign:
		if w.collect {
			for i := range n.Assigns {
				w.c.note(w.scope, n.Assigns[i].LHS)
			}
			return nil
		}
		for i := range n.Assigns {
			a := &n.Assigns[i]
			if !implicitNetTarget(a.LHS) {
				w.c.bind(w.scope, a.LHS)
			}
			w.c.bind(w.scope, a.RHS)
		}
		for _, d := range n.Delay {
			w.c.bind(w.scope, d)
		}
		return nil
	case *ast.PortConnection:
		if w.collect {
			w.c.note(w.scope, n.ConnExpr)
			return nil
		}
		if n.ConnExpr != nil {
			if _, bare := n.ConnExpr.(*ast.Ident); !bare {
				w.c.bind(w.scope, n.ConnExpr)
			}
		}
		return nil
	case *ast.Class:
		// A class whose base cannot be found (a type parameter, a class in a
		// library that is not part of the compilation) inherits members this
		// check cannot see: say nothing about names inside it.
		if cs, ok := w.c.m.ScopeOf(n); ok && !w.collect && w.c.m.hasOpenBase(cs) {
			w.c.suppress++
			for _, d := range n.Body {
				ast.Walk(&nameWalker{c: w.c, scope: cs}, d)
			}
			w.c.suppress--
			return nil
		}
	case *ast.Clocking:
		if !w.collect {
			w.c.clocking(w, n)
		}
		return nil
	case *ast.Covergroup:
		if !w.collect {
			w.c.covergroup(w, n)
		}
		return nil
	case *ast.PropertyDecl:
		if !w.collect {
			w.c.assertionDecl(w, n.Ports, n.Decls, n.Body)
		}
		return nil
	case *ast.SequenceDecl:
		if !w.collect {
			w.c.assertionDecl(w, n.Ports, n.Decls, n.Body)
		}
		return nil
	case *ast.ValueRange, *ast.Streaming, *ast.WithRange:
		return w // the binder does not look inside these; their operands are ordinary expressions
	case *ast.SeqDelay, *ast.SeqRepeat, *ast.PropPrefix, *ast.PropIf, *ast.PropCase,
		*ast.ClockedProp, *ast.DisableIff, *ast.SeqMatch:
		// Assertion operators are not typed by the binder: look at their
		// operands, which are ordinary expressions.
		return w
	case *ast.Constraint:
		if !w.collect {
			w.c.constraint(w, n)
		}
		return nil
	case *ast.ConstraintForeach:
		if !w.collect {
			w.c.constraintForeach(w, n)
		}
		return nil
	case *ast.RandSequence:
		if !w.collect {
			w.c.randSequence(w, n)
		}
		return nil
	case *ast.Parameter:
		if !w.collect && !n.IsSpecparam {
			w.c.noSpecparams(w.scope, n)
		}
		return w
	case *ast.Typedef:
		if !w.collect {
			w.c.typedef(w.scope, n)
		}
		return w
	case *ast.Variable:
		if !w.collect {
			w.c.typeName(w.scope, &n.Type)
		}
		return w
	case *ast.EventTerm:
		if !w.collect {
			w.c.edgeTerm(w.scope, n)
		}
		return nil
	case ast.Expr:
		if !w.collect {
			w.c.bind(w.scope, n)
		}
		return nil
	}
	if s, ok := w.c.m.ScopeOf(n); ok {
		return &nameWalker{c: w.c, scope: s, collect: w.collect}
	}
	return w
}

// implicitNetTarget reports whether the target of a continuous assignment may
// name an undeclared net: a bare identifier, or a concatenation of them.
func implicitNetTarget(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident:
		return true
	case *ast.Paren:
		return implicitNetTarget(e.X)
	case *ast.Concat:
		for _, el := range e.Elems {
			if !implicitNetTarget(el) {
				return false
			}
		}
		return len(e.Elems) > 0
	}
	return false
}

// randSequence checks a randsequence: every production a rule uses must be
// declared by the sequence, and the code blocks and expressions are resolved
// with the productions, the production's arguments and the block's own
// variables in scope (LRM 18.17).
func (c *nameChecker) randSequence(w *nameWalker, n *ast.RandSequence) {
	prods := map[string]bool{}
	for i := range n.Productions {
		prods[n.Productions[i].Name] = true
	}
	ast.Inspect(n, func(x ast.Node) bool {
		if it, ok := x.(*ast.RandItem); ok && !prods[it.Name] {
			c.set.Add(diag.Diagnostic{Code: diag.CodeUndeclared, Severity: diag.Error, Range: it.Pos(),
				Message: "production " + it.Name + " is not declared in this randsequence (LRM 18.17)"})
		}
		return true
	})
	if n.Start != "" && !prods[n.Start] {
		c.set.Add(diag.Diagnostic{Code: diag.CodeUndeclared, Severity: diag.Error, Range: n.Pos(),
			Message: "production " + n.Start + " is not declared in this randsequence (LRM 18.17)"})
	}
	saved := c.ignore
	defer func() { c.ignore = saved }()
	for i := range n.Productions {
		prod := &n.Productions[i]
		c.ignore = map[string]bool{}
		for k := range saved {
			c.ignore[k] = true
		}
		for k := range prods {
			c.ignore[k] = true
		}
		for _, a := range prod.Args {
			c.ignore[a.Name] = true
		}
		ast.Inspect(prod, func(x ast.Node) bool {
			if code, ok := x.(*ast.RandCode); ok {
				for _, d := range code.Decls {
					if v, ok := d.(*ast.Variable); ok {
						c.ignore[v.Name] = true
					}
				}
			}
			return true
		})
		for r := range prod.Rules {
			ast.Walk(w, &prod.Rules[r])
		}
	}
}

// hasOpenBase reports whether the class that cs opens, or one of its base
// classes, extends something that does not resolve to a class declaration.
func (m *Model) hasOpenBase(cs *Scope) bool {
	for k, n := cs, 0; k != nil && n < 64; n++ {
		cl, ok := k.Node.(*ast.Class)
		if !ok {
			return false
		}
		if cl.Extends == "" {
			return false
		}
		sup := m.superScope(k)
		if sup == nil {
			return true
		}
		k = sup
	}
	return true
}

// constraint resolves the names of a class constraint block: against the
// class that declares it, which for "constraint C::name { ... }" written
// outside the class is C (LRM 18.5, 18.5.1).
func (c *nameChecker) constraint(w *nameWalker, n *ast.Constraint) {
	scope := w.scope
	if n.Scope != "" {
		csym := c.m.classOf(scope.Lookup(n.Scope).Symbol, 0)
		if csym == nil || csym.Child == nil {
			return
		}
		scope = csym.Child
	}
	if scope == nil {
		return
	}
	if c.m.hasOpenBase(scope) {
		c.suppress++
		defer func() { c.suppress-- }()
	}
	cw := &nameWalker{c: c, scope: scope}
	for _, it := range n.Items {
		ast.Walk(cw, it)
	}
}

// constraintForeach resolves "foreach (arr[i, j]) body": the array in the
// enclosing scope, the body with the loop variables in scope (LRM 18.5.8).
func (c *nameChecker) constraintForeach(w *nameWalker, n *ast.ConstraintForeach) {
	if n.Array != nil {
		ast.Walk(w, n.Array)
	}
	saved := c.ignore
	c.ignore = map[string]bool{}
	for k := range saved {
		c.ignore[k] = true
	}
	for _, v := range n.Vars {
		if v != "" {
			c.ignore[v] = true
		}
	}
	if n.Body != nil {
		ast.Walk(w, n.Body)
	}
	c.ignore = saved
}

// assertionDecl resolves the names of a property or sequence body. Its formal
// arguments and local variables are in scope inside it (LRM 16.8, 16.12).
func (c *nameChecker) assertionDecl(w *nameWalker, ports []ast.Arg, decls []ast.Decl, body ast.Expr) {
	saved := c.ignore
	c.ignore = map[string]bool{}
	for k := range saved {
		c.ignore[k] = true
	}
	for _, p := range ports {
		c.ignore[p.Name] = true
	}
	for _, d := range decls {
		if v, ok := d.(*ast.Variable); ok {
			c.ignore[v.Name] = true
		}
	}
	if body != nil {
		ast.Walk(w, body)
	}
	c.ignore = saved
}

// covergroup resolves the names of a covergroup. Its formal arguments, the
// labels of its coverpoints and crosses, its bins and the "item" of a with
// clause are in scope inside it, and binsof/intersect are part of the language
// (LRM 19).
func (c *nameChecker) covergroup(w *nameWalker, n *ast.Covergroup) {
	saved := c.ignore
	c.ignore = map[string]bool{"item": true, "binsof": true, "intersect": true}
	for k := range saved {
		c.ignore[k] = true
	}
	for _, p := range n.Ports {
		c.ignore[p.Name] = true
	}
	for _, p := range n.SamplePorts {
		c.ignore[p.Name] = true
	}
	ast.Inspect(n, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.CoverPoint:
			if x.Name != "" {
				c.ignore[x.Name] = true
			}
		case *ast.CoverCross:
			if x.Name != "" {
				c.ignore[x.Name] = true
			}
		case *ast.CoverBin:
			if x.Name != "" {
				c.ignore[x.Name] = true
			}
		}
		return true
	})
	if n.Event != nil {
		ast.Walk(w, n.Event)
	}
	for _, it := range n.Items {
		ast.Walk(w, it)
	}
	c.ignore = saved
}

// clocking resolves the names of a clocking block: its event, and each signal
// it lists, which is either given an expression or is a signal of the
// enclosing scope that the block samples or drives (LRM 14.3).
func (c *nameChecker) clocking(w *nameWalker, n *ast.Clocking) {
	if n.Event != nil {
		ast.Walk(w, n.Event)
	}
	for i := range n.Items {
		for _, s := range n.Items[i].Signals {
			switch {
			case s.Expr != nil:
				c.bind(w.scope, s.Expr)
			case w.scope != nil && w.scope.Lookup(s.Name).Symbol == nil:
				c.set.Add(diag.Diagnostic{Code: diag.CodeUndeclared, Severity: diag.Error, Range: s.Pos(),
					Message: "\"" + s.Name + "\" is not declared"})
			}
		}
	}
}
