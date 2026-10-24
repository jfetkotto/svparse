package sema

import (
	"regexp"
	"strconv"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// checkEnums reports the enumeration declarations in file f that break the
// rules of LRM 6.19: a sized member value must be as wide as the base type, a
// 2-state base takes no x or z value, and the member after an x or z value
// needs a value of its own.
func (c *checker) checkEnums(fileScope *Scope, f *ast.File) {
	stack := []*Scope{fileScope}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		s := stack[len(stack)-1]
		if sc, ok := c.m.ScopeOf(n); ok {
			s = sc
		}
		stack = append(stack, s)
		if e, ok := n.(*ast.Enum); ok && s != nil {
			c.checkEnum(s, e)
		}
		return true
	})
}

var sizedLiteral = regexp.MustCompile(`^(\d+)\s*'`)

func (c *checker) checkEnum(s *Scope, e *ast.Enum) {
	b := c.m.newBinder(s)
	base := typeInt
	if e.BaseType.Name != "" || len(e.BaseType.PackedDims) > 0 {
		base = b.resolveType(&e.BaseType)
	}
	width, ok := Bits(base)
	if !ok {
		return
	}
	twoState := !IsFourState(base)
	afterUnknown := false
	for i := range e.Members {
		mem := &e.Members[i]
		if mem.ValueExpr == nil {
			if afterUnknown {
				c.errorf(diag.CodeInvalidEnumValue, mem.Pos(), "enum member %q follows a member with an x or z value and needs a value of its own (LRM 6.19.2)", mem.Name)
			}
			continue
		}
		afterUnknown = false
		if lit, ok := unparen(mem.ValueExpr).(*ast.Literal); ok && lit.Kind == ast.LitInt {
			if m := sizedLiteral.FindStringSubmatch(lit.Text); m != nil {
				if size, _ := strconv.Atoi(m[1]); size != width {
					c.errorf(diag.CodeInvalidEnumValue, lit.Pos(), "the value of enum member %q is %d bits wide, the base type is %d (LRM 6.19)", mem.Name, size, width)
				}
			}
		}
		be := b.bindSelf(mem.ValueExpr)
		if be == nil || IsInvalid(be.Type()) {
			continue
		}
		if v, ok := b.eval(be); ok && v.Kind == ConstInt && !v.Int.IsKnown() {
			afterUnknown = true
			if twoState {
				c.errorf(diag.CodeInvalidEnumValue, mem.ValueExpr.Pos(), "enum member %q has an x or z value but the base type is 2-state (LRM 6.19)", mem.Name)
			}
		}
	}
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.Paren)
		if !ok {
			return e
		}
		e = p.X
	}
}

// checkEnumAssign reports a procedural assignment that puts a plain integer
// into an enum variable, or applies an arithmetic assignment operator to one:
// only a value of the same enum type, or a cast, converts (LRM 6.19.3,
// 6.19.4).
func (c *checker) checkEnumAssign(s *Scope, a *ast.Assign) {
	if s == nil || a.LHS == nil || a.RHS == nil {
		return
	}
	lhs, _ := c.m.Bind(s, a.LHS)
	if lhs == nil {
		return
	}
	en, ok := Resolve(lhs.Type()).(*Enum)
	if !ok {
		return
	}
	switch a.Op {
	case "=", "<=":
		_, ds := c.m.BindAssign(s, en, a.RHS)
		for _, d := range ds {
			if d.Code == diag.CodeCastRequired || d.Code == diag.CodeIncompatibleTypes {
				c.set.Add(d)
			}
		}
	default:
		c.errorf(diag.CodeCastRequired, a.Pos(), "%q on an enum variable needs a cast: write v = type'(v op x) (LRM 6.19.4)", a.Op)
	}
}
