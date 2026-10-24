package lint

import (
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/elab"
)

func init() {
	Register(Rule{
		Name:     "implicit-net",
		Doc:      "a net is declared by use: a misspelt name in a port connection or assign silently becomes a wire (LRM 6.10)",
		Severity: diag.Warning,
		Check:    checkImplicitNets,
	})
}

func checkImplicitNets(c *Context) {
	if c.Design == nil {
		return
	}
	c.Design.Walk(func(n *elab.Node) bool {
		if c.Cancelled() {
			return false
		}
		for _, net := range n.ImplicitNets {
			c.Report(net.At, "net %q is implicitly declared as a %d-bit wire; declare it, or use `default_nettype none", net.Name, net.Width)
		}
		return true
	})
}
