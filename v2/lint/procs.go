package lint

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/sema"
)

// procKind classifies an always procedure by what it models.
type procKind uint8

const (
	// procOther: no sensitivity (a clock generator, "always #5 ...") or one the
	// classification cannot read.
	procOther procKind = iota
	// procSequential: always_ff, or always @ with an edge in its event list.
	procSequential
	// procCombinational: always_comb, always @*, or always @ with levels only.
	procCombinational
	// procLatch: always_latch.
	procLatch
)

// process is one always procedure with the scope its names resolve in.
type process struct {
	node *ast.Always
	// scope is the innermost scope at the procedure.
	scope *sema.Scope
	kind  procKind
	// control is the event control of "always @(...)", else nil.
	control *ast.TimingControl
	// body is what runs on each trigger: for "always @(...) stmt" the statement
	// after the event control, else the whole body.
	body ast.Stmt
	// explicitList is true for "always @(a or b)": a hand-written sensitivity
	// list, as opposed to always_comb or @*.
	explicitList bool
}

// eachProcess calls f for every always procedure of the design, in source
// order.
func eachProcess(c *Context, f func(p *process)) {
	for _, file := range c.Files() {
		us, _ := c.Model.UnitScope(file.Path)
		ast.Walk(&procWalker{c: c, scope: us, f: f}, file)
	}
}

type procWalker struct {
	c     *Context
	scope *sema.Scope
	f     func(*process)
}

func (w *procWalker) Visit(n ast.Node) ast.Visitor {
	if n == nil || w.c.Cancelled() {
		return nil
	}
	if a, ok := n.(*ast.Always); ok {
		w.f(classify(a, w.scope))
		return nil
	}
	if s, ok := w.c.Model.ScopeOf(n); ok {
		return &procWalker{c: w.c, scope: s, f: w.f}
	}
	return w
}

func classify(a *ast.Always, scope *sema.Scope) *process {
	p := &process{node: a, scope: scope, body: a.Body}
	switch a.Kind {
	case "always_ff":
		p.kind = procSequential
	case "always_comb":
		p.kind = procCombinational
	case "always_latch":
		p.kind = procLatch
	default:
		ts, ok := a.Body.(*ast.TimedStmt)
		if !ok || ts.Control == nil || (!ts.Control.Star && len(ts.Control.Events) == 0) {
			return p
		}
		p.control, p.body = ts.Control, ts.Body
		p.kind = procCombinational
		p.explicitList = !ts.Control.Star
		for _, ev := range ts.Control.Events {
			if ev.Edge != "" {
				p.kind = procSequential
				p.explicitList = false
			}
		}
	}
	return p
}

// localNames are the variables declared inside a statement: block locals and
// loop variables. They are temporaries, not state of the module.
func localNames(s ast.Stmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(s, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Block:
			for _, d := range n.Decls {
				if v, ok := d.(*ast.Variable); ok {
					out[v.Name] = true
				}
			}
		case *ast.For:
			for _, d := range n.InitDecls {
				if v, ok := d.(*ast.Variable); ok {
					out[v.Name] = true
				}
			}
		case *ast.Foreach:
			for _, name := range n.Vars {
				out[name] = true
			}
		}
		return true
	})
	return out
}

// lvalueRoot returns the name of the variable an assignment target writes: the
// identifier under any selects, "x" for "x[i].f", or "" for a target it cannot
// name (a concatenation has several; see lvalueRoots).
func lvalueRoots(e ast.Expr) []string {
	switch e := e.(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.Paren:
		return lvalueRoots(e.X)
	case *ast.Index:
		return lvalueRoots(e.X)
	case *ast.RangeSelect:
		return lvalueRoots(e.X)
	case *ast.WithRange:
		return lvalueRoots(e.X)
	case *ast.MemberSelect:
		return lvalueRoots(e.X)
	case *ast.Concat:
		var out []string
		for _, el := range e.Elems {
			out = append(out, lvalueRoots(el)...)
		}
		return out
	}
	return nil
}
