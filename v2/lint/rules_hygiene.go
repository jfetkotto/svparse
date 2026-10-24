package lint

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/sema"
)

func init() {
	Register(Rule{
		Name:     "unreachable-code",
		Doc:      "statements follow a return, break, continue or disable in the same block and can never run",
		Severity: diag.Warning,
		Check:    checkUnreachable,
	})
	Register(Rule{
		Name:     "shadowed-declaration",
		Doc:      "a declaration hides one of the same name in an enclosing scope, so a use means a different thing than it looks",
		Severity: diag.Warning,
		Check:    checkShadowing,
	})
	Register(Rule{
		Name:     "sign-mixing",
		Doc:      "an operator mixes signed and unsigned operands, so the signed one is silently read as unsigned (LRM 11.8.1)",
		Severity: diag.Warning,
		Check:    checkSignMixing,
	})
}

// ---- unreachable-code ----

// alwaysJumps reports whether control never continues past the statement: it
// ends in a return, break, continue or disable on every path.
func alwaysJumps(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.Return, *ast.Break, *ast.Continue:
		return true
	case *ast.Disable:
		return !s.Fork
	case *ast.Labeled:
		return alwaysJumps(s.Body)
	case *ast.Block:
		if s.Fork {
			return false
		}
		for _, st := range s.Stmts {
			if alwaysJumps(st) {
				return true
			}
		}
	case *ast.If:
		return s.Else != nil && alwaysJumps(s.Then) && alwaysJumps(s.Else)
	}
	return false
}

func checkUnreachable(c *Context) {
	var seq func(stmts []ast.Stmt)
	seq = func(stmts []ast.Stmt) {
		for i, s := range stmts {
			if !alwaysJumps(s) {
				continue
			}
			for _, next := range stmts[i+1:] {
				if _, empty := next.(*ast.Null); empty {
					continue
				}
				c.Report(next.Pos(), "this statement can never run: control has already left the block")
				break
			}
			break
		}
	}
	for _, f := range c.Comp.Files() {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Block:
				seq(n.Stmts)
			case *ast.Function:
				seq(n.Stmts)
			case *ast.Task:
				seq(n.Stmts)
			}
			return true
		})
	}
}

// ---- shadowed-declaration ----

func checkShadowing(c *Context) {
	check := func(s *sema.Scope) {
		if s.Parent == nil || s.Kind == sema.ScopeUnit || s.Kind == sema.ScopeClass {
			return
		}
		for _, sym := range s.Symbols() {
			if !shadowable(sym.Kind) {
				continue
			}
			outer := s.Parent.Lookup(sym.Name).Symbol
			if outer != nil && outer != sym && shadowable(outer.Kind) && outer.Scope.Kind != sema.ScopeClass {
				c.ReportRelated(sym.Pos(), []diag.Related{{Range: outer.Pos(), Message: "the declaration it hides"}},
					"%s %q hides the %s declared in an enclosing scope", sym.Kind, sym.Name, outer.Kind)
			}
		}
	}
	for _, f := range c.Comp.Files() {
		ast.Inspect(f, func(n ast.Node) bool {
			if c.Cancelled() {
				return false
			}
			if s, ok := c.Model.ScopeOf(n); ok {
				check(s)
			}
			return true
		})
	}
}

func shadowable(k sema.SymKind) bool {
	switch k {
	case sema.SymVariable, sema.SymNet, sema.SymPort, sema.SymArg, sema.SymParameter, sema.SymLoopVar, sema.SymGenvar:
		return true
	}
	return false
}

// ---- sign-mixing ----

var signSensitiveOps = map[string]bool{
	"<": true, "<=": true, ">": true, ">=": true, "==": true, "!=": true,
	"+": true, "-": true, "*": true, "/": true, "%": true, "&": true, "|": true, "^": true,
}

func checkSignMixing(c *Context) {
	seen := map[ast.Position]bool{}
	for _, f := range c.Comp.Files() {
		us, _ := c.Model.UnitScope(f.Path)
		ast.Walk(&signWalker{c: c, scope: us, seen: seen}, f)
	}
}

type signWalker struct {
	c     *Context
	scope *sema.Scope
	seen  map[ast.Position]bool
}

func (w *signWalker) Visit(n ast.Node) ast.Visitor {
	if n == nil || w.c.Cancelled() {
		return nil
	}
	if b, ok := n.(*ast.Binary); ok && signSensitiveOps[b.Op] && w.scope != nil && !w.seen[b.Pos()] {
		w.check(b)
	}
	if s, ok := w.c.Model.ScopeOf(n); ok {
		return &signWalker{c: w.c, scope: s, seen: w.seen}
	}
	return w
}

func (w *signWalker) check(b *ast.Binary) {
	be, _ := w.c.Model.Bind(w.scope, b)
	bb, ok := be.(*sema.BBinary)
	if !ok {
		return
	}
	xt, yt := bb.X.Type(), bb.Y.Type()
	if !sema.IsIntegral(xt) || !sema.IsIntegral(yt) || sema.IsSigned(xt) == sema.IsSigned(yt) {
		return
	}
	// Constants are written to fit and a single bit is a flag, not a number.
	for _, side := range []sema.BoundExpr{bb.X, bb.Y} {
		if isLiteralLike(side) {
			return
		}
		if w, _ := sema.Bits(side.Type()); w == 1 {
			return
		}
	}
	w.seen[b.Pos()] = true
	signed, unsigned := xt, yt
	if sema.IsSigned(yt) {
		signed, unsigned = yt, xt
	}
	w.c.Report(b.Pos(), "operator %s mixes signed %s with unsigned %s: the signed operand is read as unsigned", b.Op, signed, unsigned)
}
