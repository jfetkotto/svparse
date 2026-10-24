package lint

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
)

func init() {
	Register(Rule{
		Name:     "unconnected-port",
		Doc:      "an instance leaves a port out of its connection list: an input floats and an output goes nowhere (LRM 23.3.2); write .port() to leave one open on purpose",
		Severity: diag.Warning,
		Check:    checkUnconnectedPorts,
	})
}

// checkUnconnectedPorts reports each port an instantiation does not mention.
// A port written ".p()" is deliberately open and is not reported; neither is
// one that ".*" or ".p" connects. An instance in a loop or an array is
// elaborated many times but written once, so each finding is reported once per
// source position.
func checkUnconnectedPorts(c *Context) {
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
			if !b.Omitted {
				continue
			}
			k := key{in.Pos(), b.Name}
			if seen[k] {
				continue
			}
			seen[k] = true
			dir := string(b.Direction)
			if dir == "" {
				dir = "port"
			} else {
				dir += " port"
			}
			var related []diag.Related
			if b.Port != nil && b.Port.Decl != nil {
				related = []diag.Related{{Range: b.Port.Decl.Pos(), Message: "the port"}}
			}
			c.ReportRelated(in.Pos(), related, "%s %q of %q is not connected: connect it, or write .%s() to leave it open", dir, b.Name, n.Def.Name, b.Name)
		}
		return true
	})
}
