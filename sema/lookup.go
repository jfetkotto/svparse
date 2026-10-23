package sema

import (
	"strings"

	"github.com/jfetkotto/svparse/ast"
)

// Result is the outcome of a name lookup.
type Result struct {
	// Symbol is the declaration found, or nil.
	Symbol *Symbol
	// Via names the package a symbol was imported from, "" for a name found
	// in the scope chain itself.
	Via string
	// Ambiguous lists the candidates when two wildcard imports supply the name
	// (LRM 26.3): Symbol is then nil.
	Ambiguous []*Symbol
}

// Found reports whether the lookup found exactly one symbol.
func (r Result) Found() bool { return r.Symbol != nil }

// maxDepth bounds every chain a lookup follows (imports, base classes,
// typedefs), so a cyclic program cannot loop.
const maxDepth = 64

// Lookup resolves an unqualified name as seen from s, following LRM 23.9: the
// scope's own declarations, then its explicit imports, then its wildcard
// imports, then (for a class) the members its base classes inherit, then the
// enclosing scope, out to $unit. Names a module or package reaches by wildcard
// import are found only if no declaration of the same name is in scope.
func (s *Scope) Lookup(name string) Result {
	for sc, n := s, 0; sc != nil && n < 10000; sc, n = sc.Parent, n+1 {
		if r, ok := sc.lookupHere(name); ok {
			return r
		}
	}
	return Result{}
}

func (s *Scope) lookupHere(name string) (Result, bool) {
	if sym, ok := s.syms[name]; ok {
		return Result{Symbol: sym}, true
	}
	if pkg, ok := s.explicit[name]; ok {
		if sym, ok := s.model.packageMember(pkg, name, nil); ok {
			return Result{Symbol: sym, Via: pkg}, true
		}
	}
	var found []*Symbol
	via := ""
	for _, pkg := range s.wildcards {
		if sym, ok := s.model.packageMember(pkg, name, nil); ok {
			dup := false
			for _, f := range found {
				dup = dup || f == sym
			}
			if !dup {
				found = append(found, sym)
				via = pkg
			}
		}
	}
	switch len(found) {
	case 1:
		return Result{Symbol: found[0], Via: via}, true
	case 0:
	default:
		return Result{Ambiguous: found}, true
	}
	if s.Kind == ScopeClass {
		if sym, ok := s.model.inheritedMember(s, name, 0); ok {
			return Result{Symbol: sym}, true
		}
	}
	return Result{}, false
}

// packageMember finds name as a package user sees it with pkg::name: a
// declaration of the package, or a name it exports from another package (LRM
// 26.6). Names a package merely imports are not visible through it.
func (m *Model) packageMember(pkg, name string, seen map[string]bool) (*Symbol, bool) {
	if seen[pkg] || len(seen) > maxDepth {
		return nil, false
	}
	def, ok := m.comp.Package(pkg)
	if !ok {
		return nil, false
	}
	sc, ok := m.defs[def]
	if !ok {
		return nil, false
	}
	if sym, ok := sc.syms[name]; ok {
		return sym, true
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	seen[pkg] = true
	for _, e := range sc.exports {
		if e.name != "*" && e.name != name {
			continue
		}
		if e.pkg == "*" {
			// "export *::*": anything the package itself imports.
			if sym, ok := sc.importedMember(name, seen); ok {
				return sym, true
			}
			continue
		}
		if sym, ok := m.packageMember(e.pkg, name, seen); ok {
			return sym, true
		}
	}
	return nil, false
}

// importedMember resolves name through the scope's own imports only.
func (s *Scope) importedMember(name string, seen map[string]bool) (*Symbol, bool) {
	if pkg, ok := s.explicit[name]; ok {
		if sym, ok := s.model.packageMember(pkg, name, seen); ok {
			return sym, true
		}
	}
	for _, pkg := range s.wildcards {
		if sym, ok := s.model.packageMember(pkg, name, seen); ok {
			return sym, true
		}
	}
	return nil, false
}

// inheritedMember finds name among the members the class scope inherits from
// its base classes.
func (m *Model) inheritedMember(cs *Scope, name string, depth int) (*Symbol, bool) {
	if depth > maxDepth {
		return nil, false
	}
	super := m.superScope(cs)
	if super == nil {
		return nil, false
	}
	if sym, ok := super.syms[name]; ok {
		return sym, true
	}
	return m.inheritedMember(super, name, depth+1)
}

// superScope returns the scope of the base class of the class cs opens, or nil
// if there is none or it cannot be resolved (a type parameter, an unknown name).
func (m *Model) superScope(cs *Scope) *Scope {
	cl, ok := cs.Node.(*ast.Class)
	if !ok || cl.Extends == "" || cs.Parent == nil {
		return nil
	}
	var path []string
	if len(cl.ExtendsTypes) > 0 {
		t := cl.ExtendsTypes[0]
		if t.PackageQualifier != "" {
			path = append(path, strings.Split(t.PackageQualifier, "::")...)
		}
		path = append(path, t.Name)
	} else {
		path = []string{cl.Extends}
	}
	var res Result
	if len(path) == 1 {
		res = cs.Parent.Lookup(path[0])
	} else {
		res = cs.Parent.LookupQualified(path)
	}
	if sym := m.classOf(res.Symbol, 0); sym != nil && sym.Child != cs {
		return sym.Child
	}
	return nil
}

// classOf follows typedefs to the class symbol a symbol stands for, or nil.
func (m *Model) classOf(sym *Symbol, depth int) *Symbol {
	if sym == nil || depth > maxDepth {
		return nil
	}
	switch sym.Kind {
	case SymClass:
		return sym
	case SymTypedef:
		td, ok := sym.Decl.(*ast.Typedef)
		if !ok {
			return nil
		}
		alias, ok := td.Underlying.(*ast.TypeAlias)
		if !ok {
			return nil
		}
		t := alias.Type
		var res Result
		if t.PackageQualifier != "" {
			res = sym.Scope.LookupQualified(append(strings.Split(t.PackageQualifier, "::"), t.Name))
		} else {
			res = sym.Scope.Lookup(t.Name)
		}
		return m.classOf(res.Symbol, depth+1)
	}
	return nil
}

// LookupQualified resolves a scope-qualified name "a::b::name", given as
// []string{"a", "b", "name"} (LRM 26.3, 8.23). The first qualifier is searched
// as an ordinary name, so a class or typedef in scope hides a package of the
// same name; failing that it is a package, and "$unit" means the compilation
// unit. Each later qualifier, and the final name, is looked up among the members
// of the class before it, inherited ones included.
func (s *Scope) LookupQualified(path []string) Result {
	if len(path) < 2 || s == nil {
		return Result{}
	}
	m := s.model
	first := path[0]
	var cur *Scope // the class scope or package qualifying the next name
	pkg := ""
	switch {
	case first == "$unit":
		cur = s.unit()
	default:
		res := s.Lookup(first)
		if cs := m.classOf(res.Symbol, 0); cs != nil {
			cur = cs.Child
		} else if _, ok := m.comp.Package(first); ok && len(path) >= 2 {
			pkg = first
		} else {
			return Result{}
		}
	}
	rest := path[1:]
	if pkg != "" {
		if len(rest) == 1 {
			if sym, ok := m.packageMember(pkg, rest[0], nil); ok {
				return Result{Symbol: sym, Via: pkg}
			}
			return Result{}
		}
		// "pkg::C::x": the next qualifier must be a class declared in pkg.
		sym, ok := m.packageMember(pkg, rest[0], nil)
		cs := m.classOf(sym, 0)
		if !ok || cs == nil {
			return Result{}
		}
		cur, rest = cs.Child, rest[1:]
	}
	for i, name := range rest {
		if cur == nil {
			return Result{}
		}
		sym, ok := cur.syms[name]
		if !ok && cur.Kind == ScopeClass {
			sym, ok = m.inheritedMember(cur, name, 0)
		}
		if !ok {
			return Result{}
		}
		if i == len(rest)-1 {
			return Result{Symbol: sym}
		}
		cs := m.classOf(sym, 0)
		if cs == nil {
			return Result{}
		}
		cur = cs.Child
	}
	return Result{}
}

// unit returns the enclosing $unit scope.
func (s *Scope) unit() *Scope {
	for sc, n := s, 0; sc != nil && n < 10000; sc, n = sc.Parent, n+1 {
		if sc.Kind == ScopeUnit {
			return sc
		}
	}
	return nil
}

// LookupPath resolves a hierarchical name "a.b.c" (LRM 23.6, 23.8), given as
// []string{"a", "b", "c"}. The first component is found by the usual upward
// search, or as a top-level module ("top.x"); "$root" starts at the top.
// Following components are looked up in the scope the previous one opens: a
// module instance opens the scope of the module it instantiates, a named
// block or generate block its own. It returns the last symbol it could resolve
// and the components that remain: those after a variable or other symbol with
// no scope of its own, such as the field names of "s.a.b".
func (s *Scope) LookupPath(path []string) (*Symbol, []string) {
	if s == nil || len(path) == 0 {
		return nil, path
	}
	m := s.model
	var sym *Symbol
	i := 0
	if path[0] == "$root" {
		if len(path) < 2 {
			return nil, path
		}
		i = 1
	}
	if path[0] != "$root" {
		sym = s.Lookup(path[0]).Symbol
	}
	if sym == nil {
		def, ok := m.comp.Lookup(path[i])
		if !ok {
			return nil, path[i:]
		}
		sc, ok := m.defs[def]
		if !ok {
			return nil, path[i:]
		}
		sym = &Symbol{Name: def.Name, Kind: SymModule, Decl: def.Decl, Scope: sc.Parent, Child: sc}
		switch def.Kind {
		case DefInterface:
			sym.Kind = SymInterface
		case DefProgram:
			sym.Kind = SymProgram
		}
	}
	i++
	for ; i < len(path); i++ {
		child := m.scopeOfSymbol(sym)
		if child == nil {
			return sym, path[i:]
		}
		next, ok := child.syms[path[i]]
		if !ok {
			return sym, path[i:]
		}
		sym = next
	}
	return sym, nil
}

// scopeOfSymbol is the scope a symbol opens. An instance opens the scope of
// the design element it instantiates.
func (m *Model) scopeOfSymbol(sym *Symbol) *Scope {
	if sym.Child != nil {
		return sym.Child
	}
	if sym.Kind == SymInstance {
		if def, ok := m.comp.Lookup(sym.Target); ok {
			return m.defs[def]
		}
	}
	return nil
}

// NameClass says what an identifier that could be a type or a value turned out
// to be.
type NameClass uint8

const (
	// NameUnknown: the name is not declared.
	NameUnknown NameClass = iota
	// NameType: it names a data type.
	NameType
	// NameValue: it names a variable, net, parameter, function or other value.
	NameValue
)

// ClassifyName resolves the parser's type-or-expression ambiguity (the
// *ast.TypeOrExpr of "$bits(T)", "#(T)" and "T'(x)"): a name is a type if it
// resolves to a typedef, class, type parameter or nettype, and a value
// otherwise. It accepts a plain or package/class-qualified name; any other
// expression is a value.
func (s *Scope) ClassifyName(e ast.Expr) NameClass {
	var res Result
	switch e := e.(type) {
	case *ast.TypeOrExpr:
		return s.ClassifyName(e.X)
	case *ast.TypeExpr:
		return NameType
	case *ast.Ident:
		res = s.Lookup(e.Name)
	case *ast.ScopedName:
		path, ok := scopedPath(e)
		if !ok {
			return NameValue
		}
		res = s.LookupQualified(path)
	case *ast.ParameterizedName:
		if e.Scope == nil {
			res = s.Lookup(e.Name)
			break
		}
		path, ok := scopedPath(&ast.ScopedName{Scope: e.Scope, Name: e.Name})
		if !ok {
			return NameValue
		}
		res = s.LookupQualified(path)
	default:
		return NameValue
	}
	switch {
	case res.Symbol == nil:
		return NameUnknown
	case res.Symbol.Kind.IsType():
		return NameType
	}
	return NameValue
}

// scopedPath flattens "a::b::c" into its names.
func scopedPath(e *ast.ScopedName) ([]string, bool) {
	var path []string
	var walk func(ast.Expr) bool
	walk = func(x ast.Expr) bool {
		switch x := x.(type) {
		case *ast.Ident:
			path = append(path, x.Name)
			return true
		case *ast.ParameterizedName:
			if x.Scope != nil && !walk(x.Scope) {
				return false
			}
			path = append(path, x.Name)
			return true
		case *ast.ScopedName:
			return walk(x.Scope) && func() bool { path = append(path, x.Name); return true }()
		}
		return false
	}
	if !walk(e) {
		return nil, false
	}
	return path, true
}
