package ast

import (
	"strings"

	"github.com/jfetkotto/svparse/preprocessor"
)

// ExprString renders e as SystemVerilog source that parses back to an
// equivalent tree. Tokens are always separated by spaces ("- - a", never
// "--a") so re-lexing cannot merge two operators into a different one.
//
// It prints the tree as given: parentheses appear only where the tree has a
// Paren node. A tree from the parser is already consistent with operator
// precedence, so it round-trips; a hand-built one whose nesting contradicts
// precedence prints source that parses differently.
//
// Only the information the tree keeps is reproduced: a BadExpr prints its
// tokens verbatim, and shorthands that parse to the same tree print in one
// canonical spelling (".n" as ".n()", "new" and "new()" both as "new").
func ExprString(e Expr) string {
	if e == nil {
		return ""
	}
	return exprSource(e)
}

func exprSource(e Expr) string {
	switch e := e.(type) {
	case *Ident:
		return identSource(e.Name)
	case *Literal:
		return e.Text
	case *ScopedName:
		return exprSource(e.Scope) + "::" + identSource(e.Name)
	case *ParameterizedName:
		s := identSource(e.Name)
		if e.Scope != nil {
			s = exprSource(e.Scope) + "::" + s
		}
		return s + overridesSource(e.Overrides)
	case *MemberSelect:
		return exprSource(e.X) + "." + identSource(e.Member)
	case *Index:
		return exprSource(e.X) + "[" + exprSource(e.Index) + "]"
	case *RangeSelect:
		return exprSource(e.X) + "[" + exprSource(e.Left) + " " + e.Kind.String() + " " + exprSource(e.Right) + "]"
	case *Paren:
		return "(" + exprSource(e.X) + ")"
	case *Unary:
		return e.Op + " " + exprSource(e.X)
	case *Binary:
		return exprSource(e.X) + " " + e.Op + " " + exprSource(e.Y)
	case *Conditional:
		return exprSource(e.Cond) + " ? " + exprSource(e.Then) + " : " + exprSource(e.Else)
	case *Inside:
		return exprSource(e.X) + " inside {" + exprListSource(e.Ranges) + "}"
	case *ValueRange:
		return "[" + exprSource(e.Lo) + " : " + exprSource(e.Hi) + "]"
	case *Concat:
		return "{" + exprListSource(e.Elems) + "}"
	case *Replicate:
		return "{" + exprSource(e.Count) + " {" + exprListSource(e.Elems) + "}}"
	case *AssignPattern:
		s := "'{" + exprListSource(e.Items) + "}"
		if e.Type != nil {
			s = exprSource(e.Type) + s
		}
		return s
	case *PatternItem:
		if e.IsDefault {
			return "default : " + exprSource(e.Value)
		}
		return exprSource(e.Key) + " : " + exprSource(e.Value)
	case *Cast:
		return exprSource(e.Type) + "'(" + exprSource(e.X) + ")"
	case *Call:
		s := exprSource(e.Func) + "(" + exprListSource(e.Args) + ")"
		if e.With != nil {
			s += " with " + exprSource(e.With)
		}
		return s
	case *NamedArg:
		return "." + identSource(e.Name) + "(" + ExprString(e.Value) + ")"
	case *Empty:
		return ""
	case *New:
		if e.Copy != nil {
			return "new " + exprSource(e.Copy)
		}
		s := "new"
		if e.Scope != nil {
			s = exprSource(e.Scope) + "::new"
		}
		if len(e.Args) > 0 {
			s += "(" + exprListSource(e.Args) + ")"
		}
		return s
	case *NewArray:
		s := "new[" + exprSource(e.Size) + "]"
		if e.Init != nil {
			s += "(" + exprSource(e.Init) + ")"
		}
		return s
	case *This:
		return "this"
	case *Super:
		return "super"
	case *MinTypMax:
		return exprSource(e.Min) + " : " + exprSource(e.Typ) + " : " + exprSource(e.Max)
	case *Streaming:
		s := "{" + e.Op + " "
		if e.SliceSize != nil {
			s += exprSource(e.SliceSize) + " "
		}
		return s + "{" + exprListSource(e.Elems) + "}}"
	case *WithRange:
		sel := exprSource(e.Left)
		if e.Right != nil {
			sel += " " + e.Kind.String() + " " + exprSource(e.Right)
		}
		return exprSource(e.X) + " with [" + sel + "]"
	case *Tagged:
		s := "tagged " + identSource(e.Member)
		if e.X != nil {
			s += " " + exprSource(e.X)
		}
		return s
	case *TypeRef:
		return "type(" + exprSource(e.X) + ")"
	case *TypeExpr:
		return typeSource(e.Type)
	case *TypeOrExpr:
		return exprSource(e.X)
	case *IncDec:
		if e.Prefix {
			return e.Op + " " + exprSource(e.X)
		}
		return exprSource(e.X) + " " + e.Op
	case *AssignExpr:
		return exprSource(e.LHS) + " " + e.Op + " " + exprSource(e.RHS)
	case *Dist:
		items := make([]string, len(e.Items))
		for i := range e.Items {
			items[i] = exprSource(&e.Items[i])
		}
		return exprSource(e.X) + " dist {" + strings.Join(items, ", ") + "}"
	case *DistItem:
		s := exprSource(e.Value)
		if e.Weight != nil {
			op := ":="
			if e.PerRange {
				op = ":/"
			}
			s += " " + op + " " + exprSource(e.Weight)
		}
		return s
	case *BadExpr:
		return tokenSource(e.Tokens)
	}
	return "/*unhandled " + strings.TrimPrefix(describeType(e), "*ast.") + "*/"
}

// identSource is a name as written. An escaped identifier (backslash, a+b) runs to
// the next whitespace, so it must be followed by some or it would swallow
// whatever is printed after it.
func identSource(name string) string {
	if strings.HasPrefix(name, `\`) {
		return name + " "
	}
	return name
}

func exprListSource(list []Expr) string {
	parts := make([]string, len(list))
	for i, e := range list {
		parts[i] = exprSource(e)
	}
	return strings.Join(parts, ", ")
}

// overridesSource is "#(v, .name(v))".
func overridesSource(os []ParamOverride) string {
	parts := make([]string, len(os))
	for i, o := range os {
		val := ""
		switch {
		case o.ValueExpr != nil:
			val = exprSource(o.ValueExpr)
		case len(o.Value) > 0:
			val = tokenSource(o.Value)
		}
		if o.Name != "" {
			val = "." + identSource(o.Name) + "(" + val + ")"
		}
		parts[i] = val
	}
	return "#(" + strings.Join(parts, ", ") + ")"
}

// typeSource spells a type as written: qualifier, name, sign, packed
// dimensions.
func typeSource(t Type) string {
	var parts []string
	name := t.Name
	if t.PackageQualifier != "" {
		name = t.PackageQualifier + "::" + name
	}
	if name != "" {
		parts = append(parts, name)
	}
	if len(t.ParamOverrides) > 0 {
		parts = append(parts, overridesSource(t.ParamOverrides))
	}
	if t.Signed {
		parts = append(parts, "signed")
	}
	if t.Unsigned {
		parts = append(parts, "unsigned")
	}
	if len(t.PackedDims) > 0 {
		parts = append(parts, dimsSource(t.PackedDims))
	}
	return strings.Join(parts, " ")
}

// dimsSource is "[l:r][l]...", with each bound's raw tokens printed by
// tokenSource.
func dimsSource(ds []Dim) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteByte('[')
		b.WriteString(tokenSource(d.Left))
		if d.Right != nil {
			b.WriteByte(':')
			b.WriteString(tokenSource(d.Right))
		}
		b.WriteByte(']')
	}
	return b.String()
}

// tokenSource joins raw tokens back into source text, separating two tokens
// with a space only where the original had a gap. Joining everything with
// spaces would be harmless to read but can change what lexes: "'(" is a
// cast tick, "' (" is a malformed literal.
func tokenSource(toks []preprocessor.Token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			prev := toks[i-1]
			line, char := prev.End()
			if prev.File != t.File || line != t.Line || char != t.Character {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.Text)
	}
	if n := len(toks); n > 0 && strings.HasPrefix(toks[n-1].Text, `\`) {
		// An escaped identifier runs to the next whitespace, and whatever
		// the caller prints after this span would otherwise join it.
		b.WriteByte(' ')
	}
	return b.String()
}
