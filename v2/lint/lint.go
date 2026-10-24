// Package lint runs style and correctness rules over an elaborated design and
// reports what they find as diag.Diagnostics.
//
// A rule is a function over the compiled and elaborated design that reports
// problems the language allows but a careful engineer would not write. Rules
// register themselves by name; the name is the diagnostic's code, so it is also
// what a configuration file turns off and what a suppression comment names:
//
//	logic x;  // svparse: disable=unused-signal
//
// The framework supplies the rest: configuration (severity per rule, files to
// skip), suppression comments, a stable order, and isolation of a failing rule
// from the others.
package lint

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

// Rule is one lint check.
type Rule struct {
	// Name is the kebab-case code of its diagnostics.
	Name string
	// Doc is a one-line description.
	Doc string
	// Severity is the default severity; configuration can change it.
	Severity diag.Severity
	// Check inspects the design and reports through c.
	Check func(c *Context)
}

var (
	mu    sync.Mutex
	rules = map[string]Rule{}
)

// Register adds a rule. It is meant to be called from a package's init, and
// panics on a malformed or duplicate rule, which is a programming error rather
// than bad input. The rule's name becomes a registered diagnostic code.
func Register(r Rule) {
	mu.Lock()
	defer mu.Unlock()
	if !diag.ValidCode(r.Name) {
		panic(fmt.Sprintf("lint: rule name %q is not kebab-case", r.Name))
	}
	if r.Doc == "" || r.Check == nil {
		panic(fmt.Sprintf("lint: rule %q needs a Doc and a Check", r.Name))
	}
	if _, dup := rules[r.Name]; dup {
		panic(fmt.Sprintf("lint: rule %q is registered twice", r.Name))
	}
	if _, taken := diag.Known[r.Name]; taken {
		panic(fmt.Sprintf("lint: rule name %q collides with an existing diagnostic code", r.Name))
	}
	rules[r.Name] = r
	diag.Known[r.Name] = r.Doc
}

// Rules returns the registered rules sorted by name.
func Rules() []Rule {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup returns the rule with the given name.
func Lookup(name string) (Rule, bool) {
	mu.Lock()
	defer mu.Unlock()
	r, ok := rules[name]
	return r, ok
}

// Input is what the rules look at.
type Input struct {
	// Comp is the compiled design (required).
	Comp *sema.Compilation
	// Design is its elaboration; nil if the caller did not elaborate. A rule
	// that needs the instance tree does nothing without it.
	Design *elab.Design
	// Comments are the comments of every source read, for suppression
	// directives (preprocessor.Options.KeepComments).
	Comments []preprocessor.Comment
	// Files, if not nil, limits the run to findings in the files whose path is
	// a key: an editor that has changed one file of a design of thousands
	// needs that file's findings, not everyone's. The rules still see the
	// whole design (an instance's parameters come from its parent), but skip
	// the work of looking for findings elsewhere, and any finding outside the
	// files is dropped. Findings in other files that the change affects are not
	// refreshed; a run without Files does that.
	Files map[string]bool
}

// Context is what a rule's Check gets: the design, and the way to report.
type Context struct {
	Comp   *sema.Compilation
	Model  *sema.Model
	Design *elab.Design

	ctx   context.Context
	scope map[string]bool // Input.Files
	rule  *Rule
	out   *[]diag.Diagnostic
	sh    *shared
}

// shared holds analyses several rules need, computed once per run.
type shared struct {
	in     Input
	usage  *Usage
	widths *widthFacts
}

// Usage returns which signals, ports and parameters of the design are read and
// written (computed on first use).
func (c *Context) Usage() *Usage {
	if c.sh.usage == nil {
		c.sh.usage = buildUsage(c.Comp, c.Model, c.Design, c.scope)
	}
	return c.sh.usage
}

// InScope reports whether findings in the file are wanted: always, unless the
// run was limited to some files (Input.Files).
func (c *Context) InScope(file string) bool {
	return c.scope == nil || file == "" || c.scope[file]
}

// Files returns the files of the compilation that the run covers.
func (c *Context) Files() []*ast.File {
	all := c.Comp.Files()
	if c.scope == nil {
		return all
	}
	out := all[:0:0]
	for _, f := range all {
		if c.scope[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

// bodyWanted reports whether the body of an elaborated node, the source of the
// findings a rule makes about its contents, is in a covered file.
func (c *Context) bodyWanted(n *elab.Node) bool {
	if c.scope == nil {
		return true
	}
	if _, ok := n.Decl.(*ast.GenerateBlock); ok {
		return c.InScope(n.Decl.Pos().File)
	}
	if n.Def != nil && n.Def.Decl != nil {
		return c.InScope(n.Def.Decl.Pos().File)
	}
	return true
}

// siteWanted reports whether the place an elaborated node is written, its
// instantiation, is in a covered file: where findings about how it is
// connected go.
func (c *Context) siteWanted(n *elab.Node) bool {
	if c.scope == nil || n.Decl == nil {
		return true
	}
	return c.InScope(n.Decl.Pos().File)
}

// Cancelled reports whether the run was cancelled; a long rule should stop.
func (c *Context) Cancelled() bool { return c.ctx != nil && c.ctx.Err() != nil }

// Report records a finding at the given place.
func (c *Context) Report(at ast.Position, format string, args ...any) {
	c.ReportRelated(at, nil, format, args...)
}

// ReportRelated records a finding with other places that explain it.
func (c *Context) ReportRelated(at ast.Position, related []diag.Related, format string, args ...any) {
	if !c.InScope(at.File) {
		return
	}
	*c.out = append(*c.out, diag.Diagnostic{
		Code:     c.rule.Name,
		Severity: c.rule.Severity,
		Range:    at,
		Message:  fmt.Sprintf(format, args...),
		Related:  related,
	})
}

// Run applies the enabled rules to the design and returns their findings,
// filtered by cfg and by suppression comments, sorted by position. A rule that
// panics is reported as an internal-error diagnostic and does not stop the
// others. If ctx is cancelled the findings so far are returned.
func Run(ctx context.Context, in Input, cfg Config) []diag.Diagnostic {
	if in.Comp == nil {
		return nil
	}
	sup := ParseSuppressions(in.Comments)
	ignore := cfg.ignoreMatcher()
	model := in.Comp.Model()

	sh := &shared{in: in}
	var set diag.Set
	for _, r := range Rules() {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		level, off := cfg.level(r)
		if off {
			continue
		}
		r.Severity = level
		var found []diag.Diagnostic
		c := &Context{Comp: in.Comp, Model: model, Design: in.Design, ctx: ctx, scope: in.Files, rule: &r, out: &found, sh: sh}
		runRule(&r, c, &found)
		for _, d := range found {
			if ignore(d.Range.File) || sup.Suppressed(d.Code, d.Range.File, d.Range.Line) {
				continue
			}
			set.Add(d)
		}
	}
	return set.Sorted()
}

// runRule runs one rule, turning a panic into a diagnostic.
func runRule(r *Rule, c *Context, found *[]diag.Diagnostic) {
	defer func() {
		if p := recover(); p != nil {
			*found = append(*found, diag.Diagnostic{
				Code:     diag.CodeInternalError,
				Severity: diag.Error,
				Message:  fmt.Sprintf("lint rule %q failed: %v", r.Name, p),
			})
		}
	}()
	r.Check(c)
}
