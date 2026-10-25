// Package elab elaborates a compiled design (LRM 23.10, 27): it picks the top
// modules, instantiates the hierarchy below them, resolves every instance's
// parameters from overrides, defparams and defaults, and unrolls generate
// constructs, producing the tree of instances and generate blocks a design
// really contains.
package elab

import (
	"context"
	"fmt"
	"strings"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/sema"
)

// Options configure Elaborate.
type Options struct {
	// Tops names the modules, interfaces or programs to elaborate as roots. If
	// empty, the compilation's uninstantiated definitions are used.
	Tops []string
	// MaxDepth bounds the depth of the instance hierarchy; the default is 128.
	MaxDepth int
	// MaxInstances bounds the number of instances and generate blocks; the
	// default is one million.
	MaxInstances int
	// MaxGenerateIters bounds the iterations of one generate loop; the default
	// is 65536.
	MaxGenerateIters int
	// MaxSteps is the constant-evaluation step budget of each instance; the
	// default is the evaluator's own.
	MaxSteps int
	// DefaultNettype is the type of an implicitly declared net (LRM 6.10):
	// "wire" (the default) or "none", which makes an undeclared name in a port
	// connection or on the left of a continuous assignment an error. The
	// preprocessor does not carry `default_nettype through, so one setting
	// covers the whole design.
	DefaultNettype string
}

// NodeKind says what a Node is.
type NodeKind uint8

const (
	// KindInstance is an instance of a module, interface or program.
	KindInstance NodeKind = iota
	// KindBlock is a generate block: one iteration of a loop, a selected
	// branch of an if or case, or a bare begin/end.
	KindBlock
)

// Param is one parameter of a node with the value it ended up with.
type Param struct {
	Name string
	Sym  *sema.Symbol
	// Local is true for a localparam, and for a parameter of a module whose
	// header has a parameter port list (LRM 6.20.1), neither of which can be
	// overridden.
	Local bool
	// IsType is true for "parameter type T". Its value is TypeValue.
	IsType     bool
	Value      sema.Const
	TypeValue  sema.Type
	Overridden bool
	// Valid is false if the value could not be computed (a diagnostic says why).
	Valid bool
}

// Node is an element of the elaborated hierarchy: an instance or a generate
// block, with the instances and blocks it contains.
type Node struct {
	Kind NodeKind
	// Name is the instance name, with its index for an array element
	// ("u[2]"), or the block name ("g", "genblk1"), with the loop index for a
	// loop iteration ("g[3]").
	Name     string
	Index    int
	HasIndex bool
	Parent   *Node
	// Def is the definition of an instance; nil for a block.
	Def *sema.Definition
	// Scope is the scope the node's names are declared in: the definition's for
	// an instance, the block's for a block.
	Scope  *sema.Scope
	Env    *sema.Env
	Params []Param
	// Children are the instances and blocks directly inside, in source order
	// (loop iterations in iteration order).
	Children []*Node
	// Ports binds each port of the instantiated module to what the instance
	// connects to it, in port order. It is empty for a top and for a block.
	Ports []PortBinding
	// ImplicitNets are the nets this node's scope declares implicitly, by being
	// connected to an instance port or driven by a continuous assignment.
	ImplicitNets []ImplicitNet
	// Decl is the *ast.Instance or *ast.GenerateBlock this node comes from;
	// nil for a top.
	Decl ast.Node

	defparams []defparam
}

// Path is the hierarchical name from the top, such as "top.g[1].u".
func (n *Node) Path() string {
	if n == nil {
		return ""
	}
	if n.Parent == nil {
		return n.Name
	}
	return n.Parent.Path() + "." + n.Name
}

// Param returns the parameter with the given name.
func (n *Node) Param(name string) (Param, bool) {
	for _, p := range n.Params {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

// Child returns the direct child with the given name.
func (n *Node) Child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Find follows a dotted path of child names from n.
func (n *Node) Find(path string) *Node {
	cur := n
	if path == "" {
		return n
	}
	for _, part := range splitPath(path) {
		if cur = cur.Child(part); cur == nil {
			return nil
		}
	}
	return cur
}

// splitPath splits on dots outside brackets, so "g[1].u" has two parts.
func splitPath(p string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				parts = append(parts, p[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, p[start:])
}

// Walk calls f for n and every node below it, parents first, stopping the
// descent below a node when f returns false.
func (n *Node) Walk(f func(*Node) bool) {
	if n == nil || !f(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(f)
	}
}

// Design is the result of elaboration.
type Design struct {
	Comp *sema.Compilation
	Tops []*Node
	// Count is the number of nodes (instances and generate blocks).
	Count int
}

// Find returns the node at a path starting with a top's name, such as
// "top.u1.g[0]".
func (d *Design) Find(path string) *Node {
	parts := splitPath(path)
	for _, t := range d.Tops {
		if t.Name == parts[0] {
			return t.Find(strings.Join(parts[1:], "."))
		}
	}
	return nil
}

// Walk calls f for every node of every top, parents first.
func (d *Design) Walk(f func(*Node) bool) {
	for _, t := range d.Tops {
		t.Walk(f)
	}
}

type elaborator struct {
	ctx   context.Context
	comp  *sema.Compilation
	model *sema.Model
	opts  Options
	set   diag.Set
	count int
	stop  bool
}

// Elaborate builds the instance hierarchy of comp. Problems are returned as
// diagnostics, sorted by position: those from evaluating parameters and
// generate conditions (not-constant and the like) together with the
// elaboration errors in diag.Known (unknown-module, unknown-parameter,
// recursive-instantiation, instance-limit, ...). The design holds whatever
// could be elaborated even when there are errors. If ctx is cancelled,
// elaboration stops at the next instance and the design so far is returned;
// the caller can see this from ctx.Err().
func Elaborate(ctx context.Context, comp *sema.Compilation, opts Options) (*Design, []diag.Diagnostic) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 128
	}
	if opts.MaxInstances <= 0 {
		opts.MaxInstances = 1_000_000
	}
	if opts.MaxGenerateIters <= 0 {
		opts.MaxGenerateIters = 65536
	}
	e := &elaborator{ctx: ctx, comp: comp, model: comp.Model(), opts: opts}
	d := &Design{Comp: comp}

	var tops []*sema.Definition
	if len(opts.Tops) > 0 {
		for _, name := range opts.Tops {
			def, ok := comp.Lookup(name)
			if !ok || def.Kind == sema.DefUDP {
				e.set.Add(diag.Diagnostic{Code: diag.CodeUnknownTop, Severity: diag.Error, Message: fmt.Sprintf("top module %q is not defined", name)})
				continue
			}
			tops = append(tops, def)
		}
	} else {
		tops = comp.Tops()
		if len(tops) == 0 && len(comp.Definitions()) > 0 {
			e.set.Add(diag.Diagnostic{Code: diag.CodeNoTop, Severity: diag.Warning, Message: "the design has no top-level module"})
		}
	}
	for _, def := range tops {
		if e.cancelled() {
			break
		}
		d.Tops = append(d.Tops, e.instance(def, def.Name, nil, nil, nil, nil, nil))
	}
	e.applyBinds(d)
	e.checkPartSelects(d)
	d.Count = e.count
	return d, e.set.Sorted()
}

func (e *elaborator) cancelled() bool {
	if e.stop {
		return true
	}
	if e.ctx != nil && e.ctx.Err() != nil {
		e.stop = true
	}
	return e.stop
}

func (e *elaborator) errorf(code string, at ast.Position, format string, args ...any) {
	e.set.Add(diag.Diagnostic{Code: code, Severity: diag.Error, Range: at, Message: fmt.Sprintf(format, args...)})
}

// addAll records diagnostics from the semantic layer.
func (e *elaborator) addAll(ds []diag.Diagnostic) { e.set.AddAll(ds) }

// newEnv makes the evaluation environment of a node: it shares the overrides
// and starts with fresh caches, so a parameter is evaluated once per node, and
// copies the values of the enclosing generate variables.
func (e *elaborator) newEnv(params map[*sema.Symbol]sema.Const, vars map[*sema.Symbol]sema.Const) *sema.Env {
	env := &sema.Env{Params: params, MaxSteps: e.opts.MaxSteps}
	if len(vars) > 0 {
		env.Vars = make(map[*sema.Symbol]sema.Const, len(vars)+1)
		for k, v := range vars {
			env.Vars[k] = v
		}
	}
	return env
}

// reserve counts a new node against the limit. It reports false, after a
// diagnostic, once the limit is exceeded.
func (e *elaborator) reserve(at ast.Position) bool {
	e.count++
	if e.count > e.opts.MaxInstances {
		if !e.stop {
			e.errorf(diag.CodeInstanceLimit, at, "the design has more than %d instances and generate blocks", e.opts.MaxInstances)
		}
		e.stop = true
		return false
	}
	return true
}
