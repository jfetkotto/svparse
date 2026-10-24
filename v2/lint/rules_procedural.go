package lint

import (
	"sort"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
)

func init() {
	Register(Rule{
		Name:     "blocking-in-sequential",
		Doc:      "a blocking assignment to a signal in a clocked always block: use <= so all flip-flops update together",
		Severity: diag.Warning,
		Check:    checkBlockingInSequential,
	})
	Register(Rule{
		Name:     "nonblocking-in-combinational",
		Doc:      "a nonblocking assignment in always_comb or always @*: use = so the block settles in order",
		Severity: diag.Warning,
		Check:    checkNonblockingInCombinational,
	})
	Register(Rule{
		Name:     "case-no-default",
		Doc:      "a case statement with no default item and no unique or priority qualifier leaves some values unhandled",
		Severity: diag.Warning,
		Check:    checkCaseDefault,
	})
	Register(Rule{
		Name:     "latch-inferred",
		Doc:      "a signal is not assigned on every path of a combinational block, so synthesis infers a latch",
		Severity: diag.Warning,
		Check:    checkLatches,
	})
	Register(Rule{
		Name:     "incomplete-sensitivity",
		Doc:      "a signal read by an always @(list) block is missing from its sensitivity list, so simulation and synthesis disagree",
		Severity: diag.Warning,
		Check:    checkSensitivity,
	})
}

// assignments calls f for every assignment inside a statement whose target is
// state of the module: targets that are only block locals or loop variables
// are skipped.
func assignments(body ast.Stmt, f func(a *ast.Assign, root string)) {
	locals := localNames(body)
	ast.Inspect(body, func(n ast.Node) bool {
		a, ok := n.(*ast.Assign)
		if !ok {
			return true
		}
		for _, root := range lvalueRoots(a.LHS) {
			if !locals[root] {
				f(a, root)
				break // one report per assignment
			}
		}
		return true
	})
}

func checkBlockingInSequential(c *Context) {
	eachProcess(c, func(p *process) {
		if p.kind != procSequential {
			return
		}
		assignments(p.body, func(a *ast.Assign, root string) {
			if !a.Nonblocking() {
				c.Report(a.Pos(), "blocking assignment to %q in a clocked block; use <=", root)
			}
		})
	})
}

func checkNonblockingInCombinational(c *Context) {
	eachProcess(c, func(p *process) {
		if p.kind != procCombinational {
			return
		}
		assignments(p.body, func(a *ast.Assign, root string) {
			if a.Nonblocking() {
				c.Report(a.Pos(), "nonblocking assignment to %q in a combinational block; use =", root)
			}
		})
	})
}

func checkCaseDefault(c *Context) {
	for _, f := range c.Files() {
		ast.Inspect(f, func(n ast.Node) bool {
			cs, ok := n.(*ast.Case)
			if !ok || cs.Qualifier != "" {
				return true
			}
			for i := range cs.Items {
				if cs.Items[i].Default {
					return true
				}
			}
			c.Report(cs.Pos(), "%s statement has no default item", caseKeyword(cs))
			return true
		})
	}
}

func caseKeyword(cs *ast.Case) string {
	if cs.Kind == "" {
		return "case"
	}
	return cs.Kind
}

// flow is what a statement assigns: the signals it assigns on every path, and
// those it assigns on some path (with where first).
type flow struct {
	definite map[string]bool
	possible map[string]ast.Position
}

func newFlow() flow { return flow{definite: map[string]bool{}, possible: map[string]ast.Position{}} }

func (f flow) addPossible(name string, at ast.Position) {
	if _, ok := f.possible[name]; !ok {
		f.possible[name] = at
	}
}

// seq appends the flow of a statement run after f.
func (f flow) seq(g flow) {
	for k := range g.definite {
		f.definite[k] = true
	}
	for k, at := range g.possible {
		f.addPossible(k, at)
	}
}

// branch joins alternative flows: assigned on every path only if every branch
// assigns it.
func branch(alts []flow) flow {
	out := newFlow()
	if len(alts) == 0 {
		return out
	}
	for k := range alts[0].definite {
		all := true
		for _, a := range alts[1:] {
			if !a.definite[k] {
				all = false
			}
		}
		if all {
			out.definite[k] = true
		}
	}
	for _, a := range alts {
		for k, at := range a.possible {
			out.addPossible(k, at)
		}
	}
	return out
}

// onlyPossible keeps what a flow assigns on some path but claims nothing about
// every path.
func onlyPossible(f flow) flow {
	out := newFlow()
	out.possible = f.possible
	return out
}

// assignedBy computes the flow of a statement. It works per signal, not per
// bit: a part-select write counts as assigning the signal, so a loop that fills
// a vector bit by bit is not reported. A "for" body is assumed to run; "while"
// and "forever" bodies are not; a case with no default assigns on every path
// only if it is unique or priority.
func assignedBy(s ast.Stmt) flow {
	out := newFlow()
	switch s := s.(type) {
	case *ast.Block:
		for _, st := range s.Stmts {
			out.seq(assignedBy(st))
		}
	case *ast.Labeled:
		return assignedBy(s.Body)
	case *ast.TimedStmt:
		return assignedBy(s.Body)
	case *ast.Assign:
		for _, root := range lvalueRoots(s.LHS) {
			out.definite[root] = true
			out.addPossible(root, s.Pos())
		}
	case *ast.ProcAssign:
		for _, root := range lvalueRoots(s.LHS) {
			out.definite[root] = true
			out.addPossible(root, s.Pos())
		}
	case *ast.If:
		then := assignedBy(s.Then)
		els := newFlow()
		if s.Else != nil {
			els = assignedBy(s.Else)
		}
		return branch([]flow{then, els})
	case *ast.Case:
		var alts []flow
		hasDefault := false
		for i := range s.Items {
			if s.Items[i].Default {
				hasDefault = true
			}
			alts = append(alts, assignedBy(s.Items[i].Body))
		}
		if !hasDefault && s.Qualifier == "" {
			alts = append(alts, newFlow()) // the values no item matches
		}
		return branch(alts)
	case *ast.For:
		return assignedBy(s.Body)
	case *ast.Foreach:
		return assignedBy(s.Body)
	case *ast.Repeat:
		return assignedBy(s.Body)
	case *ast.DoWhile:
		return assignedBy(s.Body)
	case *ast.While:
		return onlyPossible(assignedBy(s.Body))
	case *ast.Forever:
		return onlyPossible(assignedBy(s.Body))
	}
	return out
}

func checkLatches(c *Context) {
	eachProcess(c, func(p *process) {
		if p.kind != procCombinational {
			return
		}
		locals := localNames(p.body)
		f := assignedBy(p.body)
		var names []string
		for name := range f.possible {
			if !f.definite[name] && !locals[name] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			c.Report(f.possible[name], "%q is not assigned on every path of this combinational block; a latch is inferred", name)
		}
	})
}

func checkSensitivity(c *Context) {
	eachProcess(c, func(p *process) {
		if p.kind != procCombinational || !p.explicitList {
			return
		}
		listed := map[string]bool{}
		for _, ev := range p.control.Events {
			ast.Inspect(ev.X, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					listed[id.Name] = true
				}
				return true
			})
		}
		locals := localNames(p.body)
		written := map[string]bool{}
		assignedAny := assignedBy(p.body)
		for name := range assignedAny.possible {
			written[name] = true
		}
		missing := map[string]bool{}
		for _, name := range readNames(p.body) {
			if listed[name] || locals[name] || written[name] || missing[name] {
				continue
			}
			if res := p.scope.Lookup(name); res.Symbol == nil || !isSignal(res.Symbol.Kind) {
				continue // a parameter, enum constant, function, ...
			}
			missing[name] = true
			c.Report(p.node.Pos(), "%q is read but is not in the sensitivity list", name)
		}
	})
}

func isSignal(k sema.SymKind) bool {
	return k == sema.SymVariable || k == sema.SymNet || k == sema.SymPort
}

// readNames lists the identifiers a statement reads, in source order,
// excluding the bare targets of assignments (their index expressions are reads).
func readNames(body ast.Stmt) []string {
	var out []string
	var reads func(e ast.Expr)
	var target func(e ast.Expr)
	reads = func(e ast.Expr) {
		ast.Inspect(e, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				out = append(out, n.Name)
			case *ast.MemberSelect:
				reads(n.X)
				return false
			}
			return true
		})
	}
	target = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Paren:
			target(e.X)
		case *ast.Index:
			target(e.X)
			reads(e.Index)
		case *ast.RangeSelect:
			target(e.X)
			reads(e.Left)
			reads(e.Right)
		case *ast.MemberSelect:
			target(e.X)
		case *ast.Concat:
			for _, el := range e.Elems {
				target(el)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Assign:
			target(n.LHS)
			reads(n.RHS)
			return false
		case *ast.ProcAssign:
			target(n.LHS)
			if n.RHS != nil {
				reads(n.RHS)
			}
			return false
		case *ast.Ident:
			out = append(out, n.Name)
		}
		return true
	})
	return out
}
