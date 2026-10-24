package sema

import (
	"fmt"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// DefKind says what a top-level Definition is.
type DefKind uint8

const (
	DefModule DefKind = iota
	DefInterface
	DefProgram
	DefUDP
	DefPackage
	DefConfig
)

func (k DefKind) String() string {
	return [...]string{"module", "interface", "program", "primitive", "package", "configuration"}[k%6]
}

// Definition is a top-level design element or package.
type Definition struct {
	Kind DefKind
	Name string
	Decl ast.Decl
	File string
}

// Pos is the position of the definition's name.
func (d *Definition) Pos() ast.Position { return d.Decl.Pos() }

// Options configure a Compilation.
type Options struct {
	// SingleUnit makes all files share one $unit scope, as the -mfcu option
	// of simulators does (LRM 3.12.1). By default each file is its own
	// compilation unit.
	SingleUnit bool
}

// Unit is a compilation-unit scope, "$unit" (LRM 3.12.1): the names declared
// outside any module, interface, program, package or other design element.
type Unit struct {
	// Name is "$unit" for a shared unit, or the file path of a per-file unit.
	Name  string
	Files []string

	members map[string]ast.Decl
	order   []string
}

// Lookup returns the declaration of name in the unit, if any.
func (u *Unit) Lookup(name string) (ast.Decl, bool) {
	if u == nil {
		return nil, false
	}
	d, ok := u.members[name]
	return d, ok
}

// Names lists the names declared in the unit, in declaration order.
func (u *Unit) Names() []string {
	if u == nil {
		return nil
	}
	return append([]string(nil), u.order...)
}

// Compilation is a set of parsed files treated as one design: its design
// elements, packages and compilation units. Adding or removing a file
// invalidates what was derived, and the next query recomputes it. A
// Compilation is not safe for concurrent use.
type Compilation struct {
	opts  Options
	files []*ast.File
	built *index
	model *Model
}

// index is everything derived from the files.
type index struct {
	defs         []*Definition
	design       map[string]*Definition // modules, interfaces, programs, primitives
	packages     map[string]*Definition
	configs      map[string]*Definition
	units        []*Unit
	unitOf       map[string]*Unit
	instantiated map[string]bool
	diags        []diag.Diagnostic
}

// NewCompilation returns an empty Compilation.
func NewCompilation(opts Options) *Compilation { return &Compilation{opts: opts} }

// AddFile adds f to the compilation. A file with the same Path as one already
// added replaces it, which is how an editor re-adds a file after a change. A
// nil file is ignored.
func (c *Compilation) AddFile(f *ast.File) {
	if f == nil {
		return
	}
	c.built, c.model = nil, nil
	for i, old := range c.files {
		if old.Path == f.Path {
			c.files[i] = f
			return
		}
	}
	c.files = append(c.files, f)
}

// RemoveFile drops the file with the given path, if present.
func (c *Compilation) RemoveFile(path string) {
	for i, f := range c.files {
		if f.Path == path {
			c.files = append(c.files[:i:i], c.files[i+1:]...)
			c.built, c.model = nil, nil
			return
		}
	}
}

// Files returns the files in the order they were added.
func (c *Compilation) Files() []*ast.File { return append([]*ast.File(nil), c.files...) }

// Definitions returns every module, interface, program, primitive, package and
// configuration, in file and source order. A name defined twice appears twice.
func (c *Compilation) Definitions() []*Definition {
	return append([]*Definition(nil), c.index().defs...)
}

// Lookup finds a module, interface, program or primitive by name, the
// "definitions name space" of LRM 3.13. If the name is defined more than once
// the first definition wins.
func (c *Compilation) Lookup(name string) (*Definition, bool) {
	d, ok := c.index().design[name]
	return d, ok
}

// Package finds a package by name (the package name space).
func (c *Compilation) Package(name string) (*Definition, bool) {
	d, ok := c.index().packages[name]
	return d, ok
}

// Config finds a configuration by name.
func (c *Compilation) Config(name string) (*Definition, bool) {
	d, ok := c.index().configs[name]
	return d, ok
}

// Units returns the compilation units.
func (c *Compilation) Units() []*Unit { return append([]*Unit(nil), c.index().units...) }

// Unit returns the $unit scope that contains the file with the given path.
func (c *Compilation) Unit(path string) (*Unit, bool) {
	u, ok := c.index().unitOf[path]
	return u, ok
}

// Tops returns the modules, interfaces and programs that nothing instantiates
// or binds: the roots of the design hierarchy (LRM 23.3.2).
func (c *Compilation) Tops() []*Definition {
	idx := c.index()
	var out []*Definition
	for _, d := range idx.defs {
		switch d.Kind {
		case DefModule, DefInterface, DefProgram:
			if !idx.instantiated[d.Name] && idx.design[d.Name] == d {
				out = append(out, d)
			}
		}
	}
	return out
}

// Diagnostics returns the problems found while building the compilation:
// names defined twice. They are sorted by position.
func (c *Compilation) Diagnostics() []diag.Diagnostic {
	return append([]diag.Diagnostic(nil), c.index().diags...)
}

func (c *Compilation) index() *index {
	if c.built == nil {
		c.built = c.build()
	}
	return c.built
}

func (c *Compilation) build() *index {
	idx := &index{
		design:       map[string]*Definition{},
		packages:     map[string]*Definition{},
		configs:      map[string]*Definition{},
		unitOf:       map[string]*Unit{},
		instantiated: map[string]bool{},
	}
	var shared *Unit
	if c.opts.SingleUnit {
		shared = &Unit{Name: "$unit", members: map[string]ast.Decl{}}
		idx.units = append(idx.units, shared)
	}
	var set diag.Set
	for _, f := range c.files {
		u := shared
		if u == nil {
			u = &Unit{Name: f.Path, members: map[string]ast.Decl{}}
			idx.units = append(idx.units, u)
		}
		u.Files = append(u.Files, f.Path)
		idx.unitOf[f.Path] = u

		for _, d := range f.Decls {
			if def := definitionOf(d, f.Path); def != nil {
				idx.add(def, &set)
				continue
			}
			for _, name := range declaredNames(d) {
				if prev, dup := u.members[name.name]; dup {
					set.Add(duplicate(diag.CodeDuplicateDeclaration, "declaration", name.name, name.decl, prev))
					continue
				}
				u.members[name.name] = name.decl
				u.order = append(u.order, name.name)
			}
		}
		// Walk reaches an instantiation wherever it sits: in a generate block,
		// or inside a bind, whose instantiated checker counts too. A bind
		// target is not instantiated by the bind.
		ast.Inspect(f, func(n ast.Node) bool {
			if inst, ok := n.(*ast.Instantiation); ok {
				idx.instantiated[inst.ModuleType] = true
			}
			return true
		})
	}
	idx.diags = set.Sorted()
	return idx
}

// add records a definition, reporting a clash with an earlier one in the same
// name space.
func (idx *index) add(def *Definition, set *diag.Set) {
	idx.defs = append(idx.defs, def)
	space := idx.design
	switch def.Kind {
	case DefPackage:
		space = idx.packages
	case DefConfig:
		space = idx.configs
	}
	if prev, dup := space[def.Name]; dup {
		set.Add(duplicate(diag.CodeDuplicateDefinition, def.Kind.String(), def.Name, def.Decl, prev.Decl))
		return
	}
	space[def.Name] = def
}

func duplicate(code, what, name string, at, first ast.Decl) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     code,
		Severity: diag.Error,
		Range:    at.Pos(),
		Message:  fmt.Sprintf("%s %q is already defined", what, name),
		Related:  []diag.Related{{Range: first.Pos(), Message: "first defined here"}},
	}
}

// definitionOf reports whether d is a top-level design element or package.
func definitionOf(d ast.Decl, file string) *Definition {
	switch n := d.(type) {
	case *ast.Container:
		kind := DefModule
		switch n.ContainerKind {
		case ast.KindInterface:
			kind = DefInterface
		case ast.KindProgram:
			kind = DefProgram
		}
		return &Definition{Kind: kind, Name: n.Name, Decl: n, File: file}
	case *ast.Package:
		return &Definition{Kind: DefPackage, Name: n.Name, Decl: n, File: file}
	case *ast.UDP:
		return &Definition{Kind: DefUDP, Name: n.Name, Decl: n, File: file}
	case *ast.Config:
		return &Definition{Kind: DefConfig, Name: n.Name, Decl: n, File: file}
	}
	return nil
}

type declared struct {
	name string
	decl ast.Decl
}

// declaredNames lists the names a top-level declaration adds to the $unit
// scope. Enum constants are declared in the scope of their enum (LRM 6.19).
// Forward typedefs, prototypes and out-of-block method definitions declare
// nothing: they refer to a declaration made elsewhere.
func declaredNames(d ast.Decl) []declared {
	switch n := d.(type) {
	case *ast.Class:
		return []declared{{n.Name, n}}
	case *ast.Typedef:
		if n.ForwardKind != "" || n.Underlying == nil {
			return nil
		}
		out := []declared{{n.Name, n}}
		if e, ok := n.Underlying.(*ast.Enum); ok {
			out = append(out, enumConstants(e)...)
		}
		return out
	case *ast.Function:
		if n.Scope != "" || n.Prototype {
			return nil
		}
		return []declared{{n.Name, n}}
	case *ast.Task:
		if n.Scope != "" || n.Prototype {
			return nil
		}
		return []declared{{n.Name, n}}
	case *ast.Parameter:
		return []declared{{n.Name, n}}
	case *ast.Variable:
		out := []declared{{n.Name, n}}
		if e, ok := n.Type.Inline.(*ast.Enum); ok {
			out = append(out, enumConstants(e)...)
		}
		return out
	case *ast.Nettype:
		return []declared{{n.Name, n}}
	case *ast.Covergroup:
		return []declared{{n.Name, n}}
	case *ast.Let:
		return []declared{{n.Name, n}}
	case *ast.PropertyDecl:
		return []declared{{n.Name, n}}
	case *ast.SequenceDecl:
		return []declared{{n.Name, n}}
	}
	return nil
}

func enumConstants(e *ast.Enum) []declared {
	out := make([]declared, len(e.Members))
	for i := range e.Members {
		out[i] = declared{e.Members[i].Name, &e.Members[i]}
	}
	return out
}
