package elab

import (
	"strconv"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
	"github.com/jfetkotto/svparse/v2/svint"
)

// items elaborates the module items of a body that create hierarchy:
// instantiations and generate constructs.
func (e *elaborator) items(n *Node, sc *sema.Scope, decls []ast.Decl) {
	for _, d := range decls {
		if e.cancelled() {
			return
		}
		switch d := d.(type) {
		case *ast.Instantiation:
			e.instantiate(n, sc, d)
		case *ast.GenerateRegion:
			e.items(n, sc, d.Body)
		case *ast.GenerateBlock:
			e.block(n, sc, d, nil, "")
		case *ast.GenerateIf:
			e.generateIf(n, sc, d)
		case *ast.GenerateCase:
			e.generateCase(n, sc, d)
		case *ast.GenerateFor:
			e.generateFor(n, sc, d)
		}
	}
}

// evalIn evaluates a constant expression in the node's environment.
func (e *elaborator) evalIn(n *Node, sc *sema.Scope, x ast.Expr, env *sema.Env) (sema.Const, bool) {
	c, ds := e.model.Eval(sc, x, env)
	e.addAll(ds)
	return c, len(ds) == 0 && !isEmpty(c)
}

func isEmpty(c sema.Const) bool { return c.Kind == sema.ConstInt && !c.Int.Valid() }

func (e *elaborator) generateIf(n *Node, sc *sema.Scope, g *ast.GenerateIf) {
	c, ok := e.evalIn(n, sc, g.Cond, n.Env)
	if !ok {
		return
	}
	// A condition with an unknown bit counts as false (LRM 27.5).
	if truthy(c) {
		e.block(n, sc, g.Then, nil, "")
		return
	}
	switch els := g.Else.(type) {
	case nil:
	case *ast.GenerateIf:
		e.generateIf(n, sc, els)
	case *ast.GenerateBlock:
		e.block(n, sc, els, nil, "")
	}
}

func truthy(c sema.Const) bool {
	switch c.Kind {
	case sema.ConstReal:
		return c.Real != 0
	case sema.ConstString:
		return c.Str != ""
	}
	return svint.Truth(c.Int) == svint.Bit1
}

func (e *elaborator) generateCase(n *Node, sc *sema.Scope, g *ast.GenerateCase) {
	subj, ok := e.evalIn(n, sc, g.Expr, n.Env)
	if !ok || subj.Kind != sema.ConstInt {
		return
	}
	var deflt *ast.GenerateBlock
	for i := range g.Items {
		item := &g.Items[i]
		if item.Default {
			deflt = item.Body
			continue
		}
		for _, x := range item.Exprs {
			v, ok := e.evalIn(n, sc, x, n.Env)
			if !ok || v.Kind != sema.ConstInt {
				return
			}
			a, b := subj.Int.AsUnsigned(), v.Int.AsUnsigned()
			w := max(a.Width(), b.Width())
			if svint.CaseEq(a.Resize(w), b.Resize(w)).Bit(0) == svint.Bit1 {
				e.block(n, sc, item.Body, nil, "")
				return
			}
		}
	}
	if deflt != nil {
		e.block(n, sc, deflt, nil, "")
	}
}

// loopVar is the genvar of a loop iteration and its value.
type loopVar struct {
	sym *sema.Symbol
	val sema.Const
}

func (e *elaborator) generateFor(n *Node, sc *sema.Scope, g *ast.GenerateFor) {
	bodyScope, ok := e.model.ScopeOf(g.Body)
	if !ok {
		return
	}
	var gv *sema.Symbol
	if g.Genvar {
		gv, _ = bodyScope.Own(g.Var)
	} else {
		gv = sc.Lookup(g.Var).Symbol
	}
	if gv == nil || gv.Kind != sema.SymGenvar {
		e.errorf(diag.CodeUndeclared, g.Pos(), "%q is not a genvar", g.Var)
		return
	}
	env := n.Env
	c, ok := e.evalIn(n, bodyScope, g.Init, env)
	if !ok || c.Kind != sema.ConstInt {
		return
	}
	cur := c
	for iter := 0; ; iter++ {
		if e.cancelled() {
			return
		}
		if iter >= e.opts.MaxGenerateIters {
			e.errorf(diag.CodeGenerateLimit, g.Pos(), "generate loop over %q ran more than %d iterations", g.Var, e.opts.MaxGenerateIters)
			return
		}
		iterEnv := e.newEnv(env.Params, env.Vars)
		if iterEnv.Vars == nil {
			iterEnv.Vars = map[*sema.Symbol]sema.Const{}
		}
		iterEnv.Vars[gv] = cur
		cond, ok := e.evalIn(n, bodyScope, g.Cond, iterEnv)
		if !ok || !truthy(cond) {
			return
		}
		idx, ok := cur.Int64()
		if !ok {
			e.errorf(diag.CodeNotConstant, g.Pos(), "genvar %q is not a known integer", g.Var)
			return
		}
		e.block(n, sc, g.Body, &loopVar{gv, cur}, "["+strconv.FormatInt(idx, 10)+"]")
		next, ok := e.step(n, bodyScope, g, gv, iterEnv)
		if !ok {
			return
		}
		cur = next
	}
}

// step computes the genvar's next value from the loop's iteration statement.
func (e *elaborator) step(n *Node, sc *sema.Scope, g *ast.GenerateFor, gv *sema.Symbol, env *sema.Env) (sema.Const, bool) {
	var expr ast.Expr
	ident := &ast.Ident{Name: g.Var}
	ident.SetPos(g.Pos())
	lit := func(text string) ast.Expr {
		l := &ast.Literal{Kind: ast.LitInt, Text: text}
		l.SetPos(g.Pos())
		return l
	}
	bin := func(op string, x, y ast.Expr) ast.Expr {
		b := &ast.Binary{Op: op, X: x, Y: y}
		b.SetPos(g.Pos())
		return b
	}
	switch s := g.Step.(type) {
	case *ast.Assign:
		if s.Op == "=" {
			expr = s.RHS
		} else {
			expr = bin(s.Op[:len(s.Op)-1], ident, s.RHS)
		}
	case *ast.ExprStmt:
		if id, ok := s.X.(*ast.IncDec); ok {
			op := "+"
			if id.Op == "--" {
				op = "-"
			}
			expr = bin(op, ident, lit("1"))
		}
	}
	if expr == nil {
		e.errorf(diag.CodeNotConstant, g.Pos(), "the iteration of a generate loop must assign its genvar")
		return sema.Const{}, false
	}
	c, ok := e.evalIn(n, sc, expr, env)
	if !ok || c.Kind != sema.ConstInt {
		return sema.Const{}, false
	}
	return c, true
}

// block elaborates one generate block as a child of n: a selected branch, a
// loop iteration (loop and suffix set) or a bare begin/end.
func (e *elaborator) block(n *Node, sc *sema.Scope, blk *ast.GenerateBlock, loop *loopVar, suffix string) {
	if e.cancelled() {
		return
	}
	if !e.reserve(blk.Pos()) {
		return
	}
	bs, ok := e.model.ScopeOf(blk)
	if !ok {
		return
	}
	node := &Node{Kind: KindBlock, Name: bs.Name + suffix, Parent: n, Scope: bs, Decl: blk}
	n.Children = append(n.Children, node)
	if loop != nil {
		idx, _ := loop.val.Int64()
		node.Index, node.HasIndex = int(idx), true
	}
	depth := 0
	for p := n; p != nil; p = p.Parent {
		depth++
	}
	if depth >= e.opts.MaxDepth {
		e.errorf(diag.CodeInstantiationDepth, blk.Pos(), "the hierarchy is nested more than %d deep at %s", e.opts.MaxDepth, node.Path())
		return
	}
	node.Env = e.newEnv(n.Env.Params, n.Env.Vars)
	if loop != nil {
		if node.Env.Vars == nil {
			node.Env.Vars = map[*sema.Symbol]sema.Const{}
		}
		node.Env.Vars[loop.sym] = loop.val
	}
	e.evalParams(node, bs, paramSymbols(bs), nil, nil)
	e.collectDefparams(node, bs, blk.Body)
	// Defparams of the enclosing node that name this block reach its contents.
	base := bs.Name
	for _, dp := range forwardedFor(n, base) {
		node.defparams = append(node.defparams, dp)
	}
	e.items(node, bs, blk.Body)
}
