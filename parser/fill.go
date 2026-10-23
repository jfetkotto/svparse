package parser

import (
	"slices"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

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
func fillExprs(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Variable:
			n.InitialExpr = parseSpan(n.Initial, false)
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
	first := toks[0]
	pos := ast.Position{File: first.File, Line: first.Line, Character: first.Character}
	pos.EndLine, pos.EndCharacter = toks[len(toks)-1].End()
	if pos.EndLine < pos.Line || (pos.EndLine == pos.Line && pos.EndCharacter < pos.Character) {
		pos.EndLine, pos.EndCharacter = pos.Line, pos.Character
	}
	b := &ast.BadExpr{Tokens: toks}
	b.SetPos(pos)
	return b
}
