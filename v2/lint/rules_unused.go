package lint

import (
	"regexp"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
)

func init() {
	Register(Rule{
		Name:     "unused-signal",
		Doc:      "a net or variable is never read, or is assigned but never read",
		Severity: diag.Warning,
		Check:    func(c *Context) { checkSignals(c, false) },
	})
	Register(Rule{
		Name:     "undriven-signal",
		Doc:      "a net or variable is read but nothing ever drives it, so it stays x or z",
		Severity: diag.Warning,
		Check:    func(c *Context) { checkSignals(c, true) },
	})
	Register(Rule{
		Name:     "unused-port",
		Doc:      "an input port is never read inside its module",
		Severity: diag.Warning,
		Check:    func(c *Context) { checkPorts(c, true) },
	})
	Register(Rule{
		Name:     "undriven-port",
		Doc:      "an output port is never driven inside its module",
		Severity: diag.Warning,
		Check:    func(c *Context) { checkPorts(c, false) },
	})
	Register(Rule{
		Name:     "unused-parameter",
		Doc:      "a parameter or localparam is never referenced",
		Severity: diag.Info,
		Check:    checkParameters,
	})
}

// deliberatelyUnused matches the names engineers give signals they know are
// unused: a leading underscore, or "unused"/"dummy" at the start.
var deliberatelyUnused = regexp.MustCompile(`(?i)^(_|unused|dummy)`)

// eachSignalScope calls f for the scope of every module and program, and of
// the generate blocks inside it, in definition order. Interfaces are skipped:
// their signals are used through modports and virtual interfaces, which the
// usage analysis does not follow. So are packages and classes, whose members
// are used from elsewhere.
func eachSignalScope(c *Context, f func(s *sema.Scope)) {
	var visit func(s *sema.Scope, depth int)
	visit = func(s *sema.Scope, depth int) {
		if depth > 64 || c.Cancelled() {
			return
		}
		f(s)
		for _, sym := range s.Symbols() {
			if sym.Child != nil && sym.Child.Kind == sema.ScopeGenerate {
				visit(sym.Child, depth+1)
			}
		}
	}
	for _, def := range c.Comp.Definitions() {
		if def.Kind != sema.DefModule && def.Kind != sema.DefProgram {
			continue
		}
		if s, ok := c.Model.DefinitionScope(def); ok && s.Def == def {
			visit(s, 0)
		}
	}
}

func checkSignals(c *Context, undriven bool) {
	u := c.Usage()
	eachSignalScope(c, func(s *sema.Scope) {
		for _, sym := range s.Symbols() {
			if sym.Kind != sema.SymVariable && sym.Kind != sema.SymNet {
				continue
			}
			v, ok := sym.Decl.(*ast.Variable)
			if !ok || deliberatelyUnused.MatchString(sym.Name) || constantNet(v) {
				continue
			}
			read, written := u.Read(sym), u.Written(sym)
			switch {
			case undriven && read && !written:
				c.Report(sym.Pos(), "signal %q is read but never driven", sym.Name)
			case !undriven && !read && !written:
				c.Report(sym.Pos(), "signal %q is declared but never used", sym.Name)
			case !undriven && !read && written:
				c.Report(sym.Pos(), "signal %q is assigned but never read", sym.Name)
			}
		}
	})
}

// constantNet reports whether the declaration is a supply net, which is
// permanently driven.
func constantNet(v *ast.Variable) bool {
	return v.Type.Name == "supply0" || v.Type.Name == "supply1"
}

func checkPorts(c *Context, unused bool) {
	u := c.Usage()
	eachSignalScope(c, func(s *sema.Scope) {
		for _, sym := range s.Symbols() {
			p, ok := sym.Decl.(*ast.Port)
			if sym.Kind != sema.SymPort || !ok || deliberatelyUnused.MatchString(sym.Name) {
				continue
			}
			read, written := u.Read(sym), u.Written(sym)
			switch {
			case unused && p.Direction == ast.DirInput && !read:
				c.Report(sym.Pos(), "input port %q is never read", sym.Name)
			case unused && p.Direction == ast.DirInout && !read && !written:
				c.Report(sym.Pos(), "inout port %q is never used", sym.Name)
			case !unused && p.Direction == ast.DirOutput && !written:
				c.Report(sym.Pos(), "output port %q is never driven", sym.Name)
			case !unused && p.Direction == ast.DirInout && !written && read:
				c.Report(sym.Pos(), "inout port %q is read but never driven", sym.Name)
			}
		}
	})
}

func checkParameters(c *Context) {
	u := c.Usage()
	eachSignalScope(c, func(s *sema.Scope) {
		for _, sym := range s.Symbols() {
			if sym.Kind != sema.SymParameter || deliberatelyUnused.MatchString(sym.Name) {
				continue
			}
			if !u.Read(sym) {
				c.Report(sym.Pos(), "parameter %q is never used", sym.Name)
			}
		}
	})
}
