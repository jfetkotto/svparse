package ast

// RandSequence is a randsequence statement (LRM 18.17). Start is the
// production the sequence begins with, "" when the header names none (the
// first production then starts it).
type RandSequence struct {
	stmtBase
	Start       string
	Productions []RandProduction
}

// RandProduction is one production, "[type] name [(args)] : rule | rule ;".
type RandProduction struct {
	stmtBase
	Name       string
	ReturnType Type // the zero Type if none is written
	Args       []Arg
	Rules      []RandRule
}

// RandRule is one alternative of a production. Elems holds, in order, the
// *RandItem, *RandCode, *RandIf, *RandRepeat, *RandSeqCase and *RandJoin nodes
// it is made of. Weight is the expression after ":=" and WeightCode the code
// block that may follow it; both are nil when absent.
type RandRule struct {
	stmtBase
	Elems      []Stmt
	Weight     Expr
	WeightCode *RandCode
}

// RandItem is a use of a production, "name" or "name(args)".
type RandItem struct {
	stmtBase
	Name string
	Args []Expr
}

// RandCode is a code block of a rule, "{ declarations statements }". Inside it
// "break" and "return" end the production (LRM 18.17.6).
type RandCode struct {
	stmtBase
	Decls []Decl
	Stmts []Stmt
}

// RandIf is "if (cond) item [else item]" inside a rule.
type RandIf struct {
	stmtBase
	Cond Expr
	Then *RandItem
	Else *RandItem // nil if absent
}

// RandRepeat is "repeat (count) item" inside a rule.
type RandRepeat struct {
	stmtBase
	Count Expr
	Item  *RandItem
}

// RandSeqCase is "case (x) ... endcase" inside a rule.
type RandSeqCase struct {
	stmtBase
	X     Expr
	Items []RandSeqCaseItem
}

// RandSeqCaseItem is one arm of a RandSeqCase: "a, b : item;" or "default :
// item;".
type RandSeqCaseItem struct {
	stmtBase
	Exprs   []Expr
	Default bool
	Item    *RandItem
}

// RandJoin is "rand join [(count)] item item ...", the productions of which run
// interleaved (LRM 18.17.5).
type RandJoin struct {
	stmtBase
	Count Expr // nil if absent
	Items []RandItem
}
