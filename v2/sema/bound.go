package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/svint"
)

// BoundExpr is an expression after name resolution and typing: every node has
// the type the LRM gives it (LRM 11.6, 11.8) and names are resolved to
// symbols. It is built from an ast.Expr by Model.Bind and evaluated by Eval.
// The set of node types is closed.
type BoundExpr interface {
	// Type is the expression's type after width and sign propagation. For an
	// operand in a context-determined position it is already the widened type
	// the surrounding operator uses.
	Type() Type
	// Source is the expression this node was bound from.
	Source() ast.Expr
	bound()
}

type exprInfo struct {
	typ Type
	src ast.Expr
}

func (e *exprInfo) Type() Type       { return e.typ }
func (e *exprInfo) Source() ast.Expr { return e.src }
func (*exprInfo) bound()             {}

// BLiteral is a literal. Kind is the source literal's kind. Value holds an
// integer, unbased unsized ('0 '1 'x 'z; one bit, filled to the node's type on
// evaluation) or string literal's bits; Real holds a real or time literal;
// Str holds a string literal's text.
type BLiteral struct {
	exprInfo
	Kind  ast.LiteralKind
	Value svint.Value
	Real  float64
	Str   string
}

// BName is a use of a declared value: a variable, net, port, argument,
// parameter, enum constant, genvar and so on.
type BName struct {
	exprInfo
	Sym *Symbol
}

// BTypeRef is a data type used as an operand, as in $bits(T).
type BTypeRef struct {
	exprInfo
	T Type
}

// BUnary is a prefix operator: + - ~ ! and the reductions & | ^ ~& ~| ~^.
type BUnary struct {
	exprInfo
	Op string
	X  BoundExpr
}

// BBinary is an infix operator. For the relational and equality operators the
// type of the node is one bit and OperandType is the common type the operands
// are converted to; for the others OperandType is the node's own type.
type BBinary struct {
	exprInfo
	Op          string
	X, Y        BoundExpr
	OperandType Type
}

// BCond is "Cond ? Then : Else".
type BCond struct {
	exprInfo
	Cond, Then, Else BoundExpr
}

// BConcat is "{a, b, ...}".
type BConcat struct {
	exprInfo
	Elems []BoundExpr
}

// BReplicate is "{Count{Elems}}".
type BReplicate struct {
	exprInfo
	Count BoundExpr
	Elems []BoundExpr
}

// BIndex is a bit select, an array element select or a string index.
type BIndex struct {
	exprInfo
	X, Index BoundExpr
}

// BPartSelect is "X[l:r]", "X[l+:w]" or "X[l-:w]".
type BPartSelect struct {
	exprInfo
	X           BoundExpr
	Kind        ast.RangeKind
	Left, Right BoundExpr
}

// BMember is a field select "X.Field" of a struct, union, class or interface.
type BMember struct {
	exprInfo
	X     BoundExpr
	Field string
	Sym   *Symbol // the class or interface member, or nil for a struct field
}

// BCall is a function call. A system function has Name "$..." and no Sym; a
// user function has Sym; a built-in method ("q.size()") has Method and Recv.
type BCall struct {
	exprInfo
	Name   string
	Sym    *Symbol
	Method string
	Recv   BoundExpr
	Args   []BoundExpr
	// Formals gives, for each entry of Args, the index of the formal argument
	// it is passed to (a named argument need not come in order). It is set for
	// a call of a declared function or task.
	Formals []int
}

// CastKind says what a cast does.
type CastKind uint8

const (
	// CastType converts to a named or built-in type, "T'(x)".
	CastType CastKind = iota
	// CastSize changes the width, "8'(x)".
	CastSize
	// CastSign changes the signedness, "signed'(x)" and "unsigned'(x)".
	CastSign
)

// BCast is a cast. The node's Type is the target.
type BCast struct {
	exprInfo
	Kind CastKind
	X    BoundExpr
}

// BInside is "X inside {Ranges}"; a range is a BRange.
type BInside struct {
	exprInfo
	X      BoundExpr
	Ranges []BoundExpr
}

// BRange is "[Lo:Hi]" in an inside set.
type BRange struct {
	exprInfo
	Lo, Hi BoundExpr
}

// BAssign is an assignment used as an expression, "(a = b)" or "(a += b)".
type BAssign struct {
	exprInfo
	Op       string
	LHS, RHS BoundExpr
}

// BIncDec is "++x", "x--" and the like.
type BIncDec struct {
	exprInfo
	Op     string
	Prefix bool
	X      BoundExpr
}

// BAssignPattern is an assignment pattern "'{...}" used where the type of the
// target is known (LRM 10.9): Items has one bound value for each element of an
// array or member of a struct, in order, after the pattern's positional,
// replicated, named and default entries are matched up with them.
type BAssignPattern struct {
	exprInfo
	Items []BoundExpr
	// ElemTypes is the type each item is converted to, as an assignment to the
	// element or member does.
	ElemTypes []Type
}

// BBad stands for an expression that could not be bound, or that this layer
// does not model yet (assignment patterns, streaming, new, tagged unions). Its
// type is Invalid, so nothing that uses it reports a further error.
type BBad struct {
	exprInfo
}

// WalkBound calls f for e and every node below it in source order, stopping
// the descent below a node when f returns false.
func WalkBound(e BoundExpr, f func(BoundExpr) bool) {
	if e == nil || !f(e) {
		return
	}
	each := func(list []BoundExpr) {
		for _, c := range list {
			WalkBound(c, f)
		}
	}
	switch n := e.(type) {
	case *BUnary:
		WalkBound(n.X, f)
	case *BBinary:
		WalkBound(n.X, f)
		WalkBound(n.Y, f)
	case *BCond:
		WalkBound(n.Cond, f)
		WalkBound(n.Then, f)
		WalkBound(n.Else, f)
	case *BConcat:
		each(n.Elems)
	case *BReplicate:
		WalkBound(n.Count, f)
		each(n.Elems)
	case *BIndex:
		WalkBound(n.X, f)
		WalkBound(n.Index, f)
	case *BPartSelect:
		WalkBound(n.X, f)
		WalkBound(n.Left, f)
		WalkBound(n.Right, f)
	case *BMember:
		WalkBound(n.X, f)
	case *BCall:
		WalkBound(n.Recv, f)
		each(n.Args)
	case *BCast:
		WalkBound(n.X, f)
	case *BInside:
		WalkBound(n.X, f)
		each(n.Ranges)
	case *BRange:
		WalkBound(n.Lo, f)
		WalkBound(n.Hi, f)
	case *BAssign:
		WalkBound(n.LHS, f)
		WalkBound(n.RHS, f)
	case *BIncDec:
		WalkBound(n.X, f)
	case *BAssignPattern:
		each(n.Items)
	}
}
