// Package ast holds the declaration-grade AST node types svparse/parser
// produces. It has no parsing logic of its own -- see the sibling
// token/lexer split for the same reasoning: vocabulary and algorithm
// stay separate.
//
// Sub-expressions -- a default value, an array-dimension bound, a parameter
// override's value, a port-connection expression -- are kept as raw
// []preprocessor.Token spans exactly as written, and each also has a parsed
// sibling field (InitialExpr, DefaultExpr, LeftExpr, ...) holding the
// expression tree. The raw span stays populated alongside it: capture what's
// written, and interpret it separately. A parsed sibling is nil when its span
// is empty, and a *BadExpr over the whole span when the span did not parse.
//
// Walk and Inspect traverse a tree in source order (go/ast style), and Dump
// renders one as a compact S-expression for golden tests and debugging.
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

// Decl is implemented by every declaration node. It is a Node that can
// appear directly in a File, Container, Class, or Package body.
type Decl interface {
	Node
	declNode()
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
//
// Position covers the type's name token only, not its dimensions or
// parameter overrides, so the bound expressions of PackedDims lie outside it
// (the range a consumer wants for hover and go-to-definition on the name).
//
// An anonymous aggregate written in place, as in "struct packed {...} s;" or
// "enum {A, B} state;", is a Type whose Name is the keyword ("struct",
// "union" or "enum") and whose Inline holds the *Struct, *Union or *Enum with
// its members. Position is then the keyword token. A typedef'd aggregate is
// not Inline: the Typedef owns the body and the Type just names it.
//
// "struct {int x;} a, b;" declares one anonymous type for two variables, but
// each Variable gets its own copy of the aggregate so the tree has no shared
// nodes; the copies keep the same Position, which is how to tell they are one
// type.
type Type struct {
	Position
	Signed           bool
	Unsigned         bool
	PackageQualifier string // "" unless Pkg::Type
	Modport          string // "" unless IfaceName.modport / interface.modport
	Name             string
	PackedDims       []Dim

	// Inline is the anonymous aggregate this type is, or nil for every other
	// type. Name is "struct", "union" or "enum" when it is set.
	Inline Decl

	// ParamOverrides carries a parameterized type reference's "#( ... )"
	// arguments -- the class-type half of the same syntax an Instantiation
	// uses, e.g. the "#(8)" in "my_class #(8) obj;". nil for the ordinary
	// unparameterized case.
	ParamOverrides []ParamOverride
}

// Dim is one array/bit-range dimension: [Left:Right] (a range) or
// [Left] alone (an unsized/queue-style dimension, Right nil). Left/Right
// are raw expression token spans -- see the package doc comment.
//
// LeftExpr and RightExpr are the parsed forms of Left and Right: nil when the
// bound is absent, a *BadExpr covering the tokens if they did not parse. A
// lone bound that is just a name ("[N]") is wrapped in a *TypeOrExpr, since in
// a declarator it is a size or, for an associative array, an index type.
// Wildcard marks the "[*]" index of a wildcard associative array (LRM 7.8.1),
// which is not an expression; Left holds its "*" token and LeftExpr is nil.
type Dim struct {
	Left, Right []preprocessor.Token

	LeftExpr, RightExpr Expr
	Wildcard            bool
}
