package lint

import (
	"math/big"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/sema"
	"github.com/jfetkotto/svparse/v2/svint"
)

func init() {
	Register(Rule{
		Name:     "width-truncation",
		Doc:      "an assignment drops the upper bits of a wider value (LRM 10.7)",
		Severity: diag.Warning,
		Check:    func(c *Context) { checkWidths(c, true) },
	})
	Register(Rule{
		Name:     "width-extension",
		Doc:      "an assignment pads a narrower signal to a wider target",
		Severity: diag.Info,
		Check:    func(c *Context) { checkWidths(c, false) },
	})
}

// checkWidths compares the width of each assignment's right side with its
// target, for every instance and generate block of the elaborated design (so
// widths that depend on parameters are the instance's own). A finding is
// reported once per source position.
func checkWidths(c *Context, truncation bool) {
	if c.Design == nil {
		return
	}
	seen := map[ast.Position]bool{}
	c.Design.Walk(func(n *elab.Node) bool {
		if c.Cancelled() {
			return false
		}
		if n.Scope == nil || n.Env == nil {
			return true
		}
		var body []ast.Decl
		switch d := n.Decl.(type) {
		case *ast.GenerateBlock:
			body = d.Body
		default:
			if n.Def != nil {
				if cont, ok := n.Def.Decl.(*ast.Container); ok {
					body = cont.Body
				}
			}
		}
		w := &widthWalker{c: c, env: n.Env, scope: n.Scope, truncation: truncation, seen: seen}
		for _, d := range body {
			ast.Walk(w, d)
		}
		return true
	})
}

type widthWalker struct {
	c          *Context
	env        *sema.Env
	scope      *sema.Scope
	truncation bool
	seen       map[ast.Position]bool
}

func (w *widthWalker) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	switch n := n.(type) {
	case *ast.GenerateIf, *ast.GenerateFor, *ast.GenerateCase, *ast.GenerateBlock:
		return nil // elaborated as nodes of their own
	case *ast.Assign:
		if n.Op == "=" || n.Op == "<=" {
			w.assignment(n.LHS, n.RHS, n.Pos())
		}
	case *ast.ContinuousAssign:
		for i := range n.Assigns {
			w.assignment(n.Assigns[i].LHS, n.Assigns[i].RHS, n.Assigns[i].Pos())
		}
	case *ast.Variable:
		if n.InitialExpr != nil && n.NetQualifier == "" {
			id := &ast.Ident{Name: n.Name}
			id.SetPos(n.Pos())
			w.assignment(id, n.InitialExpr, n.Pos())
		}
	}
	if s, ok := w.c.Model.ScopeOf(n); ok {
		n := *w
		n.scope = s
		return &n
	}
	return w
}

func (w *widthWalker) assignment(lhs, rhs ast.Expr, at ast.Position) {
	if w.seen[at] {
		return
	}
	lb, _ := w.c.Model.BindIn(w.scope, lhs, w.env)
	rb, _ := w.c.Model.BindIn(w.scope, rhs, w.env)
	if lb == nil || rb == nil || !sema.IsIntegral(lb.Type()) || !sema.IsIntegral(rb.Type()) {
		return
	}
	lw, ok := sema.Bits(lb.Type())
	if !ok {
		return
	}
	rw, ok := w.effectiveWidth(rb, true)
	if !ok {
		return
	}
	switch {
	case w.truncation && rw > lw:
		w.seen[at] = true
		w.c.Report(at, "%d-bit value assigned to a %d-bit target: the upper %d bit(s) are lost", rw, lw, rw-lw)
	case !w.truncation && rw < lw && !isLiteralLike(rb) && !grows(rb):
		w.seen[at] = true
		w.c.Report(at, "%d-bit value assigned to a %d-bit target: padded with %d bit(s)", rw, lw, lw-rw)
	}
}

// grows reports whether the operator widens its result on purpose: the carry of
// a sum, the product of two numbers, a left shift. Assigning such a result to a
// wider target ("{cout, sum} = a + b") is the point.
func grows(be sema.BoundExpr) bool {
	if b, ok := be.(*sema.BBinary); ok {
		switch b.Op {
		case "+", "-", "*", "<<", "<<<", "**":
			return true
		}
	}
	return false
}

// isLiteralLike reports whether the right side is built from literals and
// parameters only; padding those is how constants are written.
func isLiteralLike(be sema.BoundExpr) bool {
	constant := true
	sema.WalkBound(be, func(n sema.BoundExpr) bool {
		if name, ok := n.(*sema.BName); ok && isStateKind(name.Sym.Kind) {
			constant = false
		}
		return constant
	})
	return constant
}

func isStateKind(k sema.SymKind) bool {
	switch k {
	case sema.SymVariable, sema.SymNet, sema.SymPort, sema.SymArg, sema.SymLoopVar, sema.SymClockingSignal:
		return true
	}
	return false
}

// effectiveWidth is the number of bits a right side needs. fit lets a literal
// count only as many bits as its value takes: "count + 1" needs the width of
// count, however wide the literal 1 is declared. An expression of constants
// only, "W - 1" or "$clog2(N)", counts as its value. Operands of a
// concatenation, a select or a cast keep their declared width, since there the
// bits are really present.
func (w *widthWalker) effectiveWidth(be sema.BoundExpr, fit bool) (int, bool) {
	if fit && isLiteralLike(be) {
		if c, ds := w.c.Model.EvalBound(be, w.env); len(ds) == 0 && c.Kind == sema.ConstInt && c.Int.IsKnown() {
			return bitLen(c.Int), true
		}
	}
	declared := func() (int, bool) { return sema.Bits(be.Type()) }
	switch n := be.(type) {
	case *sema.BLiteral:
		if n.Kind == ast.LitUnbasedUnsized {
			return 1, true
		}
		return declared()
	case *sema.BBinary:
		switch n.Op {
		case "+", "-", "*", "/", "%", "&", "|", "^", "~^", "^~":
			x, ok1 := w.effectiveWidth(n.X, fit)
			y, ok2 := w.effectiveWidth(n.Y, fit)
			if n.Op == "&" && (isLiteralLike(n.X) || isLiteralLike(n.Y)) {
				return min(x, y), ok1 && ok2 // a constant mask bounds the result
			}
			return max(x, y), ok1 && ok2
		case ">>", ">>>":
			x, ok := w.effectiveWidth(n.X, fit)
			if k, isConst := w.constInt(n.Y); isConst {
				x = max(x-int(min(k, int64(x))), 1) // a right shift by a constant drops bits
			}
			return x, ok
		case "<<", "<<<", "**":
			return w.effectiveWidth(n.X, fit)
		}
		return declared()
	case *sema.BUnary:
		switch n.Op {
		case "+", "-", "~":
			return w.effectiveWidth(n.X, fit)
		}
		return declared()
	case *sema.BCond:
		x, ok1 := w.effectiveWidth(n.Then, fit)
		y, ok2 := w.effectiveWidth(n.Else, fit)
		return max(x, y), ok1 && ok2
	}
	return declared()
}

// constInt evaluates a constant expression to an integer.
func (w *widthWalker) constInt(be sema.BoundExpr) (int64, bool) {
	if !isLiteralLike(be) {
		return 0, false
	}
	c, ds := w.c.Model.EvalBound(be, w.env)
	if len(ds) != 0 {
		return 0, false
	}
	return c.Int64()
}

// bitLen is the number of bits a known value needs, at least one.
func bitLen(v svint.Value) int {
	n, ok := v.Big()
	if !ok {
		return 1
	}
	if n.Sign() < 0 {
		n = new(big.Int).Abs(n)
	}
	return max(n.BitLen(), 1)
}
