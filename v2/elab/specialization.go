package elab

import (
	"fmt"
	"sort"
	"strings"
)

// SpecializationKey identifies what the node looks like to an analysis that
// binds its body: the declaration it comes from and the values of every
// parameter and genvar in scope. Two nodes with the same key bind every
// expression to the same types, so an analysis whose result depends on nothing
// else (its findings are reported by source position) need run on one of them.
// A module instantiated fifty times with the same parameters is analysed once.
// "" means the node cannot be keyed and must be analysed.
func (n *Node) SpecializationKey() string {
	if n.Env == nil {
		return ""
	}
	var decl any = n.Decl
	if n.Def != nil {
		decl = n.Def
	}
	if decl == nil {
		return ""
	}
	var vals []string
	for sym, v := range n.Env.Params {
		vals = append(vals, fmt.Sprintf("p%p=%s", sym, v.String()))
	}
	for sym, v := range n.Env.Vars {
		vals = append(vals, fmt.Sprintf("v%p=%s", sym, v.String()))
	}
	sort.Strings(vals)
	return fmt.Sprintf("%p|%s", decl, strings.Join(vals, ","))
}
