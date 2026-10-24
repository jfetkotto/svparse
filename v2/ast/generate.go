package ast

// The generate constructs (LRM 27) are declarations holding further
// declarations, so they sit in a container's Body and nest. Each node's
// Position spans the whole construct from its first keyword.

// GenerateRegion is a "generate ... endgenerate" region (LRM 27.3). It only
// brackets module items; it adds no scope.
type GenerateRegion struct {
	declBase
	Body []Decl
}

// GenerateBlock is the body of a generate construct (LRM 27.3). Bare is true
// for the single un-bracketed item LRM 27.5 allows ("if (c) wire w;"): Body
// then holds that item's declarations and Label is empty. Otherwise it is a
// "begin [: label] ... end" block and Label is the name from the begin side,
// or the end side if only that one is written.
type GenerateBlock struct {
	declBase
	Label string
	Bare  bool
	Body  []Decl
}

// GenerateIf is a conditional generate (LRM 27.5). Then is always a block.
// Else is nil, a *GenerateBlock, or, for "else if", a *GenerateIf.
type GenerateIf struct {
	declBase
	Cond Expr
	Then *GenerateBlock
	Else Decl
}

// GenerateFor is a loop generate (LRM 27.4), "for (genvar i = 0; i < N; i++)".
// Genvar is true when the header declares the loop variable ("genvar i"). Step
// is the iteration, an *Assign or an *ExprStmt around an IncDec.
type GenerateFor struct {
	declBase
	Genvar bool
	Var    string
	Init   Expr
	Cond   Expr
	Step   Stmt
	Body   *GenerateBlock
}

// GenerateCase is a case generate (LRM 27.5).
type GenerateCase struct {
	declBase
	Expr  Expr
	Items []GenerateCaseItem
}

// GenerateCaseItem is one arm of a GenerateCase: Exprs are the match values,
// empty for the default arm (Default true).
type GenerateCaseItem struct {
	declBase
	Exprs   []Expr
	Default bool
	Body    *GenerateBlock
}
