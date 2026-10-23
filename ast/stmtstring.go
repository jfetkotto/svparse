package ast

import "strings"

// StmtString renders s as SystemVerilog source that parses back to an
// equivalent tree, on one line with tokens separated by spaces. Like
// ExprString it prints the tree as given, so a hand-built tree whose nesting
// the grammar would read differently (an else that belongs to an outer if)
// prints source that parses differently; a tree from the parser round-trips.
//
// A node it cannot print faithfully appears as "/*unhandled*/": a block-local
// declaration other than a variable, parameter, simple typedef, or import, or a
// variable whose type is an anonymous aggregate.
func StmtString(s Stmt) string {
	if s == nil {
		return ""
	}
	return stmtSource(s)
}

func stmtSource(s Stmt) string {
	switch s := s.(type) {
	case *Block:
		kw, closer := "begin", "end"
		if s.Fork {
			kw, closer = "fork", s.Join
			if closer == "" {
				closer = "join"
			}
		}
		parts := []string{kw + labelSuffix(s.Label)}
		for _, d := range s.Decls {
			parts = append(parts, declSource(d))
		}
		for _, st := range s.Stmts {
			parts = append(parts, stmtSource(st))
		}
		parts = append(parts, closer+labelSuffix(s.EndLabel))
		return strings.Join(parts, " ")
	case *Labeled:
		return identSource(s.Label) + " : " + stmtSource(s.Body)
	case *Null:
		return ";"
	case *If:
		out := qualified(s.Qualifier, "if") + " (" + exprSource(s.Cond) + ") " + stmtSource(s.Then)
		if s.Else != nil {
			out += " else " + stmtSource(s.Else)
		}
		return out
	case *Case:
		out := qualified(s.Qualifier, s.Kind) + " (" + exprSource(s.Subject) + ")"
		if s.Inside {
			out += " inside"
		}
		for i := range s.Items {
			out += " " + stmtSource(&s.Items[i])
		}
		return out + " endcase"
	case *CaseItem:
		if s.Default {
			return "default : " + stmtSource(s.Body)
		}
		return exprListSource(s.Exprs) + " : " + stmtSource(s.Body)
	case *For:
		return "for (" + forInitSource(s) + "; " + ExprString(s.Cond) + "; " + stmtListSource(s.Step) + ") " + stmtSource(s.Body)
	case *Foreach:
		vars := make([]string, len(s.Vars))
		for i, v := range s.Vars {
			vars[i] = v
		}
		return "foreach (" + exprSource(s.Array) + "[" + strings.Join(vars, ", ") + "]) " + stmtSource(s.Body)
	case *While:
		return "while (" + exprSource(s.Cond) + ") " + stmtSource(s.Body)
	case *DoWhile:
		return "do " + stmtSource(s.Body) + " while (" + exprSource(s.Cond) + ");"
	case *Repeat:
		return "repeat (" + exprSource(s.Count) + ") " + stmtSource(s.Body)
	case *Forever:
		return "forever " + stmtSource(s.Body)
	case *TimedStmt:
		return timingSource(s.Control) + " " + stmtSource(s.Body)
	case *Wait:
		return "wait (" + exprSource(s.Cond) + ") " + stmtSource(s.Body)
	case *WaitFork:
		return "wait fork;"
	case *WaitOrder:
		out := "wait_order (" + exprListSource(s.Events) + ")"
		if s.Then != nil {
			out += " " + stmtSource(s.Then)
		}
		if s.Else != nil {
			out += " else " + stmtSource(s.Else)
		}
		return out
	case *EventTrigger:
		out := "->"
		if s.Nonblocking {
			out = "->>"
			if s.Control != nil {
				out += " " + timingSource(s.Control)
			}
		}
		return out + " " + exprSource(s.Event) + ";"
	case *Assign:
		return assignSource(s) + ";"
	case *ProcAssign:
		out := s.Kind + " " + exprSource(s.LHS)
		if s.RHS != nil {
			out += " = " + exprSource(s.RHS)
		}
		return out + ";"
	case *ExprStmt:
		return exprSource(s.X) + ";"
	case *Return:
		if s.Value == nil {
			return "return;"
		}
		return "return " + exprSource(s.Value) + ";"
	case *Break:
		return "break;"
	case *Continue:
		return "continue;"
	case *Disable:
		if s.Fork {
			return "disable fork;"
		}
		return "disable " + exprSource(s.Target) + ";"
	case *ImmediateAssert:
		out := s.Kind
		if s.Deferred != "" {
			out += " " + s.Deferred
		}
		out += " (" + exprSource(s.Cond) + ")"
		if s.Then != nil {
			out += " " + stmtSource(s.Then)
		}
		if s.Else != nil {
			out += " else " + stmtSource(s.Else)
		}
		return out
	case *RandCase:
		out := "randcase"
		for i := range s.Items {
			out += " " + stmtSource(&s.Items[i])
		}
		return out + " endcase"
	case *RandCaseItem:
		return exprSource(s.Weight) + " : " + stmtSource(s.Body)
	case *RawStmt:
		return tokenSource(s.Tokens)
	case *BadStmt:
		return tokenSource(s.Tokens)
	case *TimingControl:
		return timingSource(s)
	}
	return unhandledStmt
}

// unhandledStmt marks a node StmtString cannot print.
const unhandledStmt = "/*unhandled*/"

func labelSuffix(label string) string {
	if label == "" {
		return ""
	}
	return " : " + identSource(label)
}

// qualified is "unique if" and the like.
func qualified(qualifier, kw string) string {
	if qualifier == "" {
		return kw
	}
	return qualifier + " " + kw
}

// assignSource is "lhs op [control] rhs" without the ';'.
func assignSource(a *Assign) string {
	out := exprSource(a.LHS) + " " + a.Op + " "
	if a.Control != nil {
		out += timingSource(a.Control) + " "
	}
	return out + exprSource(a.RHS)
}

// forInitSource is a for loop's initialisation: declarations "int i = 0, j = 1"
// or assignments "i = 0, j = 1".
func forInitSource(f *For) string {
	if len(f.InitDecls) == 0 {
		return stmtListSource(f.Init)
	}
	first, ok := f.InitDecls[0].(*Variable)
	if !ok || first.Type.Inline != nil {
		return unhandledStmt
	}
	parts := make([]string, len(f.InitDecls))
	for i, d := range f.InitDecls {
		v, ok := d.(*Variable)
		if !ok {
			return unhandledStmt
		}
		parts[i] = declaratorSource(v)
	}
	return typeSource(first.Type) + " " + strings.Join(parts, ", ")
}

// stmtListSource prints simple statements comma-separated without their ';',
// as a for header holds them.
func stmtListSource(list []Stmt) string {
	parts := make([]string, len(list))
	for i, s := range list {
		switch s := s.(type) {
		case *Assign:
			parts[i] = assignSource(s)
		case *ExprStmt:
			parts[i] = exprSource(s.X)
		default:
			parts[i] = unhandledStmt
		}
	}
	return strings.Join(parts, ", ")
}

// timingSource spells a timing control: "#10", "##2", "@(a or posedge b)", "@*",
// with a "repeat (n) " prefix.
func timingSource(c *TimingControl) string {
	if c == nil {
		return ""
	}
	var out string
	if c.Repeat != nil {
		out = "repeat (" + exprSource(c.Repeat) + ") "
	}
	switch {
	case c.Delay != nil && c.CycleDelay:
		return out + "##" + delaySource(c.Delay)
	case c.Delay != nil:
		return out + "#" + delaySource(c.Delay)
	case c.Star:
		return out + "@*"
	}
	terms := make([]string, len(c.Events))
	for i := range c.Events {
		t := &c.Events[i]
		term := exprSource(t.X)
		if t.Edge != "" {
			term = t.Edge + " " + term
		}
		if t.Iff != nil {
			term += " iff " + exprSource(t.Iff)
		}
		terms[i] = term
	}
	return out + "@(" + strings.Join(terms, " or ") + ")"
}

// delaySource prints a delay value, parenthesising anything the parser could
// not have read as a single primary, since "#a + b" would read as a delay of a
// and "#a[0]" as a delay of a followed by a select. Operators and postfix forms
// are what need the parentheses; everything else a primary can be prints as is.
func delaySource(e Expr) string {
	switch e.(type) {
	case *Binary, *Unary, *Conditional, *Inside, *Call, *MemberSelect, *Index,
		*RangeSelect, *IncDec, *AssignExpr, *Cast, *MinTypMax, *WithRange, *Dist,
		*NamedArg, *PatternItem, *ValueRange, *DistItem, *Empty:
		return "(" + exprSource(e) + ")"
	}
	return exprSource(e)
}

// declSource prints a block-local declaration with its ';'.
func declSource(d Decl) string {
	switch d := d.(type) {
	case *Variable:
		if d.Type.Inline != nil || d.Type.TypeRef != nil {
			return unhandledStmt
		}
		out := typeSource(d.Type)
		if d.IsStatic {
			out = "static " + out
		}
		return out + " " + declaratorSource(d) + ";"
	case *Parameter:
		kw := "parameter"
		if d.IsLocal {
			kw = "localparam"
		}
		out := kw + " " + typeSource(d.Type) + " " + identSource(d.Name) + dimsSource(d.UnpackedDims)
		if d.DefaultExpr != nil {
			out += " = " + exprSource(d.DefaultExpr)
		} else if len(d.Default) > 0 {
			out += " = " + tokenSource(d.Default)
		}
		return out + ";"
	case *Typedef:
		alias, ok := d.Underlying.(*TypeAlias)
		switch {
		case d.Underlying == nil:
			return "typedef " + d.ForwardKind + " " + identSource(d.Name) + ";"
		case ok && alias.Type.Inline == nil:
			return "typedef " + typeSource(alias.Type) + " " + identSource(d.Name) + ";"
		}
		return unhandledStmt
	case *Import:
		return "import " + d.Package + "::" + d.Member + ";"
	}
	return unhandledStmt
}

// declaratorSource is "name dims [= init]".
func declaratorSource(v *Variable) string {
	out := identSource(v.Name) + dimsSource(v.UnpackedDims)
	switch {
	case v.InitialExpr != nil:
		out += " = " + exprSource(v.InitialExpr)
	case len(v.Initial) > 0:
		out += " = " + tokenSource(v.Initial)
	}
	return out
}
