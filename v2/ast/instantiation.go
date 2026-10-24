package ast

import "github.com/jfetkotto/svparse/v2/preprocessor"

// Import is a package import: "import pkg::name;" or "import pkg::*;"
// (Member == "*").
type Import struct {
	declBase
	Package string
	Member  string
}

// Instantiation is a module/interface/program instantiation statement,
// which can declare several instances at once sharing one module type
// and one set of parameter overrides ("leaf u0(...), u1(...);").
type Instantiation struct {
	declBase
	ModuleType     string
	ParamOverrides []ParamOverride
	Instances      []Instance
}

// ParamOverride is one entry of an instantiation's "#( ... )" parameter
// override list. Name is "" for a positional (ordered) override.
// Position is the override's own name token for a named override (not
// the preceding "."), matching PortConnection's own convention; for a
// positional override it spans the value's tokens.
//
// ValueExpr is the parsed form of Value (a *TypeExpr, *TypeOrExpr or other
// expression, since an override may be a type); nil until the parser fills
// it, and Value stays populated alongside it. Because a named override is
// positioned on its name alone, ValueExpr lies outside Position there.
type ParamOverride struct {
	Position
	Name      string
	Value     []preprocessor.Token
	ValueExpr Expr
}

// Instance is one instance name within an Instantiation, e.g. the u0 in
// "leaf u0(...)" -- UnpackedDims covers an instance array
// ("leaf u_leaf[3] (...)").
type Instance struct {
	Position
	Name         string
	UnpackedDims []Dim
	Connections  []PortConnection
}

// PortConnection is one entry of an instance's port-connection list.
// Wildcard is true for ".*"; Implicit is true for ".name" shorthand
// (connects to a same-named signal, Expr is nil for both of those);
// Name is "" for a positional connection.
//
// Position is the name token alone for a named connection (so a rename edit
// range is exactly the name), the ".*" token for a wildcard, and the
// expression's tokens for a positional connection.
type PortConnection struct {
	Position
	Name     string
	Wildcard bool
	Implicit bool
	Expr     []preprocessor.Token
	ConnExpr Expr // parsed Expr; nil for ".*", ".name" and an empty ".name()"
}
