package ast

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jfetkotto/svparse/preprocessor"
)

// Dump renders the tree rooted at n as a compact S-expression, for golden
// tests and debugging. Each node is "(kind attrs...)" with its children one
// per line, indented two spaces; a node with no children is a single line.
//
// Positions are left out so a golden file survives reformatting the sample
// source; use DumpWithPositions to see them. Raw expression spans (a default
// value, a dimension bound) print as a quoted string of their token texts
// joined by single spaces, so they are insensitive to the source's own
// whitespace too.
func Dump(n Node) string {
	return dump(n, false)
}

// DumpWithPositions is Dump with each node's span appended as
// "@line:char-endline:endchar", as stored (zero-based lines, UTF-16 columns).
func DumpWithPositions(n Node) string {
	return dump(n, true)
}

func dump(n Node, positions bool) string {
	var b strings.Builder
	d := &dumper{b: &b, positions: positions}
	d.node(n, 0)
	b.WriteByte('\n')
	return b.String()
}

type dumper struct {
	b         *strings.Builder
	positions bool
}

// node writes n and its subtree, starting at the current column (the caller
// has already written any indentation) and nested depth levels deep.
func (d *dumper) node(n Node, depth int) {
	if e, ok := n.(Expr); ok {
		// Expressions print inline, see exprText.
		d.b.WriteString(exprText(e))
		if d.positions {
			p := e.Pos()
			fmt.Fprintf(d.b, " @%d:%d-%d:%d", p.Line, p.Character, p.EndLine, p.EndCharacter)
		}
		return
	}
	head, kids := describe(n)
	d.b.WriteByte('(')
	d.b.WriteString(head)
	if d.positions {
		p := n.Pos()
		fmt.Fprintf(d.b, " @%d:%d-%d:%d", p.Line, p.Character, p.EndLine, p.EndCharacter)
	}
	for _, k := range kids {
		d.b.WriteByte('\n')
		d.b.WriteString(strings.Repeat("  ", depth+1))
		d.node(k, depth+1)
	}
	d.b.WriteByte(')')
}

// describe returns n's one-line description (everything after the opening
// parenthesis) and the children printed beneath it. Kept as an explicit
// switch rather than reflection so the output format is a deliberate,
// reviewable choice per node; the corpus test in walk_corpus_test.go fails
// if a node type is added without a case here (it would print "(UNHANDLED T)").
func describe(n Node) (head string, kids []Node) {
	switch n := n.(type) {
	case *File:
		return "file " + strconv.Quote(n.Path), decls(n.Decls)
	case *Container:
		return containerKindName(n.ContainerKind) + " " + n.Name, concat(values(n.Params), values(n.Ports), decls(n.Body))
	case *Class:
		h := "class " + n.Name
		h += flag(n.Virtual, "virtual") + flag(n.IsInterfaceClass, "interface")
		if len(n.ExtendsTypes) > 0 {
			h += " extends=" + strings.Join(typeStrings(n.ExtendsTypes), ", ")
		} else if n.Extends != "" {
			h += " extends=" + n.Extends
		}
		if len(n.ExtendsArgExprs) > 0 {
			h += " extends-args=[" + strings.Join(exprTexts(n.ExtendsArgExprs), " ") + "]"
		} else if len(n.ExtendsArgs) > 0 {
			h += " extends-args=" + span(n.ExtendsArgs)
		}
		if len(n.ImplementsTypes) > 0 {
			h += " implements=" + strings.Join(typeStrings(n.ImplementsTypes), ", ")
		} else if len(n.Implements) > 0 {
			h += " implements=" + strings.Join(n.Implements, ",")
		}
		return h, concat(values(n.Params), decls(n.Body))
	case *Package:
		return "package " + n.Name, decls(n.Body)
	case *Constraint:
		return "constraint " + n.Name + flag(n.Prototype, "prototype"), items(n.Items)
	case *ConstraintExpr:
		return "constraint-expr" + flag(n.Soft, "soft") + " " + exprText(n.X), nil
	case *ConstraintImplies:
		return "implies " + exprText(n.Cond), items([]ConstraintItem{n.Body})
	case *ConstraintIf:
		return "constraint-if " + exprText(n.Cond), items([]ConstraintItem{n.Then, n.Else})
	case *ConstraintForeach:
		return "constraint-foreach " + exprText(n.Array) + " [" + strings.Join(n.Vars, ",") + "]", items([]ConstraintItem{n.Body})
	case *ConstraintSolve:
		return "solve [" + strings.Join(exprTexts(n.Solve), " ") + "] before [" + strings.Join(exprTexts(n.Before), " ") + "]", nil
	case *ConstraintDisableSoft:
		return "disable-soft " + exprText(n.X), nil
	case *ConstraintUnique:
		return "unique [" + strings.Join(exprTexts(n.Exprs), " ") + "]", nil
	case *ConstraintSet:
		return "constraint-set", items(n.Items)
	case *ConstraintRaw:
		return "constraint-raw " + strconv.Quote(tokenText(n.Tokens)), nil
	case *Import:
		return "import " + n.Package + "::" + n.Member, nil
	case *Variable:
		h := "variable " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims)
		if n.NetQualifier != "" {
			h += " " + n.NetQualifier
		}
		if len(n.DriveStrength) > 0 {
			h += " strength=(" + strings.Join(n.DriveStrength, ",") + ")"
		}
		if len(n.Delay) > 0 {
			h += " delay=[" + strings.Join(exprTexts(n.Delay), " ") + "]"
		}
		h += valueAttr(n.InitialExpr, n.Initial)
		h += flag(n.IsRand, "rand") + flag(n.IsRandC, "randc") + flag(n.IsStatic, "static")
		return h, typeKids(n.Type)
	case *Parameter:
		kw := "parameter"
		if n.IsLocal {
			kw = "localparam"
		}
		if n.IsSpecparam {
			kw = "specparam"
		}
		return kw + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.DefaultExpr, n.Default), typeKids(n.Type)
	case *Nettype:
		h := "nettype " + n.Name + typeAttr(n.Type)
		if n.ResolveFunc != "" {
			h += " with=" + n.ResolveFunc
		}
		return h, typeKids(n.Type)
	case *Typedef:
		if n.Underlying == nil {
			return "typedef " + n.Name + " forward" + forwardSuffix(n.ForwardKind), nil
		}
		return "typedef " + n.Name, []Node{n.Underlying}
	case *TypeAlias:
		return "type-alias" + typeAttr(n.Type), typeKids(n.Type)
	case *Struct:
		return "struct" + flag(n.Packed, "packed"), values(n.Members)
	case *Union:
		return "union" + flag(n.Tagged, "tagged") + flag(n.Packed, "packed"), values(n.Members)
	case *Enum:
		return "enum" + typeAttr(n.BaseType), values(n.Members)
	case *EnumMember:
		return "enum-member " + n.Name + valueAttr(n.ValueExpr, n.Value), nil
	case *Port:
		return "port" + direction(n.Direction) + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.DefaultExpr, n.Default), typeKids(n.Type)
	case *PortDecl:
		return "port-decl" + direction(n.Direction) + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims), typeKids(n.Type)
	case *Modport:
		return "modport " + n.Name, values(n.Ports)
	case *ModportPort:
		return "modport-port" + direction(n.Direction) + " " + n.Name, nil
	case *Arg:
		return "arg" + direction(n.Direction) + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.DefaultExpr, n.Default), typeKids(n.Type)
	case *Function:
		return "function " + n.Name + flag(n.Prototype, "prototype") + typeAttr(n.ReturnType),
			concat(typeKids(n.ReturnType), values(n.Args), decls(n.Decls), stmts(n.Stmts...))
	case *Task:
		return "task " + n.Name + flag(n.Prototype, "prototype"),
			concat(values(n.Args), decls(n.Decls), stmts(n.Stmts...))
	case *Type:
		return "type " + typeString(*n), typeKids(*n)
	case *Instantiation:
		return "instantiation " + n.ModuleType, concat(values(n.ParamOverrides), values(n.Instances))
	case *ParamOverride:
		return strings.TrimSpace("param-override " + overrideText(*n)), nil
	case *Instance:
		return "instance " + n.Name + dims(n.UnpackedDims), values(n.Connections)
	case *GateInstantiation:
		h := "gate " + n.Gate
		if len(n.DriveStrength) > 0 {
			h += " strength=(" + strings.Join(n.DriveStrength, ",") + ")"
		}
		if len(n.Delay) > 0 {
			h += " delay=[" + strings.Join(exprTexts(n.Delay), " ") + "]"
		}
		return h, values(n.Instances)
	case *GateInstance:
		name := n.Name
		if name == "" {
			name = "_"
		}
		return "gate-instance " + name + dims(n.UnpackedDims) + " (" + strings.Join(exprTexts(n.Terminals), " ") + ")", nil
	case *UDP:
		return "udp " + n.Name + flag(n.Sequential, "sequential"), concat(values(n.Ports), decls(n.Body), values(n.Rows))
	case *UDPRow:
		h := "row " + strings.Join(n.Inputs, " ")
		if n.Current != "" {
			h += " : " + n.Current
		}
		return h + " : " + n.Next, nil
	case *TimeUnit:
		h := n.Keyword + " " + n.Value
		if n.Divisor != "" {
			h += " / " + n.Divisor
		}
		return h, nil
	case *Defparam:
		return "defparam", values(n.Assigns)
	case *Bind:
		h := "bind " + n.Target
		if len(n.Instances) > 0 {
			h += " : " + strings.Join(n.Instances, ",")
		}
		var kids []Node
		if n.Inst != nil {
			kids = append(kids, n.Inst)
		}
		return h, kids
	case *Let:
		return "let " + n.Name + " = " + exprText(n.Body), values(n.Ports)
	case *Alias:
		return "alias [" + strings.Join(exprTexts(n.Nets), " ") + "]", nil
	case *Export:
		return "export " + n.Package + "::" + n.Member, nil
	case *DPIExport:
		h := "dpi-export " + strconv.Quote(n.Spec)
		if n.CName != "" {
			h += " " + n.CName + " ="
		}
		return h + " " + n.Kind + " " + n.Name, nil
	case *DefaultClocking:
		return "default-clocking " + n.Name, nil
	case *DefaultDisable:
		return "default-disable-iff " + exprText(n.Cond), nil
	case *Clocking:
		h := "clocking " + n.Name + flag(n.Default, "default") + flag(n.Global, "global")
		var kids []Node
		if n.Event != nil {
			kids = append(kids, n.Event)
		}
		return h, concat(kids, values(n.Items))
	case *ClockingItem:
		if n.Default {
			return "clocking-default" + prefixed(" input ", n.InputSkew) + prefixed(" output ", n.OutputSkew), nil
		}
		return "clocking-item " + n.Direction + prefixed(" in-skew=", n.InputSkew) + prefixed(" out-skew=", n.OutputSkew), values(n.Signals)
	case *ClockingSignal:
		if n.Expr != nil {
			return "clocking-signal " + n.Name + " = " + exprText(n.Expr), nil
		}
		return "clocking-signal " + n.Name, nil
	case *Config:
		return "config " + n.Name + " design=[" + strings.Join(n.Design, " ") + "]", concat(decls(n.Body), values(n.Rules))
	case *ConfigRule:
		return "rule " + n.Kind + " " + strconv.Quote(n.Text), nil
	case *Covergroup:
		h := "covergroup " + n.Name
		if n.Extends != "" {
			h += " extends=" + n.Extends
		}
		if n.Event != nil {
			h += " event=" + timingText(n.Event)
		}
		h += flag(n.BlockEvent, "block-event") + flag(n.Sample, "sample")
		return h, concat(values(n.Ports), values(n.SamplePorts), decls(n.Items))
	case *CoverOption:
		kw := "option"
		if n.Type {
			kw = "type_option"
		}
		return kw + "." + n.Name + " = " + exprText(n.Value), nil
	case *CoverPoint:
		h := "coverpoint"
		if n.Name != "" {
			h += " " + n.Name
		}
		h += typeAttr(n.Type) + " " + exprText(n.X)
		if n.Iff != nil {
			h += " iff=" + exprText(n.Iff)
		}
		return h, decls(n.Bins)
	case *CoverBin:
		h := n.Keyword + flag(n.Wildcard, "wildcard")
		if n.Name != "" {
			h += " " + n.Name
		}
		if n.Array {
			h += "[" + optText(n.Size) + "]"
		}
		h += " " + n.Kind
		switch n.Kind {
		case "values":
			h += " {" + strings.Join(exprTexts(n.Values), " ") + "}"
		}
		if n.Source != nil {
			h += " " + exprText(n.Source)
		}
		if n.With != nil {
			h += " with=" + exprText(n.With)
		}
		if n.Iff != nil {
			h += " iff=" + exprText(n.Iff)
		}
		return h, values(n.Trans)
	case *TransSet:
		return "trans", values(n.Steps)
	case *TransStep:
		h := "step [" + strings.Join(exprTexts(n.Values), " ") + "]"
		if n.RepKind != "" {
			h += " repeat" + n.RepKind
			if n.RepMin != nil {
				h += " " + exprText(n.RepMin)
				if n.RepMax != nil {
					h += ":" + exprText(n.RepMax)
				}
			}
		}
		return h, nil
	case *CoverCross:
		h := "cross"
		if n.Name != "" {
			h += " " + n.Name
		}
		h += " [" + strings.Join(exprTexts(n.Items), " ") + "]"
		if n.Iff != nil {
			h += " iff=" + exprText(n.Iff)
		}
		return h, decls(n.Body)
	case *PortConnection:
		switch {
		case n.Wildcard:
			return "connection .*", nil
		case n.Implicit:
			return "connection ." + n.Name + " implicit", nil
		}
		if n.ConnExpr != nil {
			return "connection ." + n.Name + " " + exprText(n.ConnExpr), nil
		}
		return "connection" + namedValue(n.Name, n.Expr), nil
	}
	if h, kids, ok := describeStmt(n); ok {
		return h, kids
	}
	// Unreachable while every Node type has a case above; surfaces a missed
	// one loudly in goldens instead of silently dropping it. The marker is
	// capitalised so it can never collide with an operator head like "?:".
	return fmt.Sprintf("UNHANDLED %T", n), nil
}

// forwardSuffix is "=struct" for a typed forward declaration, "" for a bare one.
func forwardSuffix(kind string) string {
	if kind == "" {
		return ""
	}
	return "=" + kind
}

func containerKindName(k ContainerKind) string {
	switch k {
	case KindModule:
		return "module"
	case KindInterface:
		return "interface"
	case KindProgram:
		return "program"
	}
	return "container?" + strconv.Itoa(int(k))
}

func decls(list []Decl) []Node {
	out := make([]Node, 0, len(list))
	for _, d := range list {
		out = append(out, d)
	}
	return out
}

// values returns pointers to a value-stored slice's elements, matching what
// Walk hands a visitor.
func values[T any, P interface {
	*T
	Node
}](list []T) []Node {
	out := make([]Node, 0, len(list))
	for i := range list {
		out = append(out, P(&list[i]))
	}
	return out
}

func concat(lists ...[]Node) []Node {
	var out []Node
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

func flag(on bool, word string) string {
	if on {
		return " " + word
	}
	return ""
}

func direction(dir Direction) string {
	if dir == DirUnspecified {
		return ""
	}
	return " " + string(dir)
}

// typeAttr is " <type>" for a written type and nothing for an absent one.
func typeAttr(t Type) string {
	if t.isZero() {
		return ""
	}
	return " <" + typeString(t) + ">"
}

// typeString spells t roughly as it would be written: "pkg::name",
// "iface.modport", "#(...)" overrides, sign, then packed dimensions.
func typeString(t Type) string {
	var parts []string
	name := t.Name + aggregateQualifiers(t.Inline)
	if t.TypeRef != nil {
		name = exprText(t.TypeRef)
	}
	if t.PackageQualifier != "" {
		name = t.PackageQualifier + overridesText(t.QualifierOverrides) + "::" + name
	}
	if t.Modport != "" {
		name += "." + t.Modport
	}
	if name != "" {
		parts = append(parts, name)
	}
	if len(t.ParamOverrides) > 0 {
		parts = append(parts, overridesText(t.ParamOverrides))
	}
	if t.Signed {
		parts = append(parts, "signed")
	}
	if t.Unsigned {
		parts = append(parts, "unsigned")
	}
	if len(t.PackedDims) > 0 {
		parts = append(parts, strings.TrimPrefix(dims(t.PackedDims), " "))
	}
	if t.Virtual {
		parts = append([]string{"virtual"}, parts...)
	}
	return strings.Join(parts, " ")
}

// overridesText is "#(a, .n v)" for a parameter value list, "" for none.
func overridesText(os []ParamOverride) string {
	if len(os) == 0 {
		return ""
	}
	texts := make([]string, len(os))
	for i, o := range os {
		texts[i] = overrideText(o)
	}
	return "#(" + strings.Join(texts, ", ") + ")"
}

func typeStrings(ts []Type) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = typeString(t)
	}
	return out
}

// typeKids is the anonymous aggregate a type is written as, which hangs as a
// child of whatever declares the type (the type attribute itself can only
// name it: "<struct packed>").
func typeKids(t Type) []Node {
	if t.Inline != nil {
		return []Node{t.Inline}
	}
	return nil
}

// aggregateQualifiers is " tagged packed" and the like for an inline
// aggregate, so the type attribute reads "struct packed" rather than "struct".
func aggregateQualifiers(inline Decl) string {
	switch d := inline.(type) {
	case *Struct:
		return flag(d.Packed, "packed")
	case *Union:
		return flag(d.Tagged, "tagged") + flag(d.Packed, "packed")
	}
	return ""
}

// dims is " [l:r] [l] [] [*]" for a dimension list, empty for none, using each
// bound's parsed expression when there is one.
func dims(ds []Dim) string {
	var b strings.Builder
	for _, dim := range ds {
		b.WriteString(" [")
		switch {
		case dim.Wildcard:
			b.WriteByte('*')
		default:
			b.WriteString(boundText(dim.LeftExpr, dim.Left))
			if dim.Right != nil {
				b.WriteByte(':')
				b.WriteString(boundText(dim.RightExpr, dim.Right))
			}
		}
		b.WriteByte(']')
	}
	return b.String()
}

// boundText is one dimension bound: the parsed expression if present, else
// its raw tokens.
func boundText(e Expr, raw []preprocessor.Token) string {
	if e != nil {
		return exprText(e)
	}
	return tokenText(raw)
}

// valueAttr is " = value" for a variable's initializer, a default, or an enum
// value: the parsed expression if present, else the raw span quoted.
func valueAttr(e Expr, raw []preprocessor.Token) string {
	if e != nil {
		return " = " + exprText(e)
	}
	return initial(raw)
}

func initial(toks []preprocessor.Token) string {
	if len(toks) == 0 {
		return ""
	}
	return " = " + span(toks)
}

// namedValue is " .name "value"" with either half omitted when absent.
func namedValue(name string, toks []preprocessor.Token) string {
	var s string
	if name != "" {
		s += " ." + name
	}
	if len(toks) > 0 {
		s += " " + span(toks)
	}
	return s
}

func span(toks []preprocessor.Token) string {
	return strconv.Quote(tokenText(toks))
}

func tokenText(toks []preprocessor.Token) string {
	texts := make([]string, len(toks))
	for i, t := range toks {
		texts[i] = t.Text
	}
	return strings.Join(texts, " ")
}

// items returns the non-nil constraint items as children.
func items(list []ConstraintItem) []Node {
	var out []Node
	for _, it := range list {
		if it != nil {
			out = append(out, it)
		}
	}
	return out
}
