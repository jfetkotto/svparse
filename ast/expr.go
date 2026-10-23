package ast

import "github.com/jfetkotto/svparse/preprocessor"

// Expression nodes (LRM 11, Annex A.8). They are produced by the expression
// parser and hang off the declaration nodes that used to keep a raw token
// span (see the roadmap); the raw spans stay populated alongside them.
//
// Unlike a declaration, whose Position marks its name, an expression's
// Position spans its whole source text: Line/Character is its first token and
// EndLine/EndCharacter is just past its last.
//
// Operators are kept as their source text ("+", "<<<", "==?", "&&"), not an
// enumeration, so a node prints and round-trips exactly as written and this
// package need not track the lexer's operator vocabulary.
//
// Every field typed Expr is non-nil in a tree the parser produced, except
// where its comment says otherwise. An expression the parser could not make
// sense of is a *BadExpr in that slot, never a nil, so a consumer can walk any
// tree without nil checks.

// exprBase is embedded by every expression node, mirroring declBase.
type exprBase struct {
	Position
}

func (exprBase) node()     {}
func (exprBase) exprNode() {}

func (b exprBase) Pos() Position { return b.Position }

// SetPos sets the node's span. It is exported only because the parser lives
// in another package and exprBase is unexported; nothing else needs it.
func (b *exprBase) SetPos(pos Position) { b.Position = pos }

// Expr is implemented by every expression node, including the helper nodes
// that only occur inside one (NamedArg, PatternItem, ValueRange, DistItem).
type Expr interface {
	Node
	exprNode()
}

// Ident is a simple name: a variable, net, parameter, type, function, or
// package. Also used for the keyword-like scopes $unit, $root and local
// (Name includes the '$'), and for system function names in a Call ("$clog2").
// Whether a name denotes a type or a value is not known until names are
// resolved; see TypeOrExpr.
type Ident struct {
	exprBase
	Name string
}

// ScopedName is a scope-resolution operator application, "Scope::Name"
// (LRM 26.3, 8.23). Scope is an *Ident (a package, or a class without
// parameters), a *ParameterizedName ("C#(8)::x"), or another *ScopedName
// ("pkg::C::x" nests to the left).
type ScopedName struct {
	exprBase
	Scope Expr
	Name  string
}

// ParameterizedName is a class name with a parameter value list, "C#(8)" or
// "C#(.W(8))", used as the scope of a ScopedName or as a type. It reuses
// ParamOverride, the same node an instantiation's "#( ... )" produces. Scope
// is the package (or enclosing scope) of a qualified class, as in
// "pkg::C#(8)"; nil for a bare "C#(8)".
type ParameterizedName struct {
	exprBase
	Scope     Expr // nil unless "pkg::C#(...)"
	Name      string
	Overrides []ParamOverride
}

// MemberSelect is "X.Member": a struct/union/class member, a hierarchical
// name component (LRM 23.6), or a method name that a Call then invokes.
// "a.b.c" nests to the left.
type MemberSelect struct {
	exprBase
	X      Expr
	Member string
}

// Index is a single bit/element select, "X[Index]". "a[i][j]" nests to the
// left. For an associative array Index may be a type or a "default" key.
type Index struct {
	exprBase
	X, Index Expr
}

// RangeKind is the flavour of a part-select (LRM 11.5.1).
type RangeKind int

const (
	RangeColon RangeKind = iota // [Left:Right]
	RangePlus                   // [Left+:Right]
	RangeMinus                  // [Left-:Right]
)

func (k RangeKind) String() string {
	switch k {
	case RangeColon:
		return ":"
	case RangePlus:
		return "+:"
	case RangeMinus:
		return "-:"
	}
	return "?"
}

// RangeSelect is a part-select, "X[Left:Right]", "X[Left+:Right]" or
// "X[Left-:Right]".
type RangeSelect struct {
	exprBase
	X           Expr
	Kind        RangeKind
	Left, Right Expr
}

// LiteralKind says which literal form a Literal is.
type LiteralKind int

const (
	LitInt            LiteralKind = iota // 42, 8'hFF, 'h1F, 4'b10x1
	LitReal                              // 1.5, 1e3
	LitTime                              // 10ns
	LitUnbasedUnsized                    // '0 '1 'x 'z
	LitString                            // "..." (Text includes the quotes)
	LitNull                              // null
	LitUnbounded                         // $ (a queue bound, an open range end)
)

// Literal is a literal. Text is its source spelling, unevaluated: sizing,
// base, and x/z digits are interpreted later by constant evaluation.
type Literal struct {
	exprBase
	Kind LiteralKind
	Text string
}

// Paren is a parenthesized expression, "(X)". Kept rather than dropped
// because parentheses are significant in places: an assignment used as an
// expression must be parenthesized (LRM 11.3.6), and a consumer reporting a
// diagnostic wants the span the author actually wrote.
type Paren struct {
	exprBase
	X Expr
}

// Unary is a prefix operator application: + - ! ~ and the reduction
// operators & | ^ ~& ~| ~^ ^~ (LRM 11.4.9). Increment/decrement is IncDec.
type Unary struct {
	exprBase
	Op string
	X  Expr
}

// Binary is an infix operator application (LRM 11.3, table 11-2),
// including ** , the shifts, equality and wildcard equality, and the logical
// implication and equivalence operators -> and <->. Assignment operators are
// AssignExpr.
type Binary struct {
	exprBase
	Op   string
	X, Y Expr
}

// Conditional is "Cond ? Then : Else" (LRM 11.4.11).
type Conditional struct {
	exprBase
	Cond, Then, Else Expr
}

// Inside is "X inside { Ranges }" (LRM 11.4.13). Each range is a plain value
// expression or a *ValueRange.
type Inside struct {
	exprBase
	X      Expr
	Ranges []Expr
}

// ValueRange is "[Lo:Hi]" in an inside set or a dist item (LRM 11.4.13).
// Either end may be the unbounded literal "$".
type ValueRange struct {
	exprBase
	Lo, Hi Expr
}

// Concat is "{ Elems }" (LRM 11.4.12).
type Concat struct {
	exprBase
	Elems []Expr
}

// Replicate is "{ Count { Elems } }" (LRM 11.4.12.1).
type Replicate struct {
	exprBase
	Count Expr
	Elems []Expr
}

// AssignPattern is an assignment pattern, "'{ Items }" or "T'{ Items }"
// (LRM 10.9). Type is nil for the untyped form. Each item is either a
// positional expression, a *Replicate ("'{2{1}}"), or, for the named forms,
// a *PatternItem.
type AssignPattern struct {
	exprBase
	Type  Expr // nil unless "T'{...}"
	Items []Expr
}

// PatternItem is one "key: value" entry of an assignment pattern, where key
// is an index, a member name, a type, or "default" (IsDefault, Key nil).
type PatternItem struct {
	exprBase
	Key       Expr // nil when IsDefault
	IsDefault bool
	Value     Expr
}

// Cast is "Type'(X)" (LRM 6.24.1). Type is whatever precedes the tick: a
// *TypeExpr for a builtin type or the sign/const forms (signed'(x) is a
// TypeExpr with only Signed set; const'(x) is one named "const"), a
// *TypeOrExpr or other name for a user type, or any constant expression for
// a size cast ("8'(x)", "W'(x)").
type Cast struct {
	exprBase
	Type Expr
	X    Expr
}

// Call is a function or method call (LRM 13, 8.12, 7.12). Func is an *Ident
// for a plain call ("f", or "$display" for a system call), a *ScopedName
// ("pkg::f"), or a *MemberSelect for a method ("obj.m", "arr.find"). Args are
// positional expressions, *NamedArg, or *Empty for an omitted argument
// ("f(a,,c)"). With is the parenthesized iterator expression of an array
// method ("q.find(x) with (x > 3)"). A call written without parentheses
// ("$time", "obj.size") is not a Call, except an array method with an iterator
// clause, "q.find_first with (x > 3)", where the argument list is optional and
// the call has no arguments.
type Call struct {
	exprBase
	Func Expr
	Args []Expr
	With Expr // nil unless a "with (expr)" clause

	// Constraints is the inline constraint block of a randomize call, "x.randomize()
	// with { ... }" or "with (v) { ... }" (LRM 18.7); nil if absent.
	Constraints *ConstraintBlock
}

// ConstraintBlock holds the braces of an inline constraint block, "{ x > 0; x <
// y; }". Tokens is everything between the braces, unparsed: the constraint
// grammar is not modeled yet, so this node keeps the block intact and in place
// until it is, at which point it is replaced by real constraint nodes.
type ConstraintBlock struct {
	exprBase
	Tokens []preprocessor.Token
}

// NamedArg is ".name(Value)" in a call's argument list. Value is nil for
// ".name()" and the ".name" shorthand.
type NamedArg struct {
	exprBase
	Name  string
	Value Expr // nil if absent
}

// Empty is an omitted argument between two commas, "f(a,,c)" (LRM 13.5.3).
type Empty struct {
	exprBase
}

// New is a constructor call (LRM 8.7): "new", "new(Args)", "Scope::new(Args)"
// or, with Copy set, the shallow copy "new Copy" (LRM 8.12). Scope is nil
// for a bare "new".
type New struct {
	exprBase
	Scope Expr // nil unless "C::new"
	Args  []Expr
	Copy  Expr // nil unless "new x"
}

// NewArray is a dynamic array constructor, "new[Size]" or "new[Size](Init)"
// (LRM 7.5.1).
type NewArray struct {
	exprBase
	Size Expr
	Init Expr // nil if absent
}

// This is the "this" handle (LRM 8.11).
type This struct {
	exprBase
}

// Super is the "super" handle (LRM 8.15), as in "super.new(...)".
type Super struct {
	exprBase
}

// MinTypMax is a delay triple "Min : Typ : Max" (LRM 11.11), always inside a
// Paren.
type MinTypMax struct {
	exprBase
	Min, Typ, Max Expr
}

// Streaming is a streaming concatenation, "{ << [SliceSize] { Elems } }"
// (LRM 11.4.14). Op is "<<" or ">>". SliceSize is nil when omitted; it may
// be a constant expression or a type. An element with a "with [ ... ]"
// clause is a *WithRange.
type Streaming struct {
	exprBase
	Op        string
	SliceSize Expr // nil if absent
	Elems     []Expr
}

// WithRange is a streaming element with an array range, "X with [Left:Right]"
// (LRM 11.4.14.4). Right is nil for the single-index form "X with [Left]".
type WithRange struct {
	exprBase
	X           Expr
	Kind        RangeKind
	Left, Right Expr
}

// Tagged is a tagged-union expression, "tagged Member" or
// "tagged Member X" (LRM 7.3.2).
type Tagged struct {
	exprBase
	Member string
	X      Expr // nil for a member with no value
}

// TypeRef is the type reference operator, "type(X)" (LRM 6.23). X is an
// expression or a data type.
type TypeRef struct {
	exprBase
	X Expr
}

// TypeExpr is a data type written where an expression is allowed: a builtin
// or aggregate type that cannot be a value ("$bits(logic [7:0])", the
// "int" of "int'(x)", a "#(int)" override).
type TypeExpr struct {
	exprBase
	Type Type
}

// TypeOrExpr marks a position that accepts either a type or a value, holding
// a name-shaped operand ("$bits(T)", "#(T)", "T'(x)" with T a name) that is
// syntactically both: X is an *Ident, *ScopedName or *ParameterizedName
// that name resolution later decides is a type or a value. An operand that
// can only be one or the other is never wrapped: it is a *TypeExpr or an
// ordinary expression.
type TypeOrExpr struct {
	exprBase
	X Expr
}

// IncDec is "++X", "--X", "X++" or "X--" (LRM 11.4.2). Op is "++" or "--".
type IncDec struct {
	exprBase
	Op     string
	Prefix bool
	X      Expr
}

// AssignExpr is an assignment used as an expression, "(LHS Op RHS)"
// (LRM 11.3.6), with Op "=" or a compound operator ("+=", "<<<=", ...). It
// only occurs inside a Paren. The statement-level assignment is a statement
// node.
type AssignExpr struct {
	exprBase
	Op       string
	LHS, RHS Expr
}

// Dist is "X dist { Items }" (LRM 18.5.4), valid in constraint expressions.
type Dist struct {
	exprBase
	X     Expr
	Items []DistItem
}

// DistItem is one weighted entry of a dist: "Value := Weight" or
// "Value :/ Weight". Value is a value expression or a *ValueRange. Weight is
// nil when omitted, which means a weight of 1 per value.
type DistItem struct {
	exprBase
	Value    Expr
	Weight   Expr // nil if absent
	PerRange bool // ":/" (weight divided over the range) rather than ":="
}

// BadExpr stands for source the expression parser could not parse. Tokens is
// everything it covers, so a consumer can still report or skip it.
type BadExpr struct {
	exprBase
	Tokens []preprocessor.Token
}
