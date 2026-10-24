package ast

import (
	"fmt"
	"strconv"
	"strings"
)

// exprText renders an expression as a single-line S-expression. Expressions
// are small and read best inline in a golden file, so unlike declarations
// they do not get one child per line:
//
//	a                      identifier or literal, as written
//	(+ a (* b c))          binary operator application
//	(unary - a)            prefix operator, distinct from binary "-"
//	(paren x)  (?: c t e)  (index x i)  (range+: x l r)  (. x member)  (:: pkg name)
//	(call f a .n(v) with c)  (cast T x)  (concat a b)  (repeat n a b)
//	<logic [7:0]>          a data type in expression position
//
// A node with no case here prints as "(UNHANDLED T)", which the corpus tests treat as
// a failure, the same as in describe.
func exprText(e Expr) string {
	switch e := e.(type) {
	case nil:
		return "<nil>"
	case *Ident:
		return e.Name
	case *Literal:
		return e.Text
	case *ScopedName:
		return sexpr("::", exprText(e.Scope), e.Name)
	case *ParameterizedName:
		name := e.Name
		if e.Scope != nil {
			name = exprText(e.Scope) + "::" + name
		}
		parts := []string{name}
		for i := range e.Overrides {
			parts = append(parts, overrideText(e.Overrides[i]))
		}
		return sexpr("param-name", parts...)
	case *MemberSelect:
		return sexpr(".", exprText(e.X), e.Member)
	case *Index:
		return sexpr("index", exprText(e.X), exprText(e.Index))
	case *RangeSelect:
		return sexpr("range"+e.Kind.String(), exprText(e.X), exprText(e.Left), exprText(e.Right))
	case *Paren:
		return sexpr("paren", exprText(e.X))
	case *Unary:
		return sexpr("unary", e.Op, exprText(e.X))
	case *Binary:
		return sexpr(e.Op, exprText(e.X), exprText(e.Y))
	case *Conditional:
		return sexpr("?:", exprText(e.Cond), exprText(e.Then), exprText(e.Else))
	case *Inside:
		return sexpr("inside", append([]string{exprText(e.X)}, exprTexts(e.Ranges)...)...)
	case *ValueRange:
		return "[" + exprText(e.Lo) + ":" + exprText(e.Hi) + "]"
	case *Concat:
		return sexpr("concat", exprTexts(e.Elems)...)
	case *Replicate:
		return sexpr("repeat", append([]string{exprText(e.Count)}, exprTexts(e.Elems)...)...)
	case *AssignPattern:
		var parts []string
		if e.Type != nil {
			parts = append(parts, exprText(e.Type)+"'")
		}
		return sexpr("pattern", append(parts, exprTexts(e.Items)...)...)
	case *PatternItem:
		key := "default"
		if !e.IsDefault {
			key = exprText(e.Key)
		}
		return sexpr("item", key, exprText(e.Value))
	case *Cast:
		return sexpr("cast", exprText(e.Type), exprText(e.X))
	case *Call:
		parts := append([]string{exprText(e.Func)}, exprTexts(e.Args)...)
		if e.With != nil {
			parts = append(parts, "with", exprText(e.With))
		}
		if e.Constraints != nil {
			parts = append(parts, exprText(e.Constraints))
		}
		return sexpr("call", parts...)
	case *ConstraintBlock:
		return sexpr("constraints", strconv.Quote(tokenText(e.Tokens)))
	case *NamedArg:
		if e.Value == nil {
			return "." + e.Name + "()"
		}
		return "." + e.Name + "(" + exprText(e.Value) + ")"
	case *Empty:
		return "(empty)"
	case *New:
		if e.Copy != nil {
			return sexpr("new-copy", exprText(e.Copy))
		}
		var parts []string
		if e.Scope != nil {
			parts = append(parts, exprText(e.Scope)+"::")
		}
		return sexpr("new", append(parts, exprTexts(e.Args)...)...)
	case *NewArray:
		parts := []string{exprText(e.Size)}
		if e.Init != nil {
			parts = append(parts, exprText(e.Init))
		}
		return sexpr("new-array", parts...)
	case *This:
		return "this"
	case *Super:
		return "super"
	case *MinTypMax:
		return sexpr("mintypmax", exprText(e.Min), exprText(e.Typ), exprText(e.Max))
	case *Streaming:
		parts := []string{e.Op}
		if e.SliceSize != nil {
			parts = append(parts, "slice="+exprText(e.SliceSize))
		}
		return sexpr("stream", append(parts, exprTexts(e.Elems)...)...)
	case *WithRange:
		sel := exprText(e.Left)
		if e.Right != nil {
			sel += e.Kind.String() + exprText(e.Right)
		}
		return sexpr("with", exprText(e.X), "["+sel+"]")
	case *Tagged:
		parts := []string{e.Member}
		if e.X != nil {
			parts = append(parts, exprText(e.X))
		}
		return sexpr("tagged", parts...)
	case *TypeRef:
		return sexpr("typeof", exprText(e.X))
	case *TypeExpr:
		return "<" + typeString(e.Type) + ">"
	case *TypeOrExpr:
		return sexpr("type-or-expr", exprText(e.X))
	case *IncDec:
		head := "post" + e.Op
		if e.Prefix {
			head = "pre" + e.Op
		}
		return sexpr(head, exprText(e.X))
	case *AssignExpr:
		return sexpr("assign", e.Op, exprText(e.LHS), exprText(e.RHS))
	case *Dist:
		parts := []string{exprText(e.X)}
		for i := range e.Items {
			parts = append(parts, distItemText(&e.Items[i]))
		}
		return sexpr("dist", parts...)
	case *DistItem:
		return distItemText(e)
	case *SeqDelay:
		return sexpr("##", optText(e.X), delayRangeText(e.Min, e.Max, e.Sugar), exprText(e.Y))
	case *SeqRepeat:
		return sexpr("["+e.Kind+"]", exprText(e.X), delayRangeText(e.Min, e.Max, e.Sugar))
	case *PropPrefix:
		parts := []string{}
		if e.Arg != nil {
			a := exprText(e.Arg)
			if e.Range {
				a = "[" + a + ":" + exprText(e.Max) + "]"
			}
			parts = append(parts, a)
		}
		parts = append(parts, exprText(e.X))
		return sexpr(e.Op, append(parts, exprTexts(e.Items)...)...)
	case *PropIf:
		return sexpr("prop-if", exprText(e.Cond), exprText(e.Then), optText(e.Else))
	case *PropCase:
		parts := []string{exprText(e.Expr)}
		for i := range e.Items {
			parts = append(parts, propCaseItemText(&e.Items[i]))
		}
		return sexpr("prop-case", parts...)
	case *ClockedProp:
		return sexpr("clocked", timingText(e.Event), exprText(e.X))
	case *DisableIff:
		return sexpr("disable-iff", exprText(e.Cond), exprText(e.X))
	case *SeqMatch:
		return sexpr("match", append([]string{exprText(e.X)}, exprTexts(e.Items)...)...)
	case *BadExpr:
		return sexpr("bad", strconv.Quote(tokenText(e.Tokens)))
	}
	return fmt.Sprintf("(UNHANDLED %T)", e)
}

// describeType names e's dynamic type for the fallback markers.
func describeType(e Expr) string { return fmt.Sprintf("%T", e) }

func distItemText(d *DistItem) string {
	if d.Weight == nil {
		return exprText(d.Value)
	}
	op := ":="
	if d.PerRange {
		op = ":/"
	}
	return sexpr(op, exprText(d.Value), exprText(d.Weight))
}

func exprTexts(list []Expr) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = exprText(e)
	}
	return out
}

// sexpr is "(head part part ...)", skipping empty parts.
func sexpr(head string, parts ...string) string {
	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(head)
	for _, p := range parts {
		if p != "" {
			b.WriteByte(' ')
			b.WriteString(p)
		}
	}
	b.WriteByte(')')
	return b.String()
}

// overrideText is one "#( ... )" entry: ".name value" or just "value", using
// the parsed expression when present and the raw span otherwise.
func overrideText(o ParamOverride) string {
	var parts []string
	if o.Name != "" {
		parts = append(parts, "."+o.Name)
	}
	switch {
	case o.ValueExpr != nil:
		parts = append(parts, exprText(o.ValueExpr))
	case len(o.Value) > 0:
		parts = append(parts, span(o.Value))
	}
	return strings.Join(parts, " ")
}

// optText is exprText for an optional child: "_" when absent.
func optText(e Expr) string {
	if e == nil {
		return "_"
	}
	return exprText(e)
}

// delayRangeText spells a cycle-delay or repetition bound: "[*]", "[+]",
// "N" or "[lo:hi]".
func delayRangeText(min, max Expr, sugar string) string {
	switch {
	case sugar != "":
		return "[" + sugar + "]"
	case max != nil:
		return "[" + exprText(min) + ":" + exprText(max) + "]"
	}
	return exprText(min)
}

func propCaseItemText(e *PropCaseItem) string {
	key := "default"
	if !e.Default {
		key = "[" + strings.Join(exprTexts(e.Exprs), " ") + "]"
	}
	return sexpr("item", key, exprText(e.Body))
}
