package ast

import "github.com/jfetkotto/svparse/v2/preprocessor"

// Constraint items (LRM 18.5) are neither expressions nor statements, so they
// have their own sealed interface. A constraint block holds them in
// Constraint.Items and ConstraintBlock.Items.

// ConstraintItem is implemented by every constraint item node.
type ConstraintItem interface {
	Node
	constraintNode()
}

type constraintBase struct {
	Position
}

func (constraintBase) node()           {}
func (constraintBase) constraintNode() {}

// Pos returns the node's source span.
func (b constraintBase) Pos() Position { return b.Position }

// SetPos sets the node's span. The parser stamps every node it creates.
func (b *constraintBase) SetPos(pos Position) { b.Position = pos }

// ConstraintExpr is "[soft] expr;" or "[soft] expr dist { ... };" (LRM
// 18.5.4, 18.5.14). The expression is a *Dist when a dist clause is present.
type ConstraintExpr struct {
	constraintBase
	Soft bool
	X    Expr
}

// ConstraintImplies is "expr -> set" (LRM 18.5.6).
type ConstraintImplies struct {
	constraintBase
	Cond Expr
	Body ConstraintItem
}

// ConstraintIf is "if (cond) set [else set]" (LRM 18.5.7).
type ConstraintIf struct {
	constraintBase
	Cond Expr
	Then ConstraintItem
	Else ConstraintItem
}

// ConstraintForeach is "foreach (array[i, j]) set" (LRM 18.5.8). A skipped
// index, "arr[,j]", is the empty string in Vars.
type ConstraintForeach struct {
	constraintBase
	Array Expr
	Vars  []string
	Body  ConstraintItem
}

// ConstraintSolve is "solve a, b before c, d;" (LRM 18.5.10).
type ConstraintSolve struct {
	constraintBase
	Solve  []Expr
	Before []Expr
}

// ConstraintDisableSoft is "disable soft expr;" (LRM 18.5.14.2).
type ConstraintDisableSoft struct {
	constraintBase
	X Expr
}

// ConstraintUnique is "unique { a, b, [lo:hi] };" (LRM 18.5.5).
type ConstraintUnique struct {
	constraintBase
	Exprs []Expr
}

// ConstraintSet is a braced group of items, "{ ... }", used as the body of an
// implication, if, else or foreach.
type ConstraintSet struct {
	constraintBase
	Items []ConstraintItem
}

// ConstraintRaw keeps the tokens of a constraint item that could not be parsed.
type ConstraintRaw struct {
	constraintBase
	Tokens []preprocessor.Token
}
