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
		if n.Extends != "" {
			h += " extends=" + n.Extends
		}
		if len(n.ExtendsArgExprs) > 0 {
			h += " extends-args=[" + strings.Join(exprTexts(n.ExtendsArgExprs), " ") + "]"
		} else if len(n.ExtendsArgs) > 0 {
			h += " extends-args=" + span(n.ExtendsArgs)
		}
		if len(n.Implements) > 0 {
			h += " implements=" + strings.Join(n.Implements, ",")
		}
		return h, concat(values(n.Params), decls(n.Body))
	case *Package:
		return "package " + n.Name, decls(n.Body)
	case *Constraint:
		return "constraint " + n.Name + flag(n.Prototype, "prototype"), nil
	case *Import:
		return "import " + n.Package + "::" + n.Member, nil
	case *Variable:
		h := "variable " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.InitialExpr, n.Initial)
		h += flag(n.IsRand, "rand") + flag(n.IsRandC, "randc") + flag(n.IsStatic, "static")
		return h, nil
	case *Parameter:
		kw := "parameter"
		if n.IsLocal {
			kw = "localparam"
		}
		return kw + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.DefaultExpr, n.Default), nil
	case *Typedef:
		if n.Underlying == nil {
			return "typedef " + n.Name + " forward", nil
		}
		return "typedef " + n.Name, []Node{n.Underlying}
	case *TypeAlias:
		return "type-alias" + typeAttr(n.Type), nil
	case *Struct:
		return "struct" + flag(n.Packed, "packed"), values(n.Members)
	case *Union:
		return "union" + flag(n.Packed, "packed"), values(n.Members)
	case *Enum:
		return "enum" + typeAttr(n.BaseType), values(n.Members)
	case *EnumMember:
		return "enum-member " + n.Name + valueAttr(n.ValueExpr, n.Value), nil
	case *Port:
		return "port" + direction(n.Direction) + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.DefaultExpr, n.Default), nil
	case *PortDecl:
		return "port-decl" + direction(n.Direction) + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims), nil
	case *Modport:
		return "modport " + n.Name, values(n.Ports)
	case *ModportPort:
		return "modport-port" + direction(n.Direction) + " " + n.Name, nil
	case *Arg:
		return "arg" + direction(n.Direction) + " " + n.Name + typeAttr(n.Type) + dims(n.UnpackedDims) + valueAttr(n.DefaultExpr, n.Default), nil
	case *Function:
		return "function " + n.Name + flag(n.Prototype, "prototype") + typeAttr(n.ReturnType), values(n.Args)
	case *Task:
		return "task " + n.Name + flag(n.Prototype, "prototype"), values(n.Args)
	case *Type:
		return "type " + typeString(*n), nil
	case *Instantiation:
		return "instantiation " + n.ModuleType, concat(values(n.ParamOverrides), values(n.Instances))
	case *ParamOverride:
		return strings.TrimSpace("param-override " + overrideText(*n)), nil
	case *Instance:
		return "instance " + n.Name + dims(n.UnpackedDims), values(n.Connections)
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
	// Unreachable while every Node type has a case above; surfaces a missed
	// one loudly in goldens instead of silently dropping it. The marker is
	// capitalised so it can never collide with an operator head like "?:".
	return fmt.Sprintf("UNHANDLED %T", n), nil
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
	name := t.Name
	if t.PackageQualifier != "" {
		name = t.PackageQualifier + "::" + name
	}
	if t.Modport != "" {
		name += "." + t.Modport
	}
	if name != "" {
		parts = append(parts, name)
	}
	if len(t.ParamOverrides) > 0 {
		var os []string
		for _, o := range t.ParamOverrides {
			os = append(os, overrideText(o))
		}
		parts = append(parts, "#("+strings.Join(os, ", ")+")")
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
	return strings.Join(parts, " ")
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
