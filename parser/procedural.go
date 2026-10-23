package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/token"
)

// parseProceduralItem parses one `initial`, `final`, `always*` or `assign`
// item at container scope (LRM 9.2, 10.3), the cursor on its keyword.
//
// Extents still come from skipProceduralConstruct, so declaration boundaries,
// recovery and the errors it reports are exactly what they were when these
// items were skipped. The tokens it consumed are then parsed by a bounded
// sub-parser, and any error from that parse is held in p.suppressed like the
// ones from subroutine bodies: reporting statement errors is deliberate
// future work (feature/expr-diagnostics), not a side effect of emitting nodes.
func (p *parser) parseProceduralItem() ast.Decl {
	start := p.pos
	kw := p.advance()
	bodyStart := p.pos
	p.skipProceduralConstruct()
	sub := p.newSubParser(p.toks[bodyStart:p.pos])

	var d ast.Decl
	var setPos func(ast.Position)
	switch kw.Text {
	case "initial":
		n := &ast.Initial{Body: sub.parseItemBody()}
		d, setPos = n, func(pos ast.Position) { n.Position = pos }
	case "final":
		n := &ast.Final{Body: sub.parseItemBody()}
		d, setPos = n, func(pos ast.Position) { n.Position = pos }
	case "assign":
		n := sub.parseContinuousAssign()
		d, setPos = n, func(pos ast.Position) { n.Position = pos }
	default:
		n := &ast.Always{Kind: kw.Text, Body: sub.parseItemBody()}
		d, setPos = n, func(pos ast.Position) { n.Position = pos }
	}
	p.giveBackElse(sub, bodyStart)
	setPos(p.spanFrom(start))
	p.suppressed = append(p.suppressed, sub.errs...)
	return d
}

// giveBackElse undoes the skip's habit of treating a following "else" as the
// rest of this item: an else the item's own statement did not consume (the
// statement is not an if, or is a continuous assignment) belongs to an
// enclosing generate-if ("if (c) assign a = b; else assign a = d;"). The
// cursor moves back to it. sub must be the sub-parser over p.toks[from:p.pos].
func (p *parser) giveBackElse(sub *parser, from int) {
	if tok := sub.peek(); tok.Kind == token.KindKeyword && tok.Text == "else" {
		p.pos = from + sub.pos
	}
}

// parseItemBody parses the single statement of a procedural block. Anything
// left after it (stray tokens skipProceduralConstruct happened to include) is
// dropped: the block's extent was already settled by the skip.
func (p *parser) parseItemBody() ast.Stmt {
	return p.parseStatement()
}

// parseContinuousAssign parses "[(strength)] [#delay] lhs = rhs {, lhs = rhs} ;"
// (LRM 10.3.2), the leading "assign" already consumed by the caller.
func (p *parser) parseContinuousAssign() *ast.ContinuousAssign {
	n := &ast.ContinuousAssign{}
	if p.peek().Kind == token.KindLParen {
		n.DriveStrength = p.parseStrength()
	}
	if p.peek().Kind == token.KindHash {
		n.Delay = p.parseDelay3()
	}
	for {
		start := p.pos
		lhs := p.parseUnary()
		a := ast.Assign{Op: "="}
		if p.expectKind(token.KindAssign, "'=' in a continuous assignment") {
			a.LHS = lhs
			a.RHS = p.parseExpr()
		} else {
			a.LHS = lhs
			a.RHS = p.emptyBad()
		}
		a.SetPos(p.spanFrom(start))
		n.Assigns = append(n.Assigns, a)
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}
	p.expectSemi()
	return n
}
