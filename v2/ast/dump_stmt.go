package ast

import (
	"strconv"
	"strings"
)

// describeStmt is describe for statement nodes and the procedural items. A
// statement's line carries its expressions inline, named by role ("cond=",
// "subject=") when there can be more than one, and its nested statements and
// declarations are children, one per line:
//
//	(if cond=(== a 1)          children: the then branch, then the else branch if any
//	(case casez subject=x      children: the items
//	(for init=[(= i 0)] cond=(< i 4) step=[(post++ i)]
//	                           children: init declarations if any, then the body
//	(timed @(posedge clk)      children: the statement the control applies to
//	(assign = a (+ b 1))       one line
//
// ok is false for a node that is not a statement, so describe can carry on.
func describeStmt(n Node) (head string, kids []Node, ok bool) {
	switch n := n.(type) {
	case *Block:
		h := "block begin"
		if n.Fork {
			h = "block fork" + prefixed(" ", n.Join)
		}
		h += prefixed(" label=", n.Label) + prefixed(" end=", n.EndLabel)
		return h, concat(decls(n.Decls), stmts(n.Stmts...)), true
	case *Labeled:
		return "labeled " + n.Label, stmts(n.Body), true
	case *Null:
		return "null", nil, true
	case *If:
		return "if" + prefixed(" ", n.Qualifier) + " cond=" + exprText(n.Cond), stmts(n.Then, n.Else), true
	case *Case:
		h := n.Kind + prefixed(" ", n.Qualifier) + flag(n.Inside, "inside") + " subject=" + exprText(n.Subject)
		return h, values(n.Items), true
	case *CaseItem:
		if n.Default {
			return "item default", stmts(n.Body), true
		}
		return "item " + strings.Join(exprTexts(n.Exprs), " "), stmts(n.Body), true
	case *For:
		h := "for"
		if len(n.Init) > 0 {
			h += " init=[" + strings.Join(inlineStmts(n.Init), " ") + "]"
		}
		if n.Cond != nil {
			h += " cond=" + exprText(n.Cond)
		}
		if len(n.Step) > 0 {
			h += " step=[" + strings.Join(inlineStmts(n.Step), " ") + "]"
		}
		return h, concat(decls(n.InitDecls), stmts(n.Body)), true
	case *Foreach:
		vars := make([]string, len(n.Vars))
		for i, v := range n.Vars {
			vars[i] = v
			if v == "" {
				vars[i] = "_"
			}
		}
		return "foreach " + exprText(n.Array) + " vars=[" + strings.Join(vars, " ") + "]", stmts(n.Body), true
	case *While:
		return "while cond=" + exprText(n.Cond), stmts(n.Body), true
	case *DoWhile:
		return "do-while cond=" + exprText(n.Cond), stmts(n.Body), true
	case *Repeat:
		return "repeat count=" + exprText(n.Count), stmts(n.Body), true
	case *Forever:
		return "forever", stmts(n.Body), true
	case *TimingControl:
		return "timing " + timingText(n), nil, true
	case *EventTerm:
		return "event " + eventTermText(n), nil, true
	case *TimedStmt:
		return "timed " + timingText(n.Control), stmts(n.Body), true
	case *Wait:
		return "wait cond=" + exprText(n.Cond), stmts(n.Body), true
	case *WaitFork:
		return "wait-fork", nil, true
	case *WaitOrder:
		return "wait-order " + strings.Join(exprTexts(n.Events), " ") + elseOnly(n.Then, n.Else), stmts(n.Then, n.Else), true
	case *EventTrigger:
		op := "->"
		if n.Nonblocking {
			op = "->>"
		}
		h := "trigger " + op
		if n.Control != nil {
			h += " " + timingText(n.Control)
		}
		return h + " " + exprText(n.Event), nil, true
	case *Assign:
		h := "assign " + n.Op + " " + exprText(n.LHS) + " " + exprText(n.RHS)
		if n.Control != nil {
			h += " ctl=" + timingText(n.Control)
		}
		return h, nil, true
	case *ProcAssign:
		h := "proc-" + n.Kind + " " + exprText(n.LHS)
		if n.RHS != nil {
			h += " " + exprText(n.RHS)
		}
		return h, nil, true
	case *ExprStmt:
		return "expr " + exprText(n.X), nil, true
	case *Return:
		if n.Value == nil {
			return "return", nil, true
		}
		return "return " + exprText(n.Value), nil, true
	case *Break:
		return "break", nil, true
	case *Continue:
		return "continue", nil, true
	case *Disable:
		if n.Fork {
			return "disable-fork", nil, true
		}
		return "disable " + exprText(n.Target), nil, true
	case *ImmediateAssert:
		return n.Kind + prefixed(" ", n.Deferred) + " cond=" + exprText(n.Cond) + elseOnly(n.Then, n.Else), stmts(n.Then, n.Else), true
	case *RandSequence:
		return "randsequence" + prefixed(" start=", n.Start), values(n.Productions), true
	case *RandProduction:
		return "production " + n.Name + typeAttr(n.ReturnType), concat(typeKids(n.ReturnType), values(n.Args), values(n.Rules)), true
	case *RandRule:
		kids := stmts(n.Elems...)
		if n.WeightCode != nil {
			kids = append(kids, n.WeightCode)
		}
		return "rule" + prefixed(" weight=", optExprText(n.Weight)), kids, true
	case *RandItem:
		return "item " + n.Name + prefixed(" args=", strings.Join(exprTexts(n.Args), ",")), nil, true
	case *RandCode:
		return "code", concat(decls(n.Decls), stmts(n.Stmts...)), true
	case *RandIf:
		return "rs-if cond=" + exprText(n.Cond), randItems(n.Then, n.Else), true
	case *RandRepeat:
		return "rs-repeat count=" + exprText(n.Count), randItems(n.Item), true
	case *RandSeqCase:
		return "rs-case x=" + exprText(n.X), values(n.Items), true
	case *RandSeqCaseItem:
		h := "rs-case-item"
		if n.Default {
			h += " default"
		} else {
			h += " " + strings.Join(exprTexts(n.Exprs), ",")
		}
		return h, randItems(n.Item), true
	case *RandJoin:
		return "rs-join" + prefixed(" count=", optExprText(n.Count)), values(n.Items), true
	case *RandCase:
		return "randcase", values(n.Items), true
	case *RandCaseItem:
		return "item " + exprText(n.Weight), stmts(n.Body), true
	case *RawStmt:
		return "raw " + strconv.Quote(tokenText(n.Tokens)), nil, true
	case *BadStmt:
		return "bad " + strconv.Quote(tokenText(n.Tokens)), nil, true
	case *Initial:
		return "initial", stmts(n.Body), true
	case *Final:
		return "final", stmts(n.Body), true
	case *Always:
		return n.Kind, stmts(n.Body), true
	case *ConcurrentAssert:
		h := n.Kind + flag(n.Sequence, "sequence") + " spec=" + exprText(n.Spec)
		return h + elseOnly(n.Then, n.Else), stmts(n.Then, n.Else), true
	case *AssertionItem:
		return "assertion", stmts(n.Body), true
	case *PropertyDecl:
		return "property " + n.Name + " body=" + exprText(n.Body), concat(values(n.Ports), decls(n.Decls)), true
	case *SequenceDecl:
		return "sequence " + n.Name + " body=" + exprText(n.Body), concat(values(n.Ports), decls(n.Decls)), true
	case *GenerateRegion:
		return "generate", decls(n.Body), true
	case *GenerateBlock:
		return "gen-block" + prefixed(" ", n.Label) + flag(n.Bare, "bare"), decls(n.Body), true
	case *GenerateIf:
		kids := []Node{n.Then}
		if n.Else != nil {
			kids = append(kids, n.Else)
		}
		return "gen-if cond=" + exprText(n.Cond), kids, true
	case *GenerateFor:
		return "gen-for" + flag(n.Genvar, "genvar") + " " + n.Var + " init=" + exprText(n.Init) + " cond=" + exprText(n.Cond), concat(stmts(n.Step), []Node{n.Body}), true
	case *GenerateCase:
		return "gen-case " + exprText(n.Expr), values(n.Items), true
	case *GenerateCaseItem:
		if n.Default {
			return "item default", []Node{n.Body}, true
		}
		return "item [" + strings.Join(exprTexts(n.Exprs), " ") + "]", []Node{n.Body}, true
	case *ContinuousAssign:
		h := "continuous-assign"
		if len(n.DriveStrength) > 0 {
			h += " strength=(" + strings.Join(n.DriveStrength, ",") + ")"
		}
		if len(n.Delay) > 0 {
			h += " delay=[" + strings.Join(exprTexts(n.Delay), " ") + "]"
		}
		return h, values(n.Assigns), true
	}
	return "", nil, false
}

// elseOnly marks a statement that has an else branch but no then branch, which
// the child list alone cannot show: there is then a single child, the else.
func elseOnly(then, els Stmt) string {
	if then == nil && els != nil {
		return " else-only"
	}
	return ""
}

// stmts returns the non-nil statements as children.
func stmts(list ...Stmt) []Node {
	var out []Node
	for _, s := range list {
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

// prefixed is prefix+s, or "" if s is empty.
func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

// inlineStmts renders simple statements on one line, for a for loop's head.
func inlineStmts(list []Stmt) []string {
	out := make([]string, len(list))
	for i, s := range list {
		head, kids, _ := describeStmt(s)
		out[i] = "(" + head + ")"
		if len(kids) > 0 {
			out[i] = "(" + head + " ...)"
		}
	}
	return out
}

// timingText spells a timing control: "#10", "##2", "@(posedge clk or b)",
// "@*", with a "repeat(n) " prefix for an intra-assignment repeat event control.
func timingText(c *TimingControl) string {
	if c == nil {
		return ""
	}
	var s string
	if c.Repeat != nil {
		s = "repeat(" + exprText(c.Repeat) + ") "
	}
	switch {
	case c.Delay != nil && c.CycleDelay:
		return s + "##" + exprText(c.Delay)
	case c.Delay != nil:
		return s + "#" + exprText(c.Delay)
	case c.Star:
		return s + "@*"
	}
	terms := make([]string, len(c.Events))
	for i := range c.Events {
		terms[i] = eventTermText(&c.Events[i])
	}
	return s + "@(" + strings.Join(terms, " or ") + ")"
}

func eventTermText(t *EventTerm) string {
	s := exprText(t.X)
	if t.Edge != "" {
		s = t.Edge + " " + s
	}
	if t.Iff != nil {
		s += " iff " + exprText(t.Iff)
	}
	return s
}

// randItems lists the non-nil items as children.
func randItems(items ...*RandItem) []Node {
	var out []Node
	for _, it := range items {
		if it != nil {
			out = append(out, it)
		}
	}
	return out
}

func optExprText(e Expr) string {
	if e == nil {
		return ""
	}
	return exprText(e)
}
