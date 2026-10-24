package sema

import (
	"fmt"
	"strings"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// CheckStatements applies the statement-level rules of the LRM to every
// process, subroutine and continuous assignment in the compilation and returns
// what it finds, sorted by position:
//
//   - the target of an assignment is something assignable (LRM 10.3, 10.4): not
//     an input port, parameter, enum constant or genvar, and not a net when the
//     assignment is procedural (LRM 6.5);
//   - break and continue are inside a loop (LRM 12.8);
//   - return gives a value exactly when a function returns one (LRM 13.4);
//   - a function has no delays, event controls or waits (LRM 13.4.4);
//   - a variable's drivers are compatible: one that a continuous assignment
//     drives, or that always_comb, always_latch or always_ff writes, has no
//     other driver (LRM 6.5, 9.2.2).
//
// Names it cannot resolve are skipped: reporting them is Bind's job.
func (m *Model) CheckStatements() []diag.Diagnostic { return m.CheckStatementsIn(nil) }

// CheckStatementsIn is CheckStatements for the files whose path is in files
// (nil means all of them).
func (m *Model) CheckStatementsIn(files map[string]bool) []diag.Diagnostic {
	c := &checker{m: m, writes: map[*Symbol][]write{}}
	for _, f := range m.comp.files {
		if files != nil && !files[f.Path] {
			continue
		}
		us, _ := m.UnitScope(f.Path)
		for _, d := range f.Decls {
			c.decl(us, d)
		}
	}
	c.drivers()
	return c.set.Sorted()
}

type checker struct {
	m      *Model
	set    diag.Set
	writes map[*Symbol][]write
	order  []*Symbol // symbols in the order they were first written, for determinism
}

// write is one assignment to a variable made by a process.
type write struct {
	kind  string   // "continuous", "always_comb", "always_ff", "always_latch", "always", "initial", "final"
	proc  ast.Node // the process or assignment; two writes from one proc are not a conflict
	whole bool     // the whole variable, not a bit, part or element of it
	at    ast.Position
}

type procInfo struct {
	kind string
	node ast.Node
}

type subInfo struct {
	kind    string // "function" or "task"
	name    string
	sym     *Symbol
	returns bool // a function whose return gives a value
}

func (c *checker) errorf(code string, at ast.Position, format string, args ...any) {
	c.set.Add(diag.Diagnostic{Code: code, Severity: diag.Error, Range: at, Message: fmt.Sprintf(format, args...)})
}

// decl checks one declaration found in scope s.
func (c *checker) decl(s *Scope, d ast.Decl) {
	if s == nil {
		return
	}
	switch n := d.(type) {
	case *ast.Container:
		c.decls(c.scopeOf(n, s), n.Body)
	case *ast.Package:
		c.decls(c.scopeOf(n, s), n.Body)
	case *ast.Class:
		c.decls(c.scopeOf(n, s), n.Body)
	case *ast.Function:
		c.subroutine(c.scopeOf(n, s), "function", n.Name, n.Scope, n.ReturnType, n.Stmts)
	case *ast.Task:
		c.subroutine(c.scopeOf(n, s), "task", n.Name, n.Scope, ast.Type{}, n.Stmts)
	case *ast.Initial:
		c.process(s, &procInfo{"initial", n}, n.Body)
	case *ast.Final:
		c.process(s, &procInfo{"final", n}, n.Body)
	case *ast.Always:
		c.process(s, &procInfo{n.Kind, n}, n.Body)
	case *ast.AssertionItem:
		c.process(s, &procInfo{"assertion", n}, n.Body)
	case *ast.ContinuousAssign:
		for i := range n.Assigns {
			a := &n.Assigns[i]
			c.assignTarget(s, nil, &procInfo{"continuous", a}, a.LHS, "continuous")
		}
	case *ast.GenerateRegion:
		c.decls(s, n.Body)
	case *ast.GenerateBlock:
		c.decls(c.scopeOf(n, s), n.Body)
	case *ast.GenerateIf:
		c.decl(s, n.Then)
		if n.Else != nil {
			c.decl(s, n.Else)
		}
	case *ast.GenerateFor:
		c.decl(s, n.Body)
	case *ast.GenerateCase:
		for i := range n.Items {
			c.decl(s, n.Items[i].Body)
		}
	}
}

func (c *checker) decls(s *Scope, list []ast.Decl) {
	for _, d := range list {
		c.decl(s, d)
	}
}

func (c *checker) scopeOf(n ast.Node, fallback *Scope) *Scope {
	if s, ok := c.m.ScopeOf(n); ok {
		return s
	}
	return fallback
}

func (c *checker) process(s *Scope, p *procInfo, body ast.Stmt) {
	if body != nil {
		ast.Walk(&stmtWalker{c: c, ctx: stmtCtx{scope: s, proc: p}}, body)
	}
}

func (c *checker) subroutine(s *Scope, kind, name, class string, ret ast.Type, stmts []ast.Stmt) {
	info := &subInfo{kind: kind, name: name}
	if kind == "function" {
		info.returns = ret.Name != "void" && name != "new"
	}
	if s != nil && s.Parent != nil && class == "" {
		info.sym, _ = s.Parent.Own(name)
	}
	for _, st := range stmts {
		ast.Walk(&stmtWalker{c: c, ctx: stmtCtx{scope: s, sub: info}}, st)
	}
}

// stmtCtx is what a statement is checked against: the scope its names resolve
// in, how many loops enclose it, and whether it is in a process or subroutine.
type stmtCtx struct {
	scope *Scope
	loops int
	sub   *subInfo
	proc  *procInfo
}

type stmtWalker struct {
	c   *checker
	ctx stmtCtx
}

func (w *stmtWalker) with(f func(*stmtCtx)) *stmtWalker {
	n := *w
	f(&n.ctx)
	return &n
}

func (w *stmtWalker) Visit(n ast.Node) ast.Visitor {
	c, ctx := w.c, w.ctx
	switch n := n.(type) {
	case *ast.Block:
		if s, ok := c.m.ScopeOf(n); ok {
			return w.with(func(x *stmtCtx) { x.scope = s })
		}
	case *ast.For:
		return w.with(func(x *stmtCtx) {
			x.loops++
			if s, ok := c.m.ScopeOf(n); ok {
				x.scope = s
			}
		})
	case *ast.Foreach:
		return w.with(func(x *stmtCtx) {
			x.loops++
			if s, ok := c.m.ScopeOf(n); ok {
				x.scope = s
			}
		})
	case *ast.While, *ast.DoWhile, *ast.Repeat, *ast.Forever:
		return w.with(func(x *stmtCtx) { x.loops++ })
	case *ast.Break:
		if ctx.loops == 0 {
			c.errorf(diag.CodeBreakOutsideLoop, n.Pos(), "break is not inside a loop")
		}
	case *ast.Continue:
		if ctx.loops == 0 {
			c.errorf(diag.CodeContinueOutsideLoop, n.Pos(), "continue is not inside a loop")
		}
	case *ast.Return:
		c.checkReturn(ctx, n)
	case *ast.Assign:
		kind := "blocking"
		if n.Nonblocking() {
			kind = "nonblocking"
		}
		c.assignTarget(ctx.scope, ctx.sub, ctx.procFor(), n.LHS, kind)
		if n.Control != nil {
			c.timing(ctx, n.Control.Pos())
		}
	case *ast.ProcAssign:
		c.assignTarget(ctx.scope, ctx.sub, nil, n.LHS, "force")
	case *ast.AssignExpr:
		c.assignTarget(ctx.scope, ctx.sub, ctx.procFor(), n.LHS, "blocking")
	case *ast.IncDec:
		c.assignTarget(ctx.scope, ctx.sub, ctx.procFor(), n.X, "blocking")
	case *ast.TimedStmt:
		c.timing(ctx, n.Pos())
	case *ast.Wait:
		c.timing(ctx, n.Pos())
	case *ast.WaitFork:
		c.timing(ctx, n.Pos())
	case *ast.WaitOrder:
		c.timing(ctx, n.Pos())
	}
	return w
}

func (ctx stmtCtx) procFor() *procInfo { return ctx.proc }

func (c *checker) timing(ctx stmtCtx, at ast.Position) {
	if ctx.sub != nil && ctx.sub.kind == "function" {
		c.errorf(diag.CodeTimingInFunction, at, "function %q cannot contain a delay, event control or wait", ctx.sub.name)
	}
}

func (c *checker) checkReturn(ctx stmtCtx, n *ast.Return) {
	switch {
	case ctx.sub == nil:
		c.errorf(diag.CodeReturnOutsideSubroutine, n.Pos(), "return is not inside a function or task")
	case n.Value != nil && !ctx.sub.returns:
		c.errorf(diag.CodeReturnValueInVoid, n.Pos(), "%s %q does not return a value", ctx.sub.kind, ctx.sub.name)
	case n.Value == nil && ctx.sub.returns:
		c.errorf(diag.CodeMissingReturnValue, n.Pos(), "function %q must return a value", ctx.sub.name)
	}
}

// target is one variable or net an assignment writes.
type target struct {
	sym   *Symbol
	whole bool
	hier  bool // reached through a hierarchical or scoped name
}

// lvalues finds what an assignment target writes. ok is false if the
// expression cannot be assigned to at all.
func (c *checker) lvalues(s *Scope, e ast.Expr, whole bool) (ts []target, ok bool) {
	switch e := e.(type) {
	case *ast.Ident:
		if strings.HasPrefix(e.Name, "$") {
			return nil, false
		}
		if sym := s.Lookup(e.Name).Symbol; sym != nil {
			return []target{{sym: sym, whole: whole}}, true
		}
		return nil, true // undeclared: reported when the name is bound
	case *ast.Paren:
		return c.lvalues(s, e.X, whole)
	case *ast.Index:
		return c.lvalues(s, e.X, false)
	case *ast.RangeSelect:
		return c.lvalues(s, e.X, false)
	case *ast.WithRange:
		return c.lvalues(s, e.X, false)
	case *ast.MemberSelect:
		switch e.X.(type) {
		case *ast.This, *ast.Super:
			return nil, true // a member of the object: not tracked here
		}
		if path, isPath := dottedPath(e); isPath {
			if first := s.Lookup(path[0]).Symbol; first == nil || !isValueKind(first.Kind) {
				if sym, rest := s.LookupPath(path); sym != nil {
					_ = rest
					return []target{{sym: sym, hier: true}}, true
				}
				return nil, true
			}
		}
		return c.lvalues(s, e.X, false)
	case *ast.ScopedName:
		if path, isPath := scopedPath(e); isPath {
			if sym := s.LookupQualified(path).Symbol; sym != nil {
				return []target{{sym: sym, hier: true}}, true
			}
		}
		return nil, true
	case *ast.Concat:
		for _, el := range e.Elems {
			sub, ok := c.lvalues(s, el, whole)
			if !ok {
				return nil, false
			}
			ts = append(ts, sub...)
		}
		return ts, true
	case *ast.AssignPattern, *ast.Streaming:
		return nil, true
	case *ast.This, *ast.Super:
		return nil, false
	case *ast.BadExpr:
		return nil, true
	}
	return nil, false
}

// isNetSymbol reports whether the symbol is a net (as opposed to a variable).
func isNetSymbol(sym *Symbol) bool {
	switch sym.Kind {
	case SymNet:
		return true
	case SymPort:
		p, ok := sym.Decl.(*ast.Port)
		if !ok {
			return false
		}
		if v, ok := sym.Redeclared.(*ast.Variable); ok {
			return netTypes[v.Type.Name] // "output q; reg q;" makes q a variable
		}
		if p.Direction == ast.DirInout {
			return true
		}
		return p.Type.Name == "" || netTypes[p.Type.Name]
	}
	return false
}

// assignTarget checks the target of one assignment. kind is "blocking",
// "nonblocking", "continuous" or "force"; proc is the process (nil inside a
// subroutine, whose writes are not attributed to a process).
func (c *checker) assignTarget(s *Scope, sub *subInfo, proc *procInfo, lhs ast.Expr, kind string) {
	if lhs == nil || s == nil {
		return
	}
	ts, ok := c.lvalues(s, lhs, true)
	if !ok {
		c.errorf(diag.CodeInvalidLValue, lhs.Pos(), "this expression cannot be assigned to")
		return
	}
	for _, t := range ts {
		sym := t.sym
		switch sym.Kind {
		case SymParameter, SymEnumMember, SymTypeParam:
			c.errorf(diag.CodeAssignToConstant, lhs.Pos(), "%s %q is a constant", sym.Kind, sym.Name)
			continue
		case SymGenvar:
			c.errorf(diag.CodeAssignToConstant, lhs.Pos(), "genvar %q cannot be assigned outside a generate loop header", sym.Name)
			continue
		case SymFunction:
			if sub != nil && sub.sym == sym {
				continue // "f = value;" sets a function's return value
			}
			c.errorf(diag.CodeInvalidLValue, lhs.Pos(), "function %q cannot be assigned to", sym.Name)
			continue
		case SymTask, SymModule, SymInterface, SymProgram, SymClass, SymTypedef, SymInstance, SymBlock,
			SymGenerateBlock, SymModport, SymCovergroup, SymProperty, SymSequence, SymLet, SymNettype, SymClocking, SymUDP:
			c.errorf(diag.CodeInvalidLValue, lhs.Pos(), "%s %q cannot be assigned to", sym.Kind, sym.Name)
			continue
		}
		if t.hier {
			continue
		}
		if sym.Kind == SymPort {
			if p, ok := sym.Decl.(*ast.Port); ok && p.Direction == ast.DirInput && sym.Scope.Kind != ScopeSubroutine {
				c.errorf(diag.CodeAssignToInput, lhs.Pos(), "input port %q cannot be assigned inside its module", sym.Name)
				continue
			}
		}
		net := isNetSymbol(sym)
		if net && (kind == "blocking" || kind == "nonblocking") {
			c.errorf(diag.CodeProceduralNet, lhs.Pos(), "net %q cannot be assigned in a procedural statement; declare it as a variable", sym.Name)
			continue
		}
		if net || proc == nil || kind == "force" || proc.kind == "assertion" {
			continue
		}
		if !c.ownsDrivers(sym) {
			continue
		}
		if _, seen := c.writes[sym]; !seen {
			c.order = append(c.order, sym)
		}
		c.writes[sym] = append(c.writes[sym], write{kind: proc.kind, proc: proc.node, whole: t.whole, at: lhs.Pos()})
	}
}

// ownsDrivers reports whether writes to the symbol are tracked for the driver
// rules: variables of design elements and packages, not class members or
// locals of subroutines.
func (c *checker) ownsDrivers(sym *Symbol) bool {
	if sym.Kind != SymVariable && sym.Kind != SymPort {
		return false
	}
	for s := sym.Scope; s != nil; s = s.Parent {
		switch s.Kind {
		case ScopeClass, ScopeSubroutine:
			return false
		}
	}
	return true
}

func exclusiveKind(kind string) bool {
	return kind == "always_comb" || kind == "always_latch" || kind == "always_ff"
}

func before(a, b ast.Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character < b.Character)
}

// drivers reports variables that have drivers the LRM does not allow together
// (LRM 6.5, 9.2.2.2, 9.2.2.4). A variable written as a whole by a continuous
// assignment or by always_comb, always_latch or always_ff may have no other
// driver; an initial or final block may still set a value for always_*. A
// partial write conflicts only with a whole-variable one, so different elements
// or bits of an array or vector can be driven separately.
func (c *checker) drivers() {
	for _, sym := range c.order {
		ws := c.writes[sym]
		for i, w := range ws {
			cont := w.kind == "continuous"
			if !w.whole || !(cont || exclusiveKind(w.kind)) {
				continue
			}
			for j, o := range ws {
				if i == j || o.proc == w.proc {
					continue
				}
				if !cont && (o.kind == "initial" || o.kind == "final") {
					continue
				}
				// Report at the later of the two writes, once per pair.
				late, early := w, o
				if before(w.at, o.at) {
					late, early = o, w
				}
				c.set.Add(diag.Diagnostic{
					Code:     diag.CodeMultipleDrivers,
					Severity: diag.Error,
					Range:    late.at,
					Message:  fmt.Sprintf("variable %q is driven by %s and by %s", sym.Name, describeDriver(early.kind), describeDriver(late.kind)),
					Related:  []diag.Related{{Range: early.at, Message: "the other driver"}},
				})
			}
		}
	}
}

func describeDriver(kind string) string {
	if kind == "continuous" {
		return "a continuous assignment"
	}
	return kind
}
