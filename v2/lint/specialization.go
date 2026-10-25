package lint

import "github.com/jfetkotto/svparse/v2/elab"

// specializationKey identifies what an elaborated node looks like to an
// analysis that binds its body (see elab.Node.SpecializationKey): a module
// instantiated fifty times with the same parameters is analysed once.
func specializationKey(n *elab.Node) string { return n.SpecializationKey() }
