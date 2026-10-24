package parser

import (
	"reflect"
	"slices"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// ownTypes gives every Type in f its own copies of the parts that
// declarators of one statement share, so the tree stays a tree.
//
// "struct {int x;} a, b;" and "cls #(8) c, d;" produce a Variable per name,
// each holding a copy of the same Type value: a copy shares its slices and its
// Inline pointer with the others. Left alone, Walk would visit the shared
// aggregate or override once per declarator, and a fill that writes into it
// would hang one set of expression nodes under every owner.
//
// The first Type to claim an Inline aggregate keeps it; each later one gets a
// deep copy, which keeps the original's Position. Two declarators therefore
// have equal-looking but distinct aggregates, and "same Position" is how to
// tell they were written as one type.
func ownTypes(root ast.Node) {
	claimed := make(map[ast.Decl]bool)
	claimedRefs := make(map[*ast.TypeRef]bool)
	ast.Inspect(root, func(n ast.Node) bool {
		t, ok := n.(*ast.Type)
		if !ok {
			return true
		}
		if len(t.ParamOverrides) > 0 {
			t.ParamOverrides = slices.Clone(t.ParamOverrides)
		}
		if len(t.QualifierOverrides) > 0 {
			t.QualifierOverrides = slices.Clone(t.QualifierOverrides)
		}
		if t.Inline != nil {
			if claimed[t.Inline] {
				t.Inline = cloneAggregate(t.Inline)
			}
			claimed[t.Inline] = true
		}
		if t.TypeRef != nil {
			if claimedRefs[t.TypeRef] {
				t.TypeRef, _ = cloneExpr(t.TypeRef).(*ast.TypeRef)
			}
			claimedRefs[t.TypeRef] = true
		}
		return true
	})
}

// cloneAggregate deep-copies an anonymous struct, union, or enum, including
// aggregates nested in its members. It runs before fillExprs, so only raw
// token spans (never mutated, so safe to share) need no copying.
func cloneAggregate(d ast.Decl) ast.Decl {
	switch d := d.(type) {
	case *ast.Struct:
		c := *d
		c.Members = cloneMembers(d.Members)
		return &c
	case *ast.Union:
		c := *d
		c.Members = cloneMembers(d.Members)
		return &c
	case *ast.Enum:
		c := *d
		c.BaseType = cloneType(d.BaseType)
		c.Members = slices.Clone(d.Members)
		return &c
	}
	return d
}

func cloneMembers(ms []ast.Variable) []ast.Variable {
	out := slices.Clone(ms)
	for i := range out {
		out[i].Type = cloneType(out[i].Type)
	}
	return out
}

func cloneType(t ast.Type) ast.Type {
	t.ParamOverrides = slices.Clone(t.ParamOverrides)
	t.QualifierOverrides = slices.Clone(t.QualifierOverrides)
	if t.TypeRef != nil {
		t.TypeRef, _ = cloneExpr(t.TypeRef).(*ast.TypeRef)
	}
	if t.Inline != nil {
		t.Inline = cloneAggregate(t.Inline)
	}
	return t
}

// fillExprs populates the parsed-expression sibling of every raw expression
// span in f: Variable.InitialExpr from Initial, Dim.LeftExpr from Dim.Left,
// and so on (see the field docs in package ast). The raw spans are left
// untouched.
//
// It is a pass over the finished declaration tree rather than a change to each
// place the declaration parser captures a span. The declaration parser still
// finds the span boundaries exactly as before, so no existing recovery
// behaviour moves; this pass only interprets what was captured.
//
// Expression errors are deliberately NOT reported. Until now an expression
// was never inspected, so a malformed one never produced a diagnostic;
// reporting them is a separate, deliberate change with its own baseline
// update. A span that does not parse completely and cleanly becomes a
// *ast.BadExpr over the whole span, so no tokens are silently dropped from the
// tree and a consumer can still tell the span was there.
func fillExprs(root ast.Node) {
	ast.Inspect(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Variable:
			// A statement parser may have set the initializer directly (a for
			// loop's declarations); keep it.
			if n.InitialExpr == nil {
				n.InitialExpr = parseSpan(n.Initial, false)
			}
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.Parameter:
			// A "parameter type T = ..." default is a data type, so a bare
			// name there is a type-or-expression to be resolved later.
			n.DefaultExpr = parseSpan(n.Default, n.Type.Name == "type")
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.EnumMember:
			n.ValueExpr = parseSpan(n.Value, false)
		case *ast.Port:
			n.DefaultExpr = parseSpan(n.Default, false)
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.PortDecl:
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.Arg:
			n.DefaultExpr = parseSpan(n.Default, false)
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.Instance:
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.GateInstance:
			n.UnpackedDims = filledDims(n.UnpackedDims)
		case *ast.Type:
			n.PackedDims = filledDims(n.PackedDims)
		case *ast.ParamOverride:
			// An override value may be a type. Overrides built by the
			// expression parser already carry theirs.
			if n.ValueExpr == nil {
				n.ValueExpr = parseSpan(n.Value, true)
			}
		case *ast.PortConnection:
			n.ConnExpr = parseSpan(n.Expr, false)
		case *ast.Class:
			n.ExtendsArgExprs = parseArgSpan(n.ExtendsArgs)
		}
		return true
	})
}

// filledDims returns a copy of ds with each dimension's bounds parsed. It
// copies because dimension arrays are shared: "logic [7:0] a, b, c;" yields
// three Variables whose Types hold the same slice, and parsing in place would
// hang one set of expression nodes under all three, turning the tree into a
// graph. The raw token spans stay shared (they are never mutated).
func filledDims(ds []ast.Dim) []ast.Dim {
	if len(ds) == 0 {
		return ds
	}
	out := slices.Clone(ds)
	for i := range out {
		d := &out[i]
		if d.LeftExpr != nil || d.Wildcard {
			continue // already parsed (an expression-position type's are)
		}
		if len(d.Left) == 1 && d.Left[0].Kind == token.KindMul && len(d.Right) == 0 {
			d.Wildcard = true // "[*]", a wildcard associative array index (LRM 7.8.1)
			continue
		}
		// A lone name, "[N]", is a size or an associative array's index type.
		d.LeftExpr = parseSpan(d.Left, len(d.Right) == 0)
		d.RightExpr = parseSpan(d.Right, false)
	}
	return out
}

// parseSpan parses a raw span as one expression: nil for an empty span, the
// tree if the whole span parsed without error, otherwise a BadExpr over all of
// it. If wrap, a name-shaped result is marked as type-or-expression.
func parseSpan(toks []preprocessor.Token, wrap bool) ast.Expr {
	if len(toks) == 0 {
		return nil
	}
	p := &parser{toks: toks}
	e := p.parseExpr()
	if len(p.errs) > 0 || p.peek().Kind != token.KindEOF {
		return badSpan(toks)
	}
	if wrap {
		e = markTypeOrExpr(e)
	}
	return e
}

// parseArgSpan parses a raw call-argument list (without its parentheses), as
// a Call's Args are parsed.
func parseArgSpan(toks []preprocessor.Token) []ast.Expr {
	if len(toks) == 0 {
		return nil
	}
	p := &parser{toks: toks}
	var args []ast.Expr
	for {
		args = append(args, p.parseArg(false))
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	if len(p.errs) > 0 || p.peek().Kind != token.KindEOF {
		return []ast.Expr{badSpan(toks)}
	}
	return args
}

// badSpan is a BadExpr covering every token of toks.
func badSpan(toks []preprocessor.Token) ast.Expr {
	b := &ast.BadExpr{Tokens: toks}
	b.SetPos(tokensSpan(toks))
	return b
}

var tokenSliceType = reflect.TypeOf([]preprocessor.Token(nil))

// cloneExpr deep-copies an expression tree. It is reflection-based because it
// serves the few places where one parsed expression has to be given to several
// owners ("wire #5 a, b;", "var type(x) a, b;") and nothing else is worth a
// hand-written copy of all forty node types. Raw token spans are shared: they
// are never mutated.
func cloneExpr(e ast.Expr) ast.Expr {
	if e == nil {
		return nil
	}
	return deepCopy(reflect.ValueOf(e)).Interface().(ast.Expr)
}

func cloneExprs(es []ast.Expr) []ast.Expr {
	if es == nil {
		return nil
	}
	out := make([]ast.Expr, len(es))
	for i, e := range es {
		out[i] = cloneExpr(e)
	}
	return out
}

func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(deepCopy(v.Elem()))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(deepCopy(v.Elem()))
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		c.Set(v) // a shallow copy first, which also carries the unexported position
		for i := 0; i < v.NumField(); i++ {
			if c.Field(i).CanSet() {
				c.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
		return c
	case reflect.Slice:
		if v.IsNil() || v.Type() == tokenSliceType {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(deepCopy(v.Index(i)))
		}
		return c
	}
	return v
}
