package lint

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
)

func init() {
	Register(Rule{
		Name:     "unconnected-input",
		Doc:      "an instance leaves an input port out of its connection list, or connects it to nothing with .port(): the input floats (LRM 23.3.2)",
		Severity: diag.Error,
		Check:    func(c *Context) { checkUnconnected(c, true) },
	})
	Register(Rule{
		Name:     "unconnected-output",
		Doc:      "an instance leaves an output port out of its connection list; write .port() to leave one open on purpose (LRM 23.3.2)",
		Severity: diag.Warning,
		Check:    func(c *Context) { checkUnconnected(c, false) },
	})
}

// checkUnconnected reports the ports an instantiation does not connect.
//
// An input that is left out, or written ".d()", floats, which is almost never
// intended, so inputs are reported either way. An output has a legitimate way
// to be left unused, ".q()", so only an output that is not mentioned at all is
// reported: that is the mistake of forgetting it. A port that ".*" or ".p"
// connects is connected. An instance in a loop or an array is elaborated many
// times but written once, so each finding is reported once per source
// position.
func checkUnconnected(c *Context, inputs bool) {
	if c.Design == nil {
		return
	}
	type key struct {
		at   ast.Position
		port string
	}
	seen := map[key]bool{}
	c.Design.Walk(func(n *elab.Node) bool {
		if c.Cancelled() {
			return false
		}
		in, ok := n.Decl.(*ast.Instance)
		if !ok || n.Def == nil {
			return true
		}
		for _, b := range n.Ports {
			if b.Kind != elab.ConnNone || b.Interface != nil {
				continue
			}
			isInput := b.Direction == ast.DirInput
			if isInput != inputs {
				continue
			}
			if !isInput && !b.Omitted {
				continue // ".q()": left open on purpose
			}
			k := key{in.Pos(), b.Name}
			if seen[k] {
				continue
			}
			seen[k] = true
			var related []diag.Related
			if b.Port != nil && b.Port.Decl != nil {
				related = []diag.Related{{Range: b.Port.Decl.Pos(), Message: "the port"}}
			}
			switch {
			case isInput && b.Omitted:
				c.ReportRelated(in.Pos(), related, "input port %q of %q is not connected, so it floats", b.Name, n.Def.Name)
			case isInput:
				c.ReportRelated(in.Pos(), related, "input port %q of %q is connected to nothing, so it floats", b.Name, n.Def.Name)
			default:
				c.ReportRelated(in.Pos(), related, "%s port %q of %q is not connected: connect it, or write .%s() to leave it open", b.Direction, b.Name, n.Def.Name, b.Name)
			}
		}
		return true
	})
}
