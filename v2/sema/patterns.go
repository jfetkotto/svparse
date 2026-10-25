package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// checkPatternInit reports an assignment pattern that cannot initialise the
// variable it is given to: too many or too few values for an array or
// structure, a named key that is not a member (LRM 10.9). Only patterns are
// checked here, since the binder's verdict on other initialisers is not yet
// reliable enough to report.
func (c *checker) checkPatternInit(s *Scope, v *ast.Variable) {
	if s == nil || v.InitialExpr == nil {
		return
	}
	if _, ok := unparen(v.InitialExpr).(*ast.AssignPattern); !ok {
		return
	}
	sym, ok := s.Own(v.Name)
	if !ok {
		return
	}
	t, _ := c.m.TypeOf(sym)
	if t == nil || IsInvalid(t) {
		return
	}
	_, ds := c.m.BindAssign(s, t, v.InitialExpr)
	for _, d := range ds {
		if d.Code == diag.CodeIncompatibleTypes {
			c.set.Add(d)
		}
	}
}
