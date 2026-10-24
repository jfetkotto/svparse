package lint

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/elab"
	"github.com/jfetkotto/svparse/sema"
)

func init() {
	Register(Rule{
		Name:     "overlapping-drivers",
		Doc:      "two processes drive overlapping bits or elements of one variable, where one is a continuous assignment or an always_comb, always_latch or always_ff (LRM 6.5, 9.2.2)",
		Severity: diag.Warning,
		Check:    checkMultipleDrivers,
	})
}

// selLevel is one step of a write target below the variable: a struct member,
// or a constant bit/element interval [lo, hi].
type selLevel struct {
	field  string
	lo, hi int64
}

// driver is one write to part of a variable by one process.
type driver struct {
	proc   ast.Node // the assign or always that owns the write
	strong bool     // continuous assignment or always_comb/latch/ff
	kind   string
	at     ast.Position
	region []selLevel
}

type driverKey struct {
	owner *elab.Node
	sym   *sema.Symbol
}

// checkMultipleDrivers finds partial writes that overlap. sema reports a
// whole-variable write combined with any other driver as an error; what is left
// is the per-bit case ("assign v[3:0] = a; assign v[5:2] = b;"), which needs
// constant selects, so it is evaluated per instance and generate iteration, with
// the genvars and parameters of that node. A select that is not constant is
// skipped rather than guessed at, so findings are certain overlaps.
func checkMultipleDrivers(c *Context) {
	if c.Design == nil {
		return
	}
	drivers := map[driverKey][]driver{}
	var order []driverKey
	c.Design.Walk(func(n *elab.Node) bool {
		if c.Cancelled() {
			return false
		}
		if n.Scope == nil || n.Env == nil {
			return true
		}
		var body []ast.Decl
		if d, ok := n.Decl.(*ast.GenerateBlock); ok {
			body = d.Body
		} else if n.Def != nil {
			if cont, ok := n.Def.Decl.(*ast.Container); ok {
				body = cont.Body
			}
		}
		owner := n
		for owner.Def == nil && owner.Parent != nil {
			owner = owner.Parent
		}
		col := &driverCollector{c: c, node: n, owner: owner, out: drivers, order: &order}
		for _, d := range body {
			ast.Walk(col, d)
		}
		return true
	})
	type pair struct{ a, b ast.Position }
	seen := map[pair]bool{}
	for _, key := range order {
		ds := drivers[key]
		for i := range ds {
			for j := i + 1; j < len(ds); j++ {
				a, b := ds[i], ds[j]
				if a.proc == b.proc || !(a.strong || b.strong) || !overlaps(a.region, b.region) {
					continue
				}
				late, early := b, a
				if !before(a.at, b.at) {
					late, early = a, b
				}
				if seen[pair{early.at, late.at}] {
					continue
				}
				seen[pair{early.at, late.at}] = true
				c.ReportRelated(late.at, []diag.Related{{Range: early.at, Message: "the other driver"}},
					"bits of %q are driven by %s and by %s", key.sym.Name, describe(early.kind), describe(late.kind))
			}
		}
	}
}

func before(a, b ast.Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character < b.Character)
}

func describe(kind string) string {
	if kind == "continuous" {
		return "a continuous assignment"
	}
	return kind
}

// overlaps reports whether two target regions can name the same storage. A
// region that ends first covers everything below it, so only the shared levels
// are compared; levels of different shapes (a member against a bit select) are
// not compared.
func overlaps(a, b []selLevel) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		if (x.field != "") != (y.field != "") {
			return false
		}
		if x.field != "" {
			if x.field != y.field {
				return false
			}
			continue
		}
		if x.hi < y.lo || y.hi < x.lo {
			return false
		}
	}
	return true
}

type driverCollector struct {
	c     *Context
	node  *elab.Node
	owner *elab.Node
	out   map[driverKey][]driver
	order *[]driverKey
	// proc, kind and strong describe the enclosing always; proc is nil
	// outside one.
	proc   ast.Node
	kind   string
	strong bool
	local  map[string]bool
}

func (w *driverCollector) Visit(n ast.Node) ast.Visitor {
	if n == nil || w.c.Cancelled() {
		return nil
	}
	switch n := n.(type) {
	case *ast.GenerateIf, *ast.GenerateFor, *ast.GenerateCase, *ast.GenerateBlock:
		return nil // elaborated as nodes of their own
	case *ast.Always:
		sub := *w
		sub.proc, sub.kind, sub.local = n, n.Kind, localNames(n.Body)
		sub.strong = n.Kind == "always_comb" || n.Kind == "always_latch" || n.Kind == "always_ff"
		return &sub
	case *ast.ContinuousAssign:
		for i := range n.Assigns {
			w.record(&n.Assigns[i], n.Assigns[i].LHS, "continuous", true)
		}
		return nil
	case *ast.Assign:
		if w.proc != nil {
			w.record(w.proc, n.LHS, w.kind, w.strong)
		}
	}
	return w
}

// record notes the write of one assignment target (each element of a
// concatenation separately).
func (w *driverCollector) record(proc ast.Node, lhs ast.Expr, kind string, strong bool) {
	if cat, ok := lhs.(*ast.Concat); ok {
		for _, el := range cat.Elems {
			w.record(proc, el, kind, strong)
		}
		return
	}
	name, region, ok := w.target(lhs)
	if !ok || w.local[name] {
		return
	}
	sym := w.node.Scope.Lookup(name).Symbol
	if sym == nil || !isVariable(sym) {
		return
	}
	key := driverKey{w.owner, sym}
	if _, seen := w.out[key]; !seen {
		*w.order = append(*w.order, key)
	}
	w.out[key] = append(w.out[key], driver{proc: proc, strong: strong, kind: kind, at: lhs.Pos(), region: region})
}

// target resolves an assignment target to its variable name and the constant
// selects below it, outermost first. It fails for a select that is not
// constant, so the write is left out of the comparison.
func (w *driverCollector) target(e ast.Expr) (string, []selLevel, bool) {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name, nil, true
	case *ast.Paren:
		return w.target(e.X)
	case *ast.MemberSelect:
		name, r, ok := w.target(e.X)
		return name, append(r, selLevel{field: e.Member}), ok
	case *ast.Index:
		name, r, ok := w.target(e.X)
		if !ok {
			return "", nil, false
		}
		i, ok := w.constant(e.Index)
		return name, append(r, selLevel{lo: i, hi: i}), ok
	case *ast.RangeSelect:
		name, r, ok := w.target(e.X)
		if !ok {
			return "", nil, false
		}
		l, ok1 := w.constant(e.Left)
		rt, ok2 := w.constant(e.Right)
		if !ok1 || !ok2 {
			return "", nil, false
		}
		switch e.Kind {
		case ast.RangePlus:
			return name, append(r, selLevel{lo: l, hi: l + rt - 1}), rt > 0
		case ast.RangeMinus:
			return name, append(r, selLevel{lo: l - rt + 1, hi: l}), rt > 0
		}
		return name, append(r, selLevel{lo: min(l, rt), hi: max(l, rt)}), true
	}
	return "", nil, false
}

func (w *driverCollector) constant(e ast.Expr) (int64, bool) {
	c, ds := w.c.Model.Eval(w.node.Scope, e, w.node.Env)
	if len(ds) != 0 {
		return 0, false
	}
	return c.Int64()
}

var netTypeNames = map[string]bool{
	"wire": true, "tri": true, "wand": true, "wor": true, "triand": true, "trior": true,
	"tri0": true, "tri1": true, "supply0": true, "supply1": true, "uwire": true, "trireg": true,
}

// isVariable reports whether the symbol is a variable rather than a net, whose
// several drivers are resolved and legal (LRM 6.5).
func isVariable(sym *sema.Symbol) bool {
	switch sym.Kind {
	case sema.SymVariable:
		v, ok := sym.Decl.(*ast.Variable)
		return !ok || (v.NetQualifier == "" && !netTypeNames[v.Type.Name])
	case sema.SymPort:
		p, ok := sym.Decl.(*ast.Port)
		if !ok || p.Direction == ast.DirInout {
			return false
		}
		if v, ok := sym.Redeclared.(*ast.Variable); ok {
			return !netTypeNames[v.Type.Name]
		}
		return p.Type.Name != "" && !netTypeNames[p.Type.Name]
	}
	return false
}
