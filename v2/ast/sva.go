package ast

// Sequence and property expressions (LRM 16.7, 16.12) share the expression
// grammar of LRM 11 for their boolean leaves, so they are Expr nodes. Binary
// property and sequence operators that need no extra fields ("and", "or",
// "intersect", "within", "throughout", "iff", "until", "|->", "|=>", ...) use
// Binary with the operator text as Op.

// SeqDelay is a cycle delay between sequence expressions, "X ##N Y" (LRM
// 16.7). X is nil for a leading delay ("##1 a"). Min is the delay or the low
// bound; Max is the high bound of "##[lo:hi]" (a Literal $ when unbounded) and
// nil for a single delay. Sugar is "*" for "##[*]" and "+" for "##[+]", with
// Min and Max then nil.
type SeqDelay struct {
	exprBase
	X, Y     Expr
	Min, Max Expr
	Sugar    string
}

// SeqRepeat is a sequence repetition (LRM 16.9.2). Kind is "*" (consecutive),
// "->" (goto) or "=" (non-consecutive). Min and Max are the count or range
// bounds (Max is nil without a range); Sugar is "*" for "[*]" and "+" for
// "[+]", with Min and Max then nil.
type SeqRepeat struct {
	exprBase
	X        Expr
	Kind     string
	Min, Max Expr
	Sugar    string
}

// PropPrefix is a property prefix operator (LRM 16.12): Op is "not",
// "strong", "weak", "nexttime", "s_nexttime", "always", "s_always",
// "eventually", "s_eventually", "accept_on", "reject_on", "sync_accept_on",
// "sync_reject_on" or "first_match". Arg is the count of nexttime, the low
// bound of a range, or the condition of the *_on operators; Max is the high
// bound when Range is set. Items are the match items of first_match.
type PropPrefix struct {
	exprBase
	Op    string
	Arg   Expr
	Max   Expr
	Range bool
	X     Expr
	Items []Expr
}

// PropIf is "if (cond) prop [else prop]" in a property (LRM 16.12.7).
type PropIf struct {
	exprBase
	Cond, Then, Else Expr
}

// PropCase is a property "case (expr) ... endcase" (LRM 16.12.8).
type PropCase struct {
	exprBase
	Expr  Expr
	Items []PropCaseItem
}

// PropCaseItem is one arm of a PropCase; Exprs is empty for the default arm.
type PropCaseItem struct {
	Position
	Exprs   []Expr
	Default bool
	Body    Expr
}

// ClockedProp is "@(event) X", a clocking event applied to a sequence or
// property (LRM 16.16).
type ClockedProp struct {
	exprBase
	Event *TimingControl
	X     Expr
}

// DisableIff is "disable iff (cond) X" (LRM 16.12.1).
type DisableIff struct {
	exprBase
	Cond, X Expr
}

// SeqMatch is a sequence with match items, "(seq, a = 1, f())" (LRM 16.10).
type SeqMatch struct {
	exprBase
	X     Expr
	Items []Expr
}

// PropertyDecl is "property name [(ports)]; locals expr; endproperty" (LRM
// 16.12). Ports are Args whose type name may be "sequence", "property" or
// "untyped". Decls are the local variable declarations before the body.
type PropertyDecl struct {
	declBase
	Name         string
	Ports        []Arg
	Decls        []Decl
	Body         Expr
	EndLine      int
	EndCharacter int
}

// SequenceDecl is "sequence name [(ports)]; locals expr; endsequence" (LRM
// 16.8).
type SequenceDecl struct {
	declBase
	Name         string
	Ports        []Arg
	Decls        []Decl
	Body         Expr
	EndLine      int
	EndCharacter int
}

// ConcurrentAssert is a concurrent assertion statement (LRM 16.14) or an
// "expect" statement (LRM 16.17). Kind is "assert", "assume", "cover",
// "restrict" or "expect"; Sequence is true for "cover sequence". Spec is the
// property or sequence specification inside the parentheses. Then and Else are
// the action block's pass and fail statements.
type ConcurrentAssert struct {
	stmtBase
	Kind     string
	Sequence bool
	Spec     Expr
	Then     Stmt
	Else     Stmt
}

// AssertionItem is an assertion statement at module scope; Body is a
// *ConcurrentAssert, an *ImmediateAssert, or a *Labeled around one.
type AssertionItem struct {
	declBase
	Body Stmt
}
