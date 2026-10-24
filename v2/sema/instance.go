package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// Instance binds and evaluates expressions for one instance of a design
// element: names that depend on a parameter (a vector's width, an array's
// bounds) take the instance's values from its Env, and the types resolved on
// the way are kept in a cache private to the instance (LRM 23.10: each
// instance has its own parameter values, so its types may differ from another
// instance's of the same module).
//
// The cache is the point of the type. Binding one assignment resolves the
// declared type of every signal it names; with one Instance, the next
// assignment finds those types already resolved instead of resolving them
// again, which made a pass over a design with many instances do the same work
// many times over. An Instance is for sequential use by one caller; it is not
// safe for concurrent use.
type Instance struct {
	m   *Model
	env *Env
	tc  *typeCache
}

// NewInstance returns an Instance for env, which may be nil for a module's
// default parameter values.
func (m *Model) NewInstance(env *Env) *Instance {
	return &Instance{m: m, env: env, tc: newTypeCache()}
}

func (in *Instance) binder(s *Scope) *binder {
	b := in.m.newBinder(s)
	b.env, b.tc = in.env, in.tc
	return b
}

// Bind is Model.Bind for the instance.
func (in *Instance) Bind(s *Scope, e ast.Expr) (BoundExpr, []diag.Diagnostic) {
	b := in.binder(s)
	r := b.bindSelf(e)
	if r == nil {
		r = &BBad{exprInfo{Invalid, e}}
	}
	return r, b.diags()
}

// TypeOf is Model.TypeOf for the instance.
func (in *Instance) TypeOf(sym *Symbol) (Type, []diag.Diagnostic) {
	b := in.binder(sym.Scope)
	r := b.typeOfSymbol(sym)
	return r, b.diags()
}

// Eval is Model.Eval for the instance.
func (in *Instance) Eval(s *Scope, e ast.Expr) (Const, []diag.Diagnostic) {
	b := in.binder(s)
	be := b.bindSelf(e)
	if be == nil || IsInvalid(be.Type()) {
		return Const{}, b.diags()
	}
	c, _ := b.eval(be)
	return c, b.diags()
}

// EvalBound evaluates an already bound expression with the instance's values.
func (in *Instance) EvalBound(be BoundExpr) (Const, []diag.Diagnostic) {
	b := in.binder(in.m.rootScope())
	c, _ := b.eval(be)
	return c, b.diags()
}
