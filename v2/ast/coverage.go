package ast

// Covergroups (LRM 19). Every node here is a Decl: a covergroup's items are
// declarations in it, and a cross body may hold functions.

// Covergroup is "covergroup name [(ports)] [event]; items endgroup" (LRM
// 19.3). Event is the sampling event "@(...)" (nil if absent); BlockEvent is
// true for the "@@(begin f)" form, whose contents are not modeled. Sample is
// true for "with function sample (SamplePorts)". Extends is set for
// "covergroup extends name;" (LRM 19.5.1), which redefines the covergroup of that
// name, so it equals Name.
type Covergroup struct {
	declBase
	Name         string
	Extends      string
	Ports        []Arg
	Event        *TimingControl
	BlockEvent   bool
	Sample       bool
	SamplePorts  []Arg
	Items        []Decl
	EndLine      int
	EndCharacter int
}

// CoverOption is "option.name = expr;" or "type_option.name = expr;" (LRM
// 19.7). Type is true for type_option.
type CoverOption struct {
	declBase
	Type  bool
	Name  string
	Value Expr
}

// CoverPoint is "[[type] label :] coverpoint expr [iff (cond)] { bins }" (LRM
// 19.5). Name is the label, empty when unlabeled, and Type its optional data
// type. Bins holds *CoverBin and *CoverOption items.
type CoverPoint struct {
	declBase
	Name string
	Type Type
	X    Expr
	Iff  Expr
	Bins []Decl
}

// CoverBin is one bin of a coverpoint (LRM 19.5.1) or a bins selection in a
// cross body (LRM 19.6.1.2). Keyword is "bins", "illegal_bins" or
// "ignore_bins". Kind says which form it is:
//
//	"values"          bins b = { Values } [with (With)]
//	"with"            bins b = Source with (With)         (Source: a coverpoint)
//	"set"             bins b = Source                      (a set expression)
//	"trans"           bins b = Trans                       (transition lists)
//	"default"         bins b = default
//	"default sequence" bins b = default sequence
//	"select"          bins b = Source                      (a cross select expression)
//
// Array is true for "b[]" and "b[n]"; Size is n, or nil for "b[]".
type CoverBin struct {
	declBase
	Keyword  string
	Wildcard bool
	Name     string
	Array    bool
	Size     Expr
	Kind     string
	Values   []Expr
	Source   Expr
	With     Expr
	Trans    []TransSet
	Iff      Expr
}

// TransSet is one parenthesised transition, "(a => b [*2] => c)" (LRM
// 19.5.1.1).
type TransSet struct {
	Position
	Steps []TransStep
}

// TransStep is one step of a transition: a list of values or ranges with an
// optional repetition. RepKind is "*", "->" or "=" (empty without a
// repetition); RepMin and RepMax are its count or range bounds.
type TransStep struct {
	Position
	Values  []Expr
	RepKind string
	RepMin  Expr
	RepMax  Expr
}

// CoverCross is "[label :] cross a, b [iff (cond)] { body }" (LRM 19.6). Items
// are the crossed coverpoints; Body holds *CoverBin selections, *CoverOption
// items and functions.
type CoverCross struct {
	declBase
	Name  string
	Items []Expr
	Iff   Expr
	Body  []Decl
}
