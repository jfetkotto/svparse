package sema

import (
	"fmt"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
)

// SymKind says what a Symbol names.
type SymKind uint8

const (
	SymModule SymKind = iota
	SymInterface
	SymProgram
	SymUDP
	SymClass
	SymTypedef
	SymTypeParam
	SymVariable
	SymNet
	SymGenvar
	SymParameter
	SymPort
	SymArg
	SymFunction
	SymTask
	SymInstance
	SymEnumMember
	SymGenerateBlock
	SymBlock
	SymLoopVar
	SymModport
	SymCovergroup
	SymCoverpoint
	SymProperty
	SymSequence
	SymLet
	SymNettype
	SymClocking
	SymClockingSignal
	SymConstraint
)

var symKindNames = [...]string{"module", "interface", "program", "primitive", "class", "typedef", "type parameter",
	"variable", "net", "genvar", "parameter", "port", "argument", "function", "task", "instance", "enum member",
	"generate block", "block", "loop variable", "modport", "covergroup", "coverpoint", "property", "sequence",
	"let", "nettype", "clocking block", "clocking signal", "constraint"}

func (k SymKind) String() string {
	if int(k) >= len(symKindNames) {
		return "?"
	}
	return symKindNames[k]
}

// IsType reports whether a symbol of this kind names a data type.
func (k SymKind) IsType() bool {
	switch k {
	case SymClass, SymTypedef, SymTypeParam, SymNettype:
		return true
	}
	return false
}

// Symbol is a named declaration in a scope.
type Symbol struct {
	Name string
	Kind SymKind
	// Decl is the declaring node: an ast.Decl, an *ast.Instance, or an
	// *ast.Arg. For a symbol that merges a port with its variable declaration
	// it is the port.
	Decl ast.Node
	// Scope is the scope the symbol is declared in.
	Scope *Scope
	// Child is the scope the symbol opens (a module, class, function, named
	// block, ...), or nil.
	Child *Scope
	// Owner is the *ast.Enum that declares an enum member.
	Owner ast.Node
	// Target is the module, interface or program type an instance symbol
	// instantiates.
	Target string
	// Redeclared is the variable or net that re-declares a port by the same
	// name ("output q; reg q;"), or nil.
	Redeclared ast.Node
}

// Pos is the position of the declaration's name.
func (s *Symbol) Pos() ast.Position { return s.Decl.Pos() }

// ScopeKind says what opens a Scope.
type ScopeKind uint8

const (
	ScopeUnit ScopeKind = iota
	ScopePackage
	ScopeModule
	ScopeInterface
	ScopeProgram
	ScopeClass
	ScopeSubroutine
	ScopeBlock
	ScopeGenerate
	ScopeCovergroup
	ScopeProperty
	ScopeClocking
)

// Scope is a region of source in which names are declared (LRM 3.13). Scopes
// nest; looking a name up walks outward through Parent.
type Scope struct {
	Kind   ScopeKind
	Name   string // "" for an unnamed scope
	Parent *Scope
	Node   ast.Node // the declaration or statement that opens it; nil for $unit
	Def    *Definition

	model     *Model
	syms      map[string]*Symbol
	order     []*Symbol
	wildcards []string          // packages imported with "import pkg::*"
	explicit  map[string]string // name -> package, from "import pkg::name"
	exports   []exportItem
	anon      int // unnamed generate blocks seen so far, for genblkN names
}

type exportItem struct{ pkg, name string }

// Symbols returns the scope's own symbols in declaration order.
func (s *Scope) Symbols() []*Symbol { return append([]*Symbol(nil), s.order...) }

// Own returns the symbol declared directly in the scope under name, ignoring
// imports and enclosing scopes.
func (s *Scope) Own(name string) (*Symbol, bool) {
	if s == nil {
		return nil, false
	}
	sym, ok := s.syms[name]
	return sym, ok
}

// Path is the hierarchical name of the scope, such as "top.g[0].u" without
// indices: names joined by dots, skipping $unit and unnamed scopes.
func (s *Scope) Path() string {
	if s == nil {
		return ""
	}
	p := s.Parent.Path()
	if s.Name == "" || s.Kind == ScopeUnit {
		return p
	}
	if p == "" {
		return s.Name
	}
	return p + "." + s.Name
}

// Model is the scope tree of a Compilation, built from its syntax trees.
type Model struct {
	comp    *Compilation
	units   map[*Unit]*Scope
	defs    map[*Definition]*Scope
	nodes   map[ast.Node]*Scope
	diags   []diag.Diagnostic
	pending []pendingMethod
	tc      *typeCache
	params  map[*Symbol]*paramInfo

	// boundExprs and synthetic cache what the constant function interpreter
	// binds, so loops bind their bodies once.
	boundExprs   map[boundKey]BoundExpr
	synthetic    map[ast.Node]ast.Expr // the 1 of ++ and --, by statement
	syntheticOps map[ast.Node]ast.Expr // "lhs op rhs" of a compound assignment, by statement
}

// pendingMethod is an out-of-block method definition "function C::m", whose
// scope nests in class C's scope once every class has been declared.
type pendingMethod struct {
	scope *Scope
	class string
}

// Model returns the scope tree of the compilation, building it on first use
// and again after files change.
func (c *Compilation) Model() *Model {
	if c.model == nil {
		c.model = buildModel(c)
	}
	return c.model
}

// UnitScope returns the $unit scope for the file with the given path.
func (m *Model) UnitScope(path string) (*Scope, bool) {
	u, ok := m.comp.Unit(path)
	if !ok {
		return nil, false
	}
	s, ok := m.units[u]
	return s, ok
}

// DefinitionScope returns the scope a module, interface, program or package
// opens.
func (m *Model) DefinitionScope(d *Definition) (*Scope, bool) {
	s, ok := m.defs[d]
	return s, ok
}

// ScopeOf returns the scope opened by a declaration or statement node: a
// container, package, class, function, task, named block, generate block,
// covergroup, property, sequence or clocking block.
func (m *Model) ScopeOf(n ast.Node) (*Scope, bool) {
	s, ok := m.nodes[n]
	return s, ok
}

// Diagnostics returns the duplicate-declaration problems found inside design
// elements, packages, classes and subroutines. Those among top-level names are
// reported by the Compilation.
func (m *Model) Diagnostics() []diag.Diagnostic {
	var set diag.Set
	set.AddAll(m.diags)
	return set.Sorted()
}

func buildModel(c *Compilation) *Model {
	idx := c.index()
	m := &Model{
		comp:  c,
		units: map[*Unit]*Scope{},
		defs:  map[*Definition]*Scope{},
		nodes: map[ast.Node]*Scope{},
	}
	// $unit scopes hold the unit-level declarations, which Compilation already
	// de-duplicated; each file's classes and subroutines open their own scopes.
	unitScopes := map[string]*Scope{}
	for _, u := range idx.units {
		us := &Scope{Kind: ScopeUnit, Name: "", model: m, syms: map[string]*Symbol{}}
		m.units[u] = us
		for _, p := range u.Files {
			unitScopes[p] = us
		}
	}
	for _, f := range c.files {
		us := unitScopes[f.Path]
		for _, d := range f.Decls {
			if definitionOf(d, f.Path) != nil {
				continue
			}
			m.declareIn(us, d)
		}
	}
	for _, def := range idx.defs {
		if idx.design[def.Name] != def && idx.packages[def.Name] != def && idx.configs[def.Name] != def {
			continue // a shadowed duplicate has no scope of its own
		}
		if def.Kind == DefConfig || def.Kind == DefUDP {
			continue
		}
		m.defs[def] = m.openDefinition(def, unitScopes[def.File])
	}
	m.resolvePending()
	return m
}

func (m *Model) openDefinition(def *Definition, unit *Scope) *Scope {
	switch n := def.Decl.(type) {
	case *ast.Container:
		kind := ScopeModule
		switch n.ContainerKind {
		case ast.KindInterface:
			kind = ScopeInterface
		case ast.KindProgram:
			kind = ScopeProgram
		}
		s := m.newScope(kind, n.Name, unit, n)
		s.Def = def
		m.fillContainer(s, n)
		return s
	case *ast.Package:
		s := m.newScope(ScopePackage, n.Name, unit, n)
		s.Def = def
		m.declareAll(s, n.Body)
		return s
	}
	return nil
}

func (m *Model) newScope(kind ScopeKind, name string, parent *Scope, node ast.Node) *Scope {
	s := &Scope{Kind: kind, Name: name, Parent: parent, Node: node, model: m, syms: map[string]*Symbol{}}
	if node != nil {
		m.nodes[node] = s
	}
	return s
}

func (m *Model) fillContainer(s *Scope, n *ast.Container) {
	for i := range n.Params {
		m.declare(s, paramKind(&n.Params[i]), n.Params[i].Name, &n.Params[i], nil)
	}
	for i := range n.Ports {
		m.declare(s, SymPort, n.Ports[i].Name, &n.Ports[i], nil)
	}
	m.declareAll(s, n.Body)
}

func paramKind(p *ast.Parameter) SymKind {
	if p.Type.Name == "type" {
		return SymTypeParam
	}
	return SymParameter
}

// declare adds a symbol to s, reporting a clash with an earlier one in the
// same scope. A variable or net that re-declares a port by the same name
// ("output q; reg q;", LRM 23.2.2.3) is folded into the port's symbol.
func (m *Model) declare(s *Scope, kind SymKind, name string, decl ast.Node, child *Scope) *Symbol {
	if name == "" {
		return nil
	}
	if prev, dup := s.syms[name]; dup {
		if prev.Kind == SymPort && prev.Redeclared == nil && (kind == SymVariable || kind == SymNet) {
			prev.Redeclared = decl
			return prev
		}
		m.diags = append(m.diags, diag.Diagnostic{
			Code:     diag.CodeDuplicateDeclaration,
			Severity: diag.Error,
			Range:    decl.Pos(),
			Message:  fmt.Sprintf("%s %q is already declared in this scope", kind, name),
			Related:  []diag.Related{{Range: prev.Decl.Pos(), Message: "first declared here"}},
		})
		return nil
	}
	sym := &Symbol{Name: name, Kind: kind, Decl: decl, Scope: s, Child: child}
	s.syms[name] = sym
	s.order = append(s.order, sym)
	return sym
}

func (m *Model) declareAll(s *Scope, decls []ast.Decl) {
	for _, d := range decls {
		m.declareIn(s, d)
	}
}

var netTypes = map[string]bool{
	"wire": true, "uwire": true, "tri": true, "triand": true, "trior": true, "tri0": true, "tri1": true,
	"wand": true, "wor": true, "trireg": true, "supply0": true, "supply1": true, "interconnect": true,
}

// declareIn adds the names one declaration contributes to scope s, opening
// child scopes for the declarations that have bodies.
func (m *Model) declareIn(s *Scope, d ast.Decl) {
	switch n := d.(type) {
	case *ast.Container: // a nested module, interface or program
		kind := SymModule
		skind := ScopeModule
		switch n.ContainerKind {
		case ast.KindInterface:
			kind, skind = SymInterface, ScopeInterface
		case ast.KindProgram:
			kind, skind = SymProgram, ScopeProgram
		}
		child := m.newScope(skind, n.Name, s, n)
		m.fillContainer(child, n)
		m.declare(s, kind, n.Name, n, child)
	case *ast.Class:
		child := m.newScope(ScopeClass, n.Name, s, n)
		for i := range n.Params {
			m.declare(child, paramKind(&n.Params[i]), n.Params[i].Name, &n.Params[i], nil)
		}
		m.declareAll(child, n.Body)
		m.declare(s, SymClass, n.Name, n, child)
	case *ast.Typedef:
		if n.ForwardKind != "" || n.Underlying == nil {
			return
		}
		m.declare(s, SymTypedef, n.Name, n, nil)
		if e, ok := n.Underlying.(*ast.Enum); ok {
			m.declareEnum(s, e)
		}
	case *ast.Variable:
		kind := SymVariable
		switch {
		case n.Type.Name == "genvar":
			kind = SymGenvar
		case netTypes[n.Type.Name]:
			kind = SymNet
		}
		m.declare(s, kind, n.Name, n, nil)
		if e, ok := n.Type.Inline.(*ast.Enum); ok {
			m.declareEnum(s, e)
		}
	case *ast.Parameter:
		m.declare(s, paramKind(n), n.Name, n, nil)
	case *ast.Nettype:
		m.declare(s, SymNettype, n.Name, n, nil)
	case *ast.Function:
		m.declareSubroutine(s, SymFunction, n, n.Name, n.Scope, n.Args, n.Decls, n.Stmts)
	case *ast.Task:
		m.declareSubroutine(s, SymTask, n, n.Name, n.Scope, n.Args, n.Decls, n.Stmts)
	case *ast.Constraint:
		if n.Scope == "" && !n.Prototype {
			m.declare(s, SymConstraint, n.Name, n, nil)
		}
	case *ast.Instantiation:
		for i := range n.Instances {
			if sym := m.declare(s, SymInstance, n.Instances[i].Name, &n.Instances[i], nil); sym != nil {
				sym.Target = n.ModuleType
			}
		}
	case *ast.Modport:
		m.declare(s, SymModport, n.Name, n, nil)
	case *ast.Import:
		if n.Member == "*" {
			s.wildcards = append(s.wildcards, n.Package)
		} else if n.Member != "" {
			if s.explicit == nil {
				s.explicit = map[string]string{}
			}
			if _, dup := s.explicit[n.Member]; !dup {
				s.explicit[n.Member] = n.Package
			}
		}
	case *ast.Export:
		s.exports = append(s.exports, exportItem{n.Package, n.Member})
	case *ast.Covergroup:
		child := m.newScope(ScopeCovergroup, n.Name, s, n)
		for i := range n.Ports {
			m.declare(child, SymArg, n.Ports[i].Name, &n.Ports[i], nil)
		}
		for i := range n.SamplePorts {
			m.declare(child, SymArg, n.SamplePorts[i].Name, &n.SamplePorts[i], nil)
		}
		for _, item := range n.Items {
			switch it := item.(type) {
			case *ast.CoverPoint:
				m.declare(child, SymCoverpoint, it.Name, it, nil)
			case *ast.CoverCross:
				m.declare(child, SymCoverpoint, it.Name, it, nil)
			}
		}
		m.declare(s, SymCovergroup, n.Name, n, child)
	case *ast.PropertyDecl:
		child := m.newScope(ScopeProperty, n.Name, s, n)
		m.declareArgs(child, n.Ports)
		m.declareAll(child, n.Decls)
		m.declare(s, SymProperty, n.Name, n, child)
	case *ast.SequenceDecl:
		child := m.newScope(ScopeProperty, n.Name, s, n)
		m.declareArgs(child, n.Ports)
		m.declareAll(child, n.Decls)
		m.declare(s, SymSequence, n.Name, n, child)
	case *ast.Let:
		m.declare(s, SymLet, n.Name, n, nil)
	case *ast.Clocking:
		child := m.newScope(ScopeClocking, n.Name, s, n)
		for _, it := range n.Items {
			for i := range it.Signals {
				m.declare(child, SymClockingSignal, it.Signals[i].Name, &it.Signals[i], nil)
			}
		}
		if n.Name != "" {
			m.declare(s, SymClocking, n.Name, n, child)
		}
	case *ast.GenerateRegion:
		m.declareAll(s, n.Body) // a region only brackets items
	case *ast.GenerateBlock:
		m.openGenerate(s, n, n.Label)
	case *ast.GenerateIf:
		m.declareIn(s, n.Then)
		if n.Else != nil {
			m.declareIn(s, n.Else)
		}
	case *ast.GenerateFor:
		m.openGenerateFor(s, n)
	case *ast.GenerateCase:
		for i := range n.Items {
			m.declareIn(s, n.Items[i].Body)
		}
	case *ast.Initial:
		m.scanStmt(s, n.Body)
	case *ast.Final:
		m.scanStmt(s, n.Body)
	case *ast.Always:
		m.scanStmt(s, n.Body)
	case *ast.AssertionItem:
		m.scanStmt(s, n.Body)
	}
}

func (m *Model) declareEnum(s *Scope, e *ast.Enum) {
	for i := range e.Members {
		mem := &e.Members[i]
		// "enum {A, B} x, y;" gives each declarator its own copy of the enum,
		// at the same source position: the second copy is not a clash.
		if prev, ok := s.syms[mem.Name]; ok && prev.Kind == SymEnumMember && prev.Decl.Pos() == mem.Pos() {
			continue
		}
		if sym := m.declare(s, SymEnumMember, mem.Name, mem, nil); sym != nil {
			sym.Owner = e
		}
	}
}

func (m *Model) declareArgs(s *Scope, args []ast.Arg) {
	for i := range args {
		m.declare(s, SymArg, args[i].Name, &args[i], nil)
	}
}

// declareSubroutine opens a function or task scope. A definition qualified with
// a class ("C::m") is not a member name of the enclosing scope; its scope nests
// in the class once classes are known.
func (m *Model) declareSubroutine(s *Scope, kind SymKind, n ast.Decl, name, class string, args []ast.Arg, decls []ast.Decl, stmts []ast.Stmt) {
	child := m.newScope(ScopeSubroutine, name, s, n)
	m.declareArgs(child, args)
	m.declareAll(child, decls)
	for _, st := range stmts {
		m.scanStmt(child, st)
	}
	if class != "" {
		m.pending = append(m.pending, pendingMethod{child, class})
		return
	}
	// A prototype ("extern", DPI) names the member too; its body, if any, is
	// declared elsewhere.
	m.declare(s, kind, name, n, child)
}

// openGenerate opens the scope of a generate block. An unnamed block is called
// genblkN, counting the unnamed blocks of the enclosing scope (LRM 27.6).
func (m *Model) openGenerate(s *Scope, b *ast.GenerateBlock, label string) *Scope {
	name := label
	if name == "" {
		s.anon++
		name = fmt.Sprintf("genblk%d", s.anon)
	}
	child := m.newScope(ScopeGenerate, name, s, b)
	m.declareAll(child, b.Body)
	m.declare(s, SymGenerateBlock, name, b, child)
	return child
}

func (m *Model) openGenerateFor(s *Scope, n *ast.GenerateFor) {
	body := n.Body
	name := body.Label
	if name == "" {
		s.anon++
		name = fmt.Sprintf("genblk%d", s.anon)
	}
	child := m.newScope(ScopeGenerate, name, s, body)
	if n.Genvar && n.Var != "" {
		// "for (genvar i = ...)" declares i in the loop's scope.
		m.declare(child, SymGenvar, n.Var, n, nil)
	}
	m.declareAll(child, body.Body)
	m.declare(s, SymGenerateBlock, name, body, child)
}

// scanStmt opens scopes for the blocks and loops inside a statement.
func (m *Model) scanStmt(s *Scope, st ast.Stmt) {
	if st == nil {
		return
	}
	ast.Walk(&stmtScanner{m: m, scope: s}, st)
}

type stmtScanner struct {
	m     *Model
	scope *Scope
}

func (v *stmtScanner) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.Block:
		if n.Label == "" && len(n.Decls) == 0 {
			return v
		}
		child := v.m.newScope(ScopeBlock, n.Label, v.scope, n)
		v.m.declareAll(child, n.Decls)
		if n.Label != "" {
			v.m.declare(v.scope, SymBlock, n.Label, n, child)
		}
		return &stmtScanner{m: v.m, scope: child}
	case *ast.For:
		if len(n.InitDecls) == 0 {
			return v
		}
		child := v.m.newScope(ScopeBlock, "", v.scope, n)
		v.m.declareAll(child, n.InitDecls)
		return &stmtScanner{m: v.m, scope: child}
	case *ast.Foreach:
		child := v.m.newScope(ScopeBlock, "", v.scope, n)
		for _, name := range n.Vars {
			if name != "" {
				v.m.declare(child, SymLoopVar, name, n, nil)
			}
		}
		return &stmtScanner{m: v.m, scope: child}
	}
	return v
}

// resolvePending nests each out-of-block method in its class's scope.
func (m *Model) resolvePending() {
	for _, p := range m.pending {
		res := p.scope.Parent.Lookup(p.class)
		if res.Symbol != nil && res.Symbol.Child != nil && res.Symbol.Kind == SymClass {
			p.scope.Parent = res.Symbol.Child
		}
	}
	m.pending = nil
}
