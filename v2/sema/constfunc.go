package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/svint"
)

// maxCallDepth bounds the nesting of constant function calls (a recursive
// function is legal, LRM 13.4.3), well inside what the Go stack allows.
const maxCallDepth = 200

// constBound caches the bound form of expressions evaluated inside constant
// functions, so a loop binds its body once and not once per iteration.
type boundKey struct {
	e ast.Expr
	t Type
}

type flow uint8

const (
	flowNext flow = iota
	flowBreak
	flowContinue
	flowReturn
)

// interp runs one call of a constant function (LRM 13.4.3): a small statement
// interpreter over the function's body. It supports blocking assignments to
// the function's own variables (whole, bit-select and part-select), if, case,
// loops, return, break, continue and calls of other constant functions.
// Anything that depends on the simulation state (delays, events, nonblocking
// assignments, non-local variables, arrays) makes the call not constant.
type interp struct {
	b       *binder
	fn      *ast.Function
	sym     *Symbol
	retType Type
	ret     Const
	haveRet bool
	vars    map[*Symbol]Const
}

// callFunction evaluates a call of a declared function in a constant
// expression.
func (b *binder) callFunction(n *BCall) (Const, bool) {
	env := b.environment()
	fn, ok := n.Sym.Decl.(*ast.Function)
	if !ok || fn.Prototype || n.Sym.Child == nil {
		return b.notConst(n.src, "function %q has no body to evaluate", n.Name)
	}
	if env.depth >= maxCallDepth {
		b.errorf(diag.CodeConstantLimit, n.src.Pos(), "constant function calls nested more than %d deep", maxCallDepth)
		env.limit = true
		return Const{}, false
	}
	env.depth++
	defer func() { env.depth-- }()

	scope := n.Sym.Child
	it := &interp{b: b, fn: fn, sym: n.Sym, vars: map[*Symbol]Const{}}
	b.withScope(n.Sym.Scope, func() { it.retType = b.returnType(n.Sym) })
	if IsInvalid(it.retType) && fn.ReturnType.Name != "void" {
		return b.notConst(n.src, "the return type of %q is unknown", n.Name)
	}

	// Actual arguments are evaluated in the caller's frame, before the callee's
	// variables replace it.
	actual := make([]*Const, len(fn.Args))
	for i, a := range n.Args {
		fi := i
		if i < len(n.Formals) && n.Formals[i] >= 0 {
			fi = n.Formals[i]
		}
		if fi >= len(fn.Args) {
			return b.notConst(n.src, "too many arguments in the call of %q", n.Name)
		}
		c, ok := b.eval(a)
		if !ok {
			return Const{}, false
		}
		actual[fi] = &c
	}

	saved := env.Vars
	env.Vars = it.vars
	defer func() { env.Vars = saved }()

	for i := range fn.Args {
		formal := &fn.Args[i]
		if formal.Direction == ast.DirOutput || formal.Direction == ast.DirInout || formal.Direction == ast.DirRef {
			return b.notConst(n.src, "constant function %q has an %s argument", n.Name, formal.Direction)
		}
		fsym, ok := scope.Own(formal.Name)
		if !ok {
			return Const{}, false
		}
		ft := b.typeOfSymbol(fsym)
		if actual[i] != nil {
			it.vars[fsym] = b.conv(*actual[i], ft)
			continue
		}
		if formal.DefaultExpr == nil {
			return b.notConst(n.src, "argument %q of %q has no value", formal.Name, n.Name)
		}
		c, ok := it.evalIn(scope, formal.DefaultExpr, ft)
		if !ok {
			return Const{}, false
		}
		it.vars[fsym] = c
	}
	if !it.initDecls(scope, fn.Decls) {
		return Const{}, false
	}
	if rv, ok := defaultValue(it.retType); ok {
		it.ret = rv
	}
	for _, st := range fn.Stmts {
		fl, ok := it.exec(scope, st)
		if !ok {
			return Const{}, false
		}
		if fl == flowReturn {
			break
		}
	}
	if fn.ReturnType.Name == "void" {
		return intConst(svint.FromBool(false)), true
	}
	return b.conv(it.ret, it.retType), true
}

// defaultValue is what a variable holds before it is assigned: x for the
// four-state types, zero for the two-state ones (LRM 6.8).
func defaultValue(t Type) (Const, bool) {
	t = Resolve(t)
	switch {
	case IsReal(t):
		return realConst(0), true
	case t == String:
		return strConst(""), true
	case IsIntegral(t):
		w, signed := widthOf(t), IsSigned(t)
		if IsFourState(t) {
			return intConst(svint.X(w, signed)), true
		}
		return intConst(svint.New(w, signed)), true
	}
	return Const{}, false
}

// tick counts one executed statement against the step budget.
func (it *interp) tick(at ast.Node) bool {
	env := it.b.environment()
	if env.limit {
		return false
	}
	env.steps++
	if env.steps > env.MaxSteps {
		env.limit = true
		it.b.errorf(diag.CodeConstantLimit, at.Pos(), "constant evaluation exceeded %d steps", env.MaxSteps)
		return false
	}
	return true
}

func (it *interp) fail(at ast.Node, format string, args ...any) bool {
	it.b.errorf(diag.CodeNotConstant, at.Pos(), format, args...)
	return false
}

// bound binds an expression in scope sc, once, as a self-determined operand or
// as the right side of an assignment to target.
func (it *interp) bound(sc *Scope, e ast.Expr, target Type) BoundExpr {
	m := it.b.m
	if m.boundExprs == nil {
		m.boundExprs = map[boundKey]BoundExpr{}
	}
	key := boundKey{e, target}
	if be, ok := m.boundExprs[key]; ok {
		return be
	}
	var be BoundExpr
	it.b.withScope(sc, func() {
		if target == nil {
			be = it.b.bindSelf(e)
		} else {
			be = it.b.bindAssign(target, e)
		}
	})
	m.boundExprs[key] = be
	return be
}

// evalIn evaluates e in scope sc, converted to target if target is non-nil.
func (it *interp) evalIn(sc *Scope, e ast.Expr, target Type) (Const, bool) {
	be := it.bound(sc, e, target)
	if be == nil || IsInvalid(be.Type()) {
		return Const{}, false
	}
	c, ok := it.b.eval(be)
	if ok && target != nil {
		c = it.b.conv(c, target)
	}
	return c, ok
}

func (it *interp) initDecls(sc *Scope, decls []ast.Decl) bool {
	for _, d := range decls {
		v, ok := d.(*ast.Variable)
		if !ok {
			continue
		}
		sym, ok := sc.Own(v.Name)
		if !ok {
			continue
		}
		t := it.b.typeOfSymbol(sym)
		val, ok := defaultValue(t)
		if !ok {
			return it.fail(v, "variable %q has a type a constant function cannot hold", v.Name)
		}
		if v.InitialExpr != nil {
			if val, ok = it.evalIn(sc, v.InitialExpr, t); !ok {
				return false
			}
		}
		it.vars[sym] = val
	}
	return true
}

func (it *interp) truthy(sc *Scope, e ast.Expr) (bool, bool) {
	c, ok := it.evalIn(sc, e, nil)
	if !ok {
		return false, false
	}
	return svint.Truth(truth(c)) == svint.Bit1, true
}

// exec runs one statement. The second result is false if evaluation had to
// stop (an error was reported).
func (it *interp) exec(sc *Scope, s ast.Stmt) (flow, bool) {
	if s == nil {
		return flowNext, true
	}
	if !it.tick(s) {
		return flowNext, false
	}
	switch s := s.(type) {
	case *ast.Null:
		return flowNext, true
	case *ast.Labeled:
		return it.exec(sc, s.Body)
	case *ast.Block:
		if s.Fork {
			return flowNext, it.fail(s, "fork is not allowed in a constant function")
		}
		inner := sc
		if bs, ok := it.b.m.ScopeOf(s); ok {
			inner = bs
		}
		if !it.initDecls(inner, s.Decls) {
			return flowNext, false
		}
		for _, st := range s.Stmts {
			fl, ok := it.exec(inner, st)
			if !ok || fl != flowNext {
				return fl, ok
			}
		}
		return flowNext, true
	case *ast.Assign:
		if s.Op == "<=" || s.Control != nil {
			return flowNext, it.fail(s, "a constant function cannot use nonblocking assignments or delays")
		}
		return flowNext, it.assign(sc, s, s.LHS, s.RHS, s.Op)
	case *ast.ExprStmt:
		return flowNext, it.exprStmt(sc, s)
	case *ast.If:
		t, ok := it.truthy(sc, s.Cond)
		if !ok {
			return flowNext, false
		}
		if t {
			return it.exec(sc, s.Then)
		}
		return it.exec(sc, s.Else)
	case *ast.Case:
		return it.execCase(sc, s)
	case *ast.For:
		return it.execFor(sc, s)
	case *ast.While:
		return it.loop(sc, s.Cond, nil, s.Body, nil, false)
	case *ast.DoWhile:
		return it.loop(sc, s.Cond, nil, s.Body, nil, true)
	case *ast.Forever:
		return it.loop(sc, nil, nil, s.Body, nil, false)
	case *ast.Repeat:
		c, ok := it.evalIn(sc, s.Count, nil)
		if !ok {
			return flowNext, false
		}
		n, ok := c.Int64()
		if !ok {
			return flowNext, it.fail(s, "the repeat count is not a known integer")
		}
		for i := int64(0); i < n; i++ {
			fl, ok := it.exec(sc, s.Body)
			if !ok || fl == flowReturn {
				return fl, ok
			}
			if fl == flowBreak {
				break
			}
		}
		return flowNext, true
	case *ast.Return:
		if s.Value != nil {
			c, ok := it.evalIn(sc, s.Value, it.retType)
			if !ok {
				return flowNext, false
			}
			it.ret = c
		}
		return flowReturn, true
	case *ast.Break:
		return flowBreak, true
	case *ast.Continue:
		return flowContinue, true
	case *ast.ImmediateAssert:
		return flowNext, true
	}
	return flowNext, it.fail(s, "this statement cannot appear in a constant function")
}

// loop runs "while", "do ... while" and "forever" loops and the loop part of a
// "for". step runs after the body and after a continue.
func (it *interp) loop(sc *Scope, cond ast.Expr, _ []ast.Stmt, body ast.Stmt, step []ast.Stmt, bodyFirst bool) (flow, bool) {
	for first := true; ; first = false {
		if cond != nil && !(bodyFirst && first) {
			t, ok := it.truthy(sc, cond)
			if !ok {
				return flowNext, false
			}
			if !t {
				return flowNext, true
			}
		}
		if !it.tick(body) {
			return flowNext, false
		}
		fl, ok := it.exec(sc, body)
		if !ok || fl == flowReturn {
			return fl, ok
		}
		if fl == flowBreak {
			return flowNext, true
		}
		for _, st := range step {
			if _, ok := it.exec(sc, st); !ok {
				return flowNext, false
			}
		}
	}
}

func (it *interp) execFor(sc *Scope, s *ast.For) (flow, bool) {
	inner := sc
	if fs, ok := it.b.m.ScopeOf(s); ok {
		inner = fs
	}
	if !it.initDecls(inner, s.InitDecls) {
		return flowNext, false
	}
	for _, st := range s.Init {
		if _, ok := it.exec(inner, st); !ok {
			return flowNext, false
		}
	}
	return it.loop(inner, s.Cond, nil, s.Body, s.Step, false)
}

// exprStmt runs an expression used as a statement: ++, --, an assignment
// expression, or a call.
func (it *interp) exprStmt(sc *Scope, s *ast.ExprStmt) bool {
	switch x := s.X.(type) {
	case *ast.IncDec:
		op := "+="
		if x.Op == "--" {
			op = "-="
		}
		one := it.syntheticOne(x)
		return it.assign(sc, x, x.X, one, op)
	case *ast.AssignExpr:
		return it.assign(sc, x, x.LHS, x.RHS, x.Op)
	case *ast.Call:
		if id, ok := x.Func.(*ast.Ident); ok && len(id.Name) > 0 && id.Name[0] == '$' {
			return true // system tasks like $display do not affect the value
		}
		_, ok := it.evalIn(sc, x, nil)
		return ok
	case *ast.Paren:
		return it.exprStmt(sc, &ast.ExprStmt{X: x.X})
	}
	return it.fail(s, "this expression statement cannot appear in a constant function")
}

// syntheticOne is the literal 1 standing in for the operand of ++ and --. It is
// created once per statement so that its bound form is cached.
func (it *interp) syntheticOne(at ast.Node) ast.Expr {
	m := it.b.m
	if m.synthetic == nil {
		m.synthetic = map[ast.Node]ast.Expr{}
	}
	if e, ok := m.synthetic[at]; ok {
		return e
	}
	one := &ast.Literal{Kind: ast.LitInt, Text: "1"}
	one.SetPos(at.Pos())
	m.synthetic[at] = one
	return one
}

// syntheticOp builds "lhs op rhs" for a compound assignment, once per
// statement.
func (it *interp) syntheticOp(at ast.Node, op string, lhs, rhs ast.Expr) ast.Expr {
	m := it.b.m
	if m.syntheticOps == nil {
		m.syntheticOps = map[ast.Node]ast.Expr{}
	}
	if e, ok := m.syntheticOps[at]; ok {
		return e
	}
	bin := &ast.Binary{Op: op, X: lhs, Y: rhs}
	bin.SetPos(at.Pos())
	m.syntheticOps[at] = bin
	return bin
}

// lval is a resolved assignment target: a local variable, or a bit or part of
// one.
type lval struct {
	sym      *Symbol
	typ      Type // the type of the whole variable
	isReturn bool // the function's own name
	sel      BoundExpr
	hi       BoundExpr
	kind     ast.RangeKind
	selected bool
	part     bool
}

func (it *interp) lvalue(sc *Scope, e ast.Expr) (*lval, bool) {
	switch e := e.(type) {
	case *ast.Paren:
		return it.lvalue(sc, e.X)
	case *ast.Ident:
		sym := sc.Lookup(e.Name).Symbol
		if sym == nil {
			return nil, it.fail(e, "%q is not declared", e.Name)
		}
		if sym == it.sym {
			return &lval{sym: sym, typ: it.retType, isReturn: true}, true
		}
		if _, local := it.vars[sym]; !local {
			return nil, it.fail(e, "%q is not a local variable of the constant function", e.Name)
		}
		return &lval{sym: sym, typ: it.b.typeOfSymbol(sym)}, true
	case *ast.Index:
		base, ok := it.lvalue(sc, e.X)
		if !ok || base.selected {
			return nil, ok && it.fail(e, "only a single bit or part of a variable can be assigned in a constant function")
		}
		base.selected = true
		base.sel = it.bound(sc, e.Index, nil)
		return base, base.sel != nil
	case *ast.RangeSelect:
		base, ok := it.lvalue(sc, e.X)
		if !ok || base.selected {
			return nil, ok && it.fail(e, "only a single bit or part of a variable can be assigned in a constant function")
		}
		base.selected, base.part, base.kind = true, true, e.Kind
		base.sel, base.hi = it.bound(sc, e.Left, nil), it.bound(sc, e.Right, nil)
		return base, base.sel != nil && base.hi != nil
	}
	return nil, it.fail(e, "this target cannot be assigned in a constant function")
}

func (it *interp) load(l *lval) Const {
	if l.isReturn {
		return it.ret
	}
	return it.vars[l.sym]
}

func (it *interp) store(l *lval, c Const) {
	if l.isReturn {
		it.ret = c
		return
	}
	it.vars[l.sym] = c
}

func (it *interp) assign(sc *Scope, at ast.Node, lhs, rhs ast.Expr, op string) bool {
	l, ok := it.lvalue(sc, lhs)
	if !ok {
		return false
	}
	if op != "=" {
		bop := op[:len(op)-1] // "+=" is "+"
		rhs = it.syntheticOp(at, bop, lhs, rhs)
	}
	target := l.typ
	if l.selected {
		// A select's width follows from its bounds; the value is converted to
		// that many bits below.
		target = nil
	}
	val, ok := it.evalIn(sc, rhs, target)
	if !ok {
		return false
	}
	if !l.selected {
		it.store(l, val)
		return true
	}
	return it.storeSelect(sc, at, l, val)
}

// storeSelect writes val into a bit or part of a vector variable. A write
// outside the vector is ignored (LRM 11.5.1).
func (it *interp) storeSelect(sc *Scope, at ast.Node, l *lval, val Const) bool {
	cur := it.load(l)
	if cur.Kind != ConstInt {
		return it.fail(at, "only an integral variable can be partly assigned")
	}
	r, ew, ok := packedLayout(l.typ)
	if !ok {
		return it.fail(at, "cannot assign part of this variable in a constant function")
	}
	idx, ok := it.evalBoundInt(l.sel)
	if !ok {
		return false
	}
	var first, last int64
	if l.part {
		other, ok := it.evalBoundInt(l.hi)
		if !ok {
			return false
		}
		switch l.kind {
		case ast.RangeColon:
			first, last = idx, other
		case ast.RangePlus:
			first, last = idx, idx+other-1
			if r.Left >= r.Right {
				first, last = idx+other-1, idx
			}
		default:
			first, last = idx, idx-other+1
			if r.Left < r.Right {
				first, last = idx-other+1, idx
			}
		}
	} else {
		first, last = idx, idx
	}
	p1, p2 := bitPosition(r, ew, first), bitPosition(r, ew, last)
	hi, lo := max(p1, p2)+int64(ew)-1, min(p1, p2)
	w := int64(cur.Int.Width())
	if lo < 0 || hi >= w {
		return true
	}
	piece := withSign(it.b.conv(val, intT(int(hi-lo+1), false, true)).Int, false)
	parts := []svint.Value{}
	if hi < w-1 {
		parts = append(parts, svint.Slice(cur.Int, int(w-1), int(hi+1)))
	}
	parts = append(parts, piece)
	if lo > 0 {
		parts = append(parts, svint.Slice(cur.Int, int(lo-1), 0))
	}
	merged := withSign(svint.Concat(parts...), cur.Int.Signed())
	it.store(l, intConst(merged))
	return true
}

func (it *interp) evalBoundInt(be BoundExpr) (int64, bool) {
	c, ok := it.b.eval(be)
	if !ok {
		return 0, false
	}
	n, ok := c.Int64()
	if !ok {
		return 0, it.fail(be.Source(), "the index is not a known integer")
	}
	return n, true
}

func (it *interp) execCase(sc *Scope, s *ast.Case) (flow, bool) {
	subj, ok := it.evalIn(sc, s.Subject, nil)
	if !ok || subj.Kind != ConstInt {
		return flowNext, ok && it.fail(s, "the case subject is not an integral value")
	}
	var deflt ast.Stmt
	haveDefault := false
	for i := range s.Items {
		item := &s.Items[i]
		if item.Default {
			deflt, haveDefault = item.Body, true
			continue
		}
		for _, e := range item.Exprs {
			match, ok := it.caseMatches(sc, s, subj, e)
			if !ok {
				return flowNext, false
			}
			if match {
				return it.exec(sc, item.Body)
			}
		}
	}
	if haveDefault {
		return it.exec(sc, deflt)
	}
	return flowNext, true
}

func (it *interp) caseMatches(sc *Scope, s *ast.Case, subj Const, e ast.Expr) (bool, bool) {
	if vr, ok := e.(*ast.ValueRange); ok && s.Inside {
		lo, ok1 := it.evalIn(sc, vr.Lo, nil)
		hi, ok2 := it.evalIn(sc, vr.Hi, nil)
		if !ok1 || !ok2 || lo.Kind != ConstInt || hi.Kind != ConstInt {
			return false, false
		}
		in := svint.LogicalAnd(svint.Ge(subj.Int, lo.Int), svint.Le(subj.Int, hi.Int))
		return in.Bit(0) == svint.Bit1, true
	}
	c, ok := it.evalIn(sc, e, nil)
	if !ok || c.Kind != ConstInt {
		return false, ok && it.fail(e, "a case item is not an integral value")
	}
	a, b := subj.Int.AsUnsigned(), c.Int.AsUnsigned()
	w := max(a.Width(), b.Width())
	a, b = a.Resize(w), b.Resize(w)
	switch {
	case s.Inside:
		return svint.WildEq(a, b).Bit(0) == svint.Bit1, true
	case s.Kind == "casez" || s.Kind == "casex":
		for i := 0; i < w; i++ {
			x, y := a.Bit(i), b.Bit(i)
			wild := func(bit svint.Bit) bool {
				return bit == svint.BitZ || (s.Kind == "casex" && bit == svint.BitX)
			}
			if wild(x) || wild(y) {
				continue
			}
			if x != y {
				return false, true
			}
		}
		return true, true
	}
	return svint.CaseEq(a, b).Bit(0) == svint.Bit1, true
}
