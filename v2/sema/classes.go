package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// builtinMethods are the methods every class has and none may redefine
// (LRM 18.6.3, 18.8, 18.9).
var builtinMethods = map[string]bool{"randomize": true, "rand_mode": true, "constraint_mode": true}

// externalDefinitions returns the out-of-block definitions of the compilation,
// "Class::name" for each "function C::f", "task C::t" and "constraint C::c"
// that has a body (LRM 8.24, 18.5.1).
func (c *checker) externalDefinitions() map[string]bool {
	if c.external != nil {
		return c.external
	}
	c.external = map[string]bool{}
	for _, f := range c.m.comp.files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.Function:
				if d.Scope != "" && !d.Prototype {
					c.external[d.Scope+"::"+d.Name] = true
				}
			case *ast.Task:
				if d.Scope != "" && !d.Prototype {
					c.external[d.Scope+"::"+d.Name] = true
				}
			case *ast.Constraint:
				if d.Scope != "" && !d.Prototype {
					c.external[d.Scope+"::"+d.Name] = true
				}
			}
			return true
		})
	}
	return c.external
}

// classRules checks one class declaration (scope cs) against the rules of LRM
// chapters 8 and 18 that need no expression typing: abstract members belong to
// virtual classes only, a concrete class implements every abstract member it
// inherits, an extern constraint has a definition, and the built-in
// randomization methods stay as they are.
func (c *checker) classRules(cs *Scope, cl *ast.Class) {
	abstract := cl.Virtual || cl.IsInterfaceClass
	for _, d := range cl.Body {
		switch n := d.(type) {
		case *ast.Function:
			c.memberRules(cl, abstract, n.Name, n.Scope, n.Pure, n.Pos())
		case *ast.Task:
			c.memberRules(cl, abstract, n.Name, n.Scope, n.Pure, n.Pos())
		case *ast.Constraint:
			if n.Pure && !abstract {
				c.errorf(diag.CodeClassRule, n.Pos(), "pure constraint %q is in class %q, which is not virtual (LRM 18.5.2)", n.Name, cl.Name)
			}
			if n.Extern && n.Scope == "" && !c.externalDefinitions()[cl.Name+"::"+n.Name] {
				c.errorf(diag.CodeClassRule, n.Pos(), "constraint %q of class %q is declared extern but never defined (LRM 18.5.1)", n.Name, cl.Name)
			}
		}
	}
	if !abstract && cs != nil {
		c.unimplemented(cs, cl)
	}
	c.randcRules(cl)
}

// randcRules reports the uses of a randc variable that the constraint
// language does not allow: ordering it in "solve ... before", giving it a soft
// constraint or a distribution (LRM 18.5.4, 18.5.10, 18.5.14).
func (c *checker) randcRules(cl *ast.Class) {
	randc := map[string]bool{}
	for _, d := range cl.Body {
		if v, ok := d.(*ast.Variable); ok && v.IsRandC {
			randc[v.Name] = true
		}
	}
	if len(randc) == 0 {
		return
	}
	uses := func(e ast.Expr) string {
		found := ""
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && randc[id.Name] && found == "" {
				found = id.Name
			}
			return found == ""
		})
		return found
	}
	for _, d := range cl.Body {
		con, ok := d.(*ast.Constraint)
		if !ok {
			continue
		}
		for _, it := range con.Items {
			ast.Inspect(it, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.ConstraintExpr:
					if n.Soft {
						if v := uses(n.X); v != "" {
							c.errorf(diag.CodeClassRule, n.Pos(), "soft constraint on randc variable %q (LRM 18.5.14)", v)
						}
					}
				case *ast.Dist:
					if v := uses(n.X); v != "" {
						c.errorf(diag.CodeClassRule, n.Pos(), "distribution on randc variable %q (LRM 18.5.4)", v)
					}
				case *ast.ConstraintSolve:
					for _, list := range [][]ast.Expr{n.Solve, n.Before} {
						for _, e := range list {
							if v := uses(e); v != "" {
								c.errorf(diag.CodeClassRule, e.Pos(), "randc variable %q cannot be ordered with solve ... before (LRM 18.5.10)", v)
							}
						}
					}
				}
				return true
			})
		}
	}
}

func (c *checker) memberRules(cl *ast.Class, abstract bool, name, scope string, pure bool, at ast.Position) {
	if scope != "" {
		return
	}
	if builtinMethods[name] {
		c.errorf(diag.CodeClassRule, at, "%q is a built-in method of every class and cannot be redefined (LRM 18.6.3)", name)
	}
	if pure && !abstract {
		c.errorf(diag.CodeClassRule, at, "pure virtual method %q is in class %q, which is not virtual (LRM 8.21)", name, cl.Name)
	}
}

// unimplemented reports a concrete class that inherits an abstract method or
// constraint it does not define, directly or in a class between it and the one
// declaring it (LRM 8.21, 18.5.2).
func (c *checker) unimplemented(cs *Scope, cl *ast.Class) {
	defined := map[string]bool{}
	define := func(k *ast.Class) {
		for _, d := range k.Body {
			switch n := d.(type) {
			case *ast.Function:
				if !n.Pure && !n.Prototype || n.Prototype && !n.Pure && c.externalDefinitions()[k.Name+"::"+n.Name] {
					defined[n.Name] = true
				}
			case *ast.Task:
				if !n.Pure && !n.Prototype || n.Prototype && !n.Pure && c.externalDefinitions()[k.Name+"::"+n.Name] {
					defined[n.Name] = true
				}
			case *ast.Constraint:
				if !n.Pure && (!n.Prototype || c.externalDefinitions()[k.Name+"::"+n.Name]) {
					defined["constraint "+n.Name] = true
				}
			}
		}
	}
	define(cl)
	sc := cs
	for i := 0; i < 64; i++ {
		sc = c.m.superScope(sc)
		if sc == nil {
			return
		}
		base, ok := sc.Node.(*ast.Class)
		if !ok {
			return
		}
		for _, d := range base.Body {
			var key, what string
			switch n := d.(type) {
			case *ast.Function:
				if n.Pure {
					key, what = n.Name, "method"
				}
			case *ast.Task:
				if n.Pure {
					key, what = n.Name, "method"
				}
			case *ast.Constraint:
				if n.Pure {
					key, what = "constraint "+n.Name, "constraint"
				}
			}
			if key != "" && !defined[key] {
				name := key
				if what == "constraint" {
					name = key[len("constraint "):]
				}
				c.errorf(diag.CodeClassRule, cl.Pos(), "class %q is not virtual but does not implement the pure %s %q of %q (LRM 8.21)", cl.Name, what, name, base.Name)
				defined[key] = true // once per member
			}
		}
		define(base)
	}
}

// checkNew reports "new" on a handle whose class cannot be constructed: a
// virtual (abstract) class or an interface class (LRM 8.21, 8.26.5).
func (c *checker) checkNew(s *Scope, t Type, e ast.Expr) {
	n, ok := unparen(e).(*ast.New)
	if !ok || n.Scope != nil || n.Copy != nil || s == nil {
		return
	}
	cls, ok := Resolve(t).(*Class)
	if !ok {
		return
	}
	sym := s.Lookup(cls.Name).Symbol
	if sym == nil {
		return
	}
	if decl, ok := sym.Decl.(*ast.Class); ok {
		switch {
		case decl.IsInterfaceClass:
			c.errorf(diag.CodeClassRule, n.Pos(), "interface class %q cannot be constructed (LRM 8.26.5)", cls.Name)
		case decl.Virtual:
			c.errorf(diag.CodeClassRule, n.Pos(), "virtual class %q is abstract and cannot be constructed (LRM 8.21)", cls.Name)
		}
	}
}
