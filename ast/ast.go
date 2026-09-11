// Package ast holds the declaration-grade AST node types svparse/parser
// produces. It has no parsing logic of its own -- see the sibling
// token/lexer split for the same reasoning: vocabulary and algorithm
// stay separate.
//
// Unparsed sub-expressions -- a default value, an array-dimension bound,
// a parameter override's value, a port-connection expression -- are
// kept as raw []preprocessor.Token spans, never a real expression tree.
// This is the "declaration-grade, not full expression grammar" design
// principle made concrete: capture what's written, don't interpret it.
package ast

import "github.com/jfetkotto/svparse/preprocessor"

// Position is embedded in every node. Line/Character mark the node's
// name (or, where there's no name, its start); EndLine/EndCharacter mark
// where its span ends (its closing keyword/brace, or itself for a
// single-token construct).
type Position struct {
	File                  string
	Line, Character       int
	EndLine, EndCharacter int
}

// declBase is embedded by every concrete node type, satisfying Decl via
// an unexported marker method (sealing the interface to intentional
// implementers within this package, the same technique go/ast uses)
// plus Pos().
type declBase struct {
	Position
}

func (declBase) declNode() {}

func (b declBase) Pos() Position { return b.Position }

// Decl is implemented by every AST node.
type Decl interface {
	declNode()
	Pos() Position
}

// File is one source file's parse result: every top-level declaration,
// in source order.
type File struct {
	Path  string
	Decls []Decl
}

// Type is a data type reference: a builtin (logic, bit, int, ...) or a
// user-defined typedef name, optionally package-qualified -- the parser
// doesn't resolve or distinguish these, it just records the name and
// dimensions as written.
type Type struct {
	Position
	Signed           bool
	Unsigned         bool
	PackageQualifier string // "" unless Pkg::Type
	Name             string
	PackedDims       []Dim

	// ParamOverrides carries a parameterized type reference's "#( ... )"
	// arguments -- the class-type half of the same syntax an Instantiation
	// uses, e.g. the "#(8)" in "my_class #(8) obj;". nil for the ordinary
	// unparameterized case.
	ParamOverrides []ParamOverride
}

// Dim is one array/bit-range dimension: [Left:Right] (a range) or
// [Left] alone (an unsized/queue-style dimension, Right nil). Left/Right
// are raw expression token spans -- see the package doc comment.
type Dim struct {
	Left, Right []preprocessor.Token
}
