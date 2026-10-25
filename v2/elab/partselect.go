package elab

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
)

// checkPartSelects reports an indexed part-select, "v[base +: width]" or
// "v[base -: width]", whose width is not positive in the instance that
// elaborates it (LRM 11.5.1). The width is a constant expression, so it can
// depend on a parameter: "[3 +: W]" is fine where W is 4 and an error where an
// instance leaves it 0, which is why this is decided per elaborated node and
// not when the module is first read.
func (e *elaborator) checkPartSelects(d *Design) {
	analysed := map[string]bool{}
	d.Walk(func(n *Node) bool {
		if e.cancelled() {
			return false
		}
		if n.Scope == nil || n.Env == nil {
			return true
		}
		var body []ast.Decl
		switch x := n.Decl.(type) {
		case *ast.GenerateBlock:
			body = x.Body
		default:
			if n.Def != nil {
				if c, ok := n.Def.Decl.(*ast.Container); ok {
					body = c.Body
				}
			}
		}
		if key := n.SpecializationKey(); key != "" {
			if analysed[key] {
				return true
			}
			analysed[key] = true
		}
		inst := e.model.NewInstance(n.Env)
		for _, decl := range body {
			ast.Inspect(decl, func(x ast.Node) bool {
				switch x := x.(type) {
				case *ast.GenerateIf, *ast.GenerateFor, *ast.GenerateCase, *ast.GenerateBlock:
					return false // elaborated as nodes of their own
				case *ast.RangeSelect:
					if x.Kind == ast.RangeColon || x.Right == nil {
						return true
					}
					c, ds := inst.Eval(n.Scope, x.Right)
					if len(ds) == 0 && c.Kind == sema.ConstInt && c.Int.IsKnown() {
						if w, ok := c.Int64(); ok && w <= 0 {
							e.errorf(diag.CodeInvalidExpression, x.Right.Pos(), "the width of an indexed part-select must be positive, it is %d here (LRM 11.5.1)", w)
						}
					}
				}
				return true
			})
		}
		return true
	})
}
