package ast

import "github.com/jfetkotto/svparse/preprocessor"

// Statement nodes (LRM 9, 10, 12, 13, Annex A.6) and the procedural items that
// hold them. The statement parser produces them; until it does, function and
// task bodies and procedural blocks are skipped as raw tokens.
//
// Like an expression, a statement's Position spans its whole source text,
// first token through the last (a simple statement includes its ';'). A
// statement that needs a label is wrapped in a *Labeled rather than carrying
// a label field of its own.
//
// Where the grammar allows a statement-or-null the field is a Stmt that is
// never nil in a tree the parser produced: an empty statement is a *Null, and a
// construct the parser cannot read is a *BadStmt, so a consumer can walk any
// tree without nil checks. The exceptions are the optional parts a field's
// comment names, such as an else branch.

// stmtBase is embedded by every statement node, mirroring exprBase.
type stmtBase struct {
	Position
}

func (stmtBase) node()     {}
func (stmtBase) stmtNode() {}

func (b stmtBase) Pos() Position { return b.Position }

// SetPos sets the node's span. It is exported only because the parser lives in
// another package and stmtBase is unexported.
func (b *stmtBase) SetPos(pos Position) { b.Position = pos }

// Stmt is implemented by every statement node, including the helper nodes that
// only occur inside one (CaseItem, EventTerm, TimingControl, RandCaseItem).
type Stmt interface {
	Node
	stmtNode()
}

// Block is a "begin ... end" or "fork ... join" block (LRM 9.3). Decls are the
// block-local declarations (variables, typedefs, parameters); the LRM puts them
// before the statements, and a block that mixes them is read the same way, so
// their relative order is not kept.
type Block struct {
	stmtBase
	Fork     bool   // "fork ... join" rather than "begin ... end"
	Join     string // "join", "join_any" or "join_none" for a fork, else ""
	Label    string // the name in "begin : name"
	EndLabel string // the name in "end : name"
	Decls    []Decl
	Stmts    []Stmt
}

// Labeled is a statement with a label, "name: statement" (LRM 9.3.5).
type Labeled struct {
	stmtBase
	Label string
	Body  Stmt
}

// Null is the empty statement, a lone ";".
type Null struct {
	stmtBase
}

// If is an if/else statement (LRM 12.4). Qualifier is "unique", "unique0" or
// "priority", or "". Else is nil if absent; an "else if" chain nests in Else.
type If struct {
	stmtBase
	Qualifier string
	Cond      Expr
	Then      Stmt
	Else      Stmt // nil if absent
}

// Case is a case statement (LRM 12.5): "case", "casez", "casex", or the
// "case (x) inside" form.
type Case struct {
	stmtBase
	Qualifier string // "unique", "unique0", "priority", or ""
	Kind      string // "case", "casez" or "casex"
	Inside    bool   // "case (x) inside"
	Subject   Expr
	Items     []CaseItem
}

// CaseItem is one arm of a Case: "expr, expr: statement" or "default:
// statement". Exprs is empty for the default arm.
type CaseItem struct {
	stmtBase
	Exprs   []Expr
	Default bool
	Body    Stmt
}

// For is a for loop (LRM 12.7.1). The initialisation is either declarations
// ("for (int i = 0, j = 1; ...)", InitDecls) or assignments ("for (i = 0;
// ...)", Init), never both. Cond is nil if omitted. Each Step is an
// assignment, an increment or decrement, or a call, as an *Assign or *ExprStmt.
type For struct {
	stmtBase
	InitDecls []Decl
	Init      []Stmt
	Cond      Expr // nil if omitted
	Step      []Stmt
	Body      Stmt
}

// Foreach loops over an array's dimensions, "foreach (arr[i, j]) ..." (LRM
// 12.7.3). Vars holds one name per dimension in order; "" stands for a
// dimension whose variable is left out, as in "foreach (arr[, j])".
type Foreach struct {
	stmtBase
	Array Expr
	Vars  []string
	Body  Stmt
}

// While is "while (Cond) Body" (LRM 12.7.4).
type While struct {
	stmtBase
	Cond Expr
	Body Stmt
}

// DoWhile is "do Body while (Cond);" (LRM 12.7.6).
type DoWhile struct {
	stmtBase
	Body Stmt
	Cond Expr
}

// Repeat is "repeat (Count) Body" (LRM 12.7.2).
type Repeat struct {
	stmtBase
	Count Expr
	Body  Stmt
}

// Forever is "forever Body" (LRM 12.7.5).
type Forever struct {
	stmtBase
	Body Stmt
}

// TimingControl is a delay or event control (LRM 9.4): "#10", "##2", "@(posedge
// clk or negedge rst)", "@*", and, in an intra-assignment event control, a
// leading "repeat (n)". Exactly one of Delay, Events or Star describes it.
type TimingControl struct {
	stmtBase
	Delay      Expr        // "#d" or "##d"; nil for an event control
	CycleDelay bool        // "##" (a clocking-block cycle delay) rather than "#"
	Events     []EventTerm // "@(a or b, c)"
	Star       bool        // "@*" or "@(*)"
	Repeat     Expr        // the count of "repeat (n) @(...)"; nil otherwise
}

// EventTerm is one entry of an event expression: "posedge clk", "a iff en",
// "b". Edge is "posedge", "negedge", "edge", or "".
type EventTerm struct {
	stmtBase
	Edge string
	X    Expr
	Iff  Expr // nil if absent
}

// TimedStmt is a statement preceded by a timing control, "#10 x = 1;" or
// "@(posedge clk) q <= d;" (LRM 9.4).
type TimedStmt struct {
	stmtBase
	Control *TimingControl
	Body    Stmt
}

// Wait is "wait (Cond) Body" (LRM 9.4.3).
type Wait struct {
	stmtBase
	Cond Expr
	Body Stmt
}

// WaitFork is "wait fork;" (LRM 9.6.1).
type WaitFork struct {
	stmtBase
}

// WaitOrder is "wait_order (a, b, c) Then else Else" (LRM 15.5.4).
type WaitOrder struct {
	stmtBase
	Events []Expr
	Then   Stmt // nil if only an else branch is written
	Else   Stmt // nil if absent
}

// EventTrigger is "-> ev;" or the nonblocking "->> [control] ev;" (LRM 15.5.1).
type EventTrigger struct {
	stmtBase
	Nonblocking bool
	Control     *TimingControl // the delay or event control of "->>"; nil if absent
	Event       Expr
}

// Assign is a procedural assignment (LRM 10.4): blocking "a = b;", nonblocking
// "a <= b;", or a compound operator such as "a += b;". Op is the operator as
// written. Control is the intra-assignment delay or event control of "a = #5
// b;" and nil if absent.
type Assign struct {
	stmtBase
	Op      string
	LHS     Expr
	Control *TimingControl
	RHS     Expr
}

// Nonblocking reports whether a is a nonblocking assignment, "<=".
func (a *Assign) Nonblocking() bool { return a.Op == "<=" }

// ProcAssign is a procedural continuous assignment (LRM 10.6): "assign a = b;",
// "deassign a;", "force a = b;" or "release a;". RHS is nil for deassign and
// release.
type ProcAssign struct {
	stmtBase
	Kind string // "assign", "deassign", "force" or "release"
	LHS  Expr
	RHS  Expr // nil for deassign and release
}

// ExprStmt is an expression used as a statement: a call "f(x);", "void'(f());",
// or an increment "i++;".
type ExprStmt struct {
	stmtBase
	X Expr
}

// Return is "return;" or "return Value;" (LRM 13.4.1).
type Return struct {
	stmtBase
	Value Expr // nil if absent
}

// Break is "break;" (LRM 12.8).
type Break struct {
	stmtBase
}

// Continue is "continue;" (LRM 12.8).
type Continue struct {
	stmtBase
}

// Disable is "disable name;" or "disable fork;" (LRM 9.6).
type Disable struct {
	stmtBase
	Fork   bool // "disable fork"
	Target Expr // the block, task or function named; nil for "disable fork"
}

// ImmediateAssert is an immediate assertion (LRM 16.3): "assert (c) pass else
// fail;", and the "assume" and "cover" forms. Deferred is "#0" or "final" for
// a deferred assertion and "" for a simple one. Then and Else are nil if
// absent; a cover has only Then.
type ImmediateAssert struct {
	stmtBase
	Kind     string // "assert", "assume" or "cover"
	Deferred string // "", "#0" or "final"
	Cond     Expr
	Then     Stmt
	Else     Stmt
}

// RandCase is a randcase statement (LRM 18.16).
type RandCase struct {
	stmtBase
	Items []RandCaseItem
}

// RandCaseItem is one weighted arm of a RandCase, "Weight: Body".
type RandCaseItem struct {
	stmtBase
	Weight Expr
	Body   Stmt
}

// RawStmt stands for a statement the parser recognised but does not model yet
// (a pattern-matching "if (x matches ...)", a randsequence). Tokens is all of
// it, so nothing is lost, and no error is reported for it.
type RawStmt struct {
	stmtBase
	Tokens []preprocessor.Token
}

// BadStmt stands for source the statement parser could not make sense of.
// Tokens is everything it covers.
type BadStmt struct {
	stmtBase
	Tokens []preprocessor.Token
}

// The procedural items below sit in a container's Body, so they are
// declarations (Decl) holding statements. Each one's Position spans the whole
// item from its keyword.

// Initial is an initial block (LRM 9.2.1).
type Initial struct {
	declBase
	Body Stmt
}

// Final is a final block (LRM 9.2.3).
type Final struct {
	declBase
	Body Stmt
}

// Always is an always procedure (LRM 9.2.2). Kind is "always", "always_comb",
// "always_latch" or "always_ff". Body usually starts with a timing control, so
// "always @(posedge clk) q <= d;" has a *TimedStmt as its Body.
type Always struct {
	declBase
	Kind string
	Body Stmt
}

// ContinuousAssign is a continuous assignment, "assign #d (strength) a = b,
// c = d;" (LRM 10.3). Every entry of Assigns has Op "=" and no Control.
// DriveStrength is the parenthesised pair, e.g. ["strong0", "pull1"]; Delay
// holds the one to three delay values.
type ContinuousAssign struct {
	declBase
	DriveStrength []string
	Delay         []Expr
	Assigns       []Assign
}
