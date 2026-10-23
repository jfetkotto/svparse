package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// maxStmtDepth bounds how deeply statements may nest (blocks inside blocks,
// else-if chains). Real code stays far below it; it exists, like
// maxExprDepth, so adversarial input cannot exhaust the goroutine stack. Past
// it the rest of the statement is kept as a BadStmt.
const maxStmtDepth = 256

// ParseStmt parses toks as a single statement (LRM 9, 10, 12, 13, Annex A.6).
//
// It never fails and never returns nil: source it cannot make sense of becomes
// a *ast.BadStmt (or a *ast.BadExpr inside a statement) and an Error, and
// parsing carries on at the next ';' or block end. Tokens left over after a
// complete statement are reported as an error but not consumed into the tree.
//
// Constructs recognised but not modeled yet come back as *ast.RawStmt with no
// error: pattern matching ("if (x matches ...)", "case (x) matches"),
// randsequence, expect, and concurrent assertions.
func ParseStmt(toks []preprocessor.Token) (ast.Stmt, []Error) {
	p := &parser{toks: toks}
	s := p.parseStatement()
	if tok := p.peek(); tok.Kind != token.KindEOF {
		p.errorf(tok, "unexpected %s after statement", describeTok(tok))
	}
	ownTypes(s)
	fillExprs(s)
	return s, p.errs
}

// finishStmt stamps s with the span from token index start to the last token
// consumed, and returns it.
func finishStmt[S interface {
	ast.Stmt
	SetPos(ast.Position)
}](p *parser, start int, s S) S {
	s.SetPos(p.spanFrom(start))
	return s
}

// stmtStopKeywords end a block, a case, or the construct around a statement,
// so a statement scan stops at one without consuming it: it belongs to
// something enclosing. The container end keywords (isEndKeyword) stop it too.
var stmtStopKeywords = map[string]bool{
	"end": true, "join": true, "join_any": true, "join_none": true, "endcase": true,
	"endgenerate": true, "endsequence": true, "endproperty": true, "endclocking": true,
	"endchecker": true, "endgroup": true, "endspecify": true, "endtable": true,
	"endprimitive": true, "endconfig": true, "endrandsequence": true,
}

func isStmtStop(tok preprocessor.Token) bool {
	return tok.Kind == token.KindKeyword && (stmtStopKeywords[tok.Text] || isEndKeyword(tok.Text))
}

// stmtStartKeywords begin a statement of their own, so skipping garbage after
// a malformed statement stops at one instead of eating a good statement.
var stmtStartKeywords = map[string]bool{
	"begin": true, "fork": true, "if": true, "unique": true, "unique0": true, "priority": true,
	"case": true, "casex": true, "casez": true, "for": true, "foreach": true, "while": true,
	"do": true, "repeat": true, "forever": true, "wait": true, "wait_order": true,
	"return": true, "break": true, "continue": true, "disable": true,
	"assign": true, "deassign": true, "force": true, "release": true,
	"assert": true, "assume": true, "cover": true, "randcase": true,
}

func (p *parser) enterStmt() bool {
	if p.stmtDepth >= maxStmtDepth {
		return false
	}
	p.stmtDepth++
	return true
}

func (p *parser) leaveStmt() { p.stmtDepth-- }

// parseStatement parses one statement, "[label :] statement_item" (LRM A.6.4).
// An empty statement ";" is a *ast.Null. It always consumes at least one token
// unless it is at a block end or the end of input, where it consumes none and
// returns a zero-width BadStmt for the missing statement.
func (p *parser) parseStatement() ast.Stmt {
	p.skipAttributeInstances()
	start := p.pos
	if !p.enterStmt() {
		return p.stmtTooDeep(start)
	}
	defer p.leaveStmt()

	tok := p.peek()
	if tok.Kind == token.KindIdent && p.peekAt(1).Kind == token.KindColon {
		p.advance() // label
		p.advance() // ':'
		return finishStmt(p, start, &ast.Labeled{Label: tok.Text, Body: p.parseStatement()})
	}

	switch tok.Kind {
	case token.KindSemi:
		p.advance()
		return finishStmt(p, start, &ast.Null{})
	case token.KindHash, token.KindHashHash, token.KindAt:
		ctl := p.parseTimingControl()
		return finishStmt(p, start, &ast.TimedStmt{Control: ctl, Body: p.parseStatement()})
	case token.KindArrow, token.KindNonblockTrigger:
		return p.parseEventTrigger(start)
	case token.KindKeyword:
		if s := p.parseKeywordStatement(start); s != nil {
			return s
		}
	}
	return p.parseSimpleStatement(start)
}

// parseKeywordStatement parses a statement introduced by a keyword. It returns
// nil if the keyword does not begin one this function handles (a type keyword
// in a cast, "this", "super"), leaving the cursor where it was.
func (p *parser) parseKeywordStatement(start int) ast.Stmt {
	tok := p.peek()
	switch tok.Text {
	case "begin", "fork":
		return p.parseBlock(start)
	case "if":
		return p.parseIf(start, "")
	case "unique", "unique0", "priority":
		switch p.peekAt(1).Text {
		case "if":
			p.advance()
			return p.parseIf(start, tok.Text)
		case "case", "casex", "casez":
			p.advance()
			return p.parseCase(start, tok.Text)
		}
		return nil
	case "case", "casex", "casez":
		return p.parseCase(start, "")
	case "for":
		return p.parseFor(start)
	case "foreach":
		return p.parseForeach(start)
	case "while":
		p.advance()
		cond := p.parseParenCond()
		return finishStmt(p, start, &ast.While{Cond: cond, Body: p.parseStatement()})
	case "do":
		p.advance()
		body := p.parseStatement()
		d := &ast.DoWhile{Body: body}
		if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "while" {
			p.advance()
			d.Cond = p.parseParenCond()
		} else {
			p.errorf(p.peek(), "expected 'while' after the body of do, found %s", describeTok(p.peek()))
			d.Cond = p.emptyBad()
		}
		p.expectSemi()
		return finishStmt(p, start, d)
	case "repeat":
		p.advance()
		count := p.parseParenCond()
		return finishStmt(p, start, &ast.Repeat{Count: count, Body: p.parseStatement()})
	case "forever":
		p.advance()
		return finishStmt(p, start, &ast.Forever{Body: p.parseStatement()})
	case "wait":
		return p.parseWait(start)
	case "wait_order":
		return p.parseWaitOrder(start)
	case "return":
		p.advance()
		r := &ast.Return{}
		if p.peek().Kind != token.KindSemi {
			r.Value = p.parseExpr()
		}
		p.expectSemi()
		return finishStmt(p, start, r)
	case "break":
		p.advance()
		p.expectSemi()
		return finishStmt(p, start, &ast.Break{})
	case "continue":
		p.advance()
		p.expectSemi()
		return finishStmt(p, start, &ast.Continue{})
	case "disable":
		p.advance()
		d := &ast.Disable{}
		if t := p.peek(); t.Kind == token.KindKeyword && t.Text == "fork" {
			p.advance()
			d.Fork = true
		} else {
			d.Target = p.parseUnary()
		}
		p.expectSemi()
		return finishStmt(p, start, d)
	case "assign", "force":
		return p.parseProcAssign(start, true)
	case "deassign", "release":
		return p.parseProcAssign(start, false)
	case "assert", "assume", "cover":
		return p.parseImmediateAssert(start)
	case "randcase":
		return p.parseRandCase(start)
	case "randsequence", "expect", "restrict":
		return p.rawStatement(start)
	}
	return nil
}

// parseSimpleStatement parses an assignment ("a = b;", "a <= b;", "a += b;") or
// an expression statement ("f(x);", "i++;", "void'(f());").
//
// The left side is read at primary level, not as a full expression, because
// "a <= b" is a nonblocking assignment here and a comparison in an expression;
// reading the whole line as an expression would take the first meaning.
func (p *parser) parseSimpleStatement(start int) ast.Stmt {
	tok := p.peek()
	if !startsExpr(tok) {
		return p.badStatement(start)
	}
	lhs := p.parseUnary()
	if op := p.peek(); isAssignOp(op.Kind) || op.Kind == token.KindLe {
		p.advance()
		a := &ast.Assign{Op: op.Text, LHS: lhs, Control: p.parseIntraControl()}
		a.RHS = p.parseExpr()
		if p.atMatches() {
			return p.rawStatement(start)
		}
		p.expectSemi()
		return finishStmt(p, start, a)
	}
	if p.atMatches() {
		return p.rawStatement(start)
	}
	p.expectSemi()
	return finishStmt(p, start, &ast.ExprStmt{X: lhs})
}

// atMatches reports whether the cursor is at "matches", which in an expression
// begins a pattern ("x = v matches tagged A ? 1 : 2;"). Patterns are not
// modeled yet, so such a statement is kept raw.
func (p *parser) atMatches() bool {
	tok := p.peek()
	return tok.Kind == token.KindKeyword && tok.Text == "matches"
}

// parseIntraControl parses the optional delay or event control between an
// assignment operator and its right side, "a = #5 b;", "a <= @(e) b;", "a = repeat
// (3) @(e) b;", and a clocking drive's "##2".
func (p *parser) parseIntraControl() *ast.TimingControl {
	switch tok := p.peek(); {
	case tok.Kind == token.KindHash, tok.Kind == token.KindHashHash, tok.Kind == token.KindAt:
		return p.parseTimingControl()
	case tok.Kind == token.KindKeyword && tok.Text == "repeat":
		return p.parseTimingControl()
	}
	return nil
}

// parseTimingControl parses "#d", "##d", "@(events)", "@*", "@ident", or "repeat
// (n) @(events)" (LRM 9.4). The cursor is on the first token.
func (p *parser) parseTimingControl() *ast.TimingControl {
	start := p.pos
	c := &ast.TimingControl{}
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "repeat" {
		p.advance()
		c.Repeat = p.parseParenCond()
	}
	switch tok := p.peek(); tok.Kind {
	case token.KindHash:
		p.advance()
		c.Delay = p.parseDelayValue()
	case token.KindHashHash:
		p.advance()
		c.CycleDelay = true
		c.Delay = p.parseDelayValue()
	case token.KindAt:
		p.advance()
		p.parseEventControl(c)
	default:
		p.errorf(tok, "expected a delay or event control, found %s", describeTok(tok))
	}
	return finishStmt(p, start, c)
}

// parseDelayValue parses what follows '#': a number, a name, or a parenthesised
// expression. It is a primary, not an expression, so "#10 x = 1;" stops at x.
func (p *parser) parseDelayValue() ast.Expr {
	if p.peek().Kind == token.KindLParen {
		return p.parseParen()
	}
	start := p.pos
	e := p.parsePrimary()
	// A package-scoped name, "#pkg::delay" (a ps_identifier, LRM A.2.2.3).
	for {
		switch e.(type) {
		case *ast.Ident, *ast.ScopedName:
		default:
			return e
		}
		if p.peek().Kind != token.KindColonColon || p.peekAt(1).Kind != token.KindIdent {
			return e
		}
		p.advance()
		name := p.advance()
		e = finish(p, start, &ast.ScopedName{Scope: e, Name: name.Text})
	}
}

// parseEventControl parses what follows '@' into c: "*", "(*)", "(event_expr)",
// or a single event name.
func (p *parser) parseEventControl(c *ast.TimingControl) {
	switch tok := p.peek(); {
	case tok.Kind == token.KindMul:
		p.advance()
		c.Star = true
	case tok.Kind == token.KindAttrOpen && p.peekAt(1).Kind == token.KindRParen:
		// "@(* )" with a space: the lexer reads "(*" as an attribute opener.
		p.advance()
		p.advance()
		c.Star = true
	case tok.Kind == token.KindLParen:
		p.advance()
		if p.peek().Kind == token.KindMul && p.peekAt(1).Kind == token.KindRParen {
			p.advance()
			p.advance()
			c.Star = true
			return
		}
		for {
			c.Events = append(c.Events, p.parseEventTerm())
			if nx := p.peek(); nx.Kind == token.KindComma || (nx.Kind == token.KindKeyword && nx.Text == "or") {
				p.advance()
				continue
			}
			break
		}
		p.expectKind(token.KindRParen, "')' to close the event expression")
	default:
		// "@clk", "@pkg::ev", "@obj.ev"
		termStart := p.pos
		x := p.parsePostfix(p.parsePrimary(), termStart)
		term := &ast.EventTerm{X: x}
		c.Events = append(c.Events, *finishStmt(p, termStart, term))
	}
}

// parseEventTerm parses "[edge] expr [iff expr]".
func (p *parser) parseEventTerm() ast.EventTerm {
	start := p.pos
	t := &ast.EventTerm{}
	if tok := p.peek(); tok.Kind == token.KindKeyword && (tok.Text == "posedge" || tok.Text == "negedge" || tok.Text == "edge") {
		t.Edge = tok.Text
		p.advance()
	}
	t.X = p.parseExpr()
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "iff" {
		p.advance()
		t.Iff = p.parseExpr()
	}
	return *finishStmt(p, start, t)
}

// parseEventTrigger parses "-> ev;" and "->> [control] ev;".
func (p *parser) parseEventTrigger(start int) ast.Stmt {
	t := &ast.EventTrigger{Nonblocking: p.advance().Kind == token.KindNonblockTrigger}
	if t.Nonblocking {
		t.Control = p.parseIntraControl()
	}
	t.Event = p.parseUnary()
	p.expectSemi()
	return finishStmt(p, start, t)
}

// parseBlock parses "begin ... end" and "fork ... join[_any|_none]" with their
// optional labels, block-local declarations, and statements. The cursor is on
// the opening keyword.
func (p *parser) parseBlock(start int) ast.Stmt {
	b := &ast.Block{Fork: p.advance().Text == "fork"}
	if p.peek().Kind == token.KindColon {
		p.advance()
		if name, ok := p.expectIdent(); ok {
			b.Label = name.Text
		}
	}

	for {
		p.skipAttributeInstances()
		tok := p.peek()
		switch {
		case tok.Kind == token.KindEOF:
			p.errorf(tok, "unterminated %s block, expected %s", blockKeyword(b), blockCloser(b))
			return finishStmt(p, start, b)
		case tok.Kind == token.KindKeyword && closesBlock(b, tok.Text):
			p.advance()
			if b.Fork {
				b.Join = tok.Text
			}
			if p.peek().Kind == token.KindColon {
				p.advance()
				if name, ok := p.expectIdent(); ok {
					b.EndLabel = name.Text
				}
			}
			return finishStmt(p, start, b)
		case isStmtStop(tok):
			// A different closer: the block's own is missing. Leave it for
			// whatever it belongs to.
			p.errorf(tok, "expected %s before %q", blockCloser(b), tok.Text)
			return finishStmt(p, start, b)
		case p.atBlockDeclaration():
			b.Decls = append(b.Decls, p.parseBlockDeclaration()...)
		default:
			before := p.pos
			b.Stmts = append(b.Stmts, p.parseStatement())
			if p.pos == before {
				p.advance() // guarantee progress
			}
		}
	}
}

func blockKeyword(b *ast.Block) string {
	if b.Fork {
		return "fork"
	}
	return "begin"
}

func blockCloser(b *ast.Block) string {
	if b.Fork {
		return "join"
	}
	return "end"
}

func closesBlock(b *ast.Block, kw string) bool {
	if b.Fork {
		return kw == "join" || kw == "join_any" || kw == "join_none"
	}
	return kw == "end"
}

// atBlockDeclaration reports whether the cursor is at a block-local
// declaration (LRM 9.3.1's block_item_declaration) rather than a statement.
func (p *parser) atBlockDeclaration() bool {
	tok := p.peek()
	switch tok.Kind {
	case token.KindKeyword:
		switch tok.Text {
		case "typedef", "parameter", "localparam", "import", "const", "var", "automatic", "static", "nettype", "virtual":
			return true
		}
		if isTypeCastKeyword(tok, p.peekAt(1)) {
			return false // "int'(x);" is an expression statement
		}
		return dataTypeKeywords[tok.Text] || aggregateKeywords[tok.Text]
	case token.KindIdent:
		return p.looksLikeTypedDeclaration()
	}
	return false
}

// looksLikeTypedDeclaration reports whether an identifier-led line is a
// declaration with a user-defined type, "my_t x;", "pkg::t x;", "cls #(8) x;",
// "my_t [3:0] x;": a type-like prefix followed directly by a second name, a
// shape no statement has.
func (p *parser) looksLikeTypedDeclaration() bool {
	i := 1 // past the first identifier
	for p.peekAt(i).Kind == token.KindColonColon && p.peekAt(i+1).Kind == token.KindIdent {
		i += 2
	}
	if p.peekAt(i).Kind == token.KindHash && p.peekAt(i+1).Kind == token.KindLParen {
		i = p.skipBalanced(i + 1)
		for p.peekAt(i).Kind == token.KindColonColon && p.peekAt(i+1).Kind == token.KindIdent {
			i += 2
		}
	}
	for p.peekAt(i).Kind == token.KindLBrack {
		i = p.skipBalanced(i)
	}
	return p.peekAt(i).Kind == token.KindIdent
}

// skipBalanced returns the offset just past the bracketed group that opens at
// offset i (a '(' or '['), or the offset of the end of input if it is unclosed.
func (p *parser) skipBalanced(i int) int {
	depth := 0
	for {
		switch p.peekAt(i).Kind {
		case token.KindEOF:
			return i
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
}

// parseBlockDeclaration parses one block-local declaration using the same
// declaration parser a module body uses, and returns what it declared.
func (p *parser) parseBlockDeclaration() []ast.Decl {
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "automatic" {
		p.advance() // a lifetime qualifier: recorded nowhere, like a function's
	}
	before := p.pos
	decls, ok := p.parseDecl()
	if !ok {
		p.recover()
	}
	if p.pos == before {
		p.advance()
	}
	return decls
}

// parseIf parses "if (cond) stmt [else stmt]"; the cursor is on "if" and any
// qualifier has been consumed. A pattern-matching condition is kept raw.
//
// An "else if" chain is read in a loop rather than by recursion: each link is a
// continuation of the one before, not a deeper construct, and a chain of
// hundreds of branches (a decoder, a generated state machine) is ordinary. The
// tree is still nested, one If inside the previous one's Else, as the grammar
// says; only the parse is flat. Every If in a chain ends where the chain does.
func (p *parser) parseIf(start int, qualifier string) ast.Stmt {
	type link struct {
		node  *ast.If
		start int
	}
	var chain []link
	curStart, curQualifier := start, qualifier
	var tail ast.Stmt // set if the chain ends in a raw statement

	for {
		p.advance() // "if"
		n := &ast.If{Qualifier: curQualifier}
		cond, matches := p.parseIfCond()
		n.Cond = cond
		chain = append(chain, link{n, curStart})
		if matches {
			tail = p.rawStatement(curStart)
			chain = chain[:len(chain)-1] // the raw statement replaces this link
			break
		}
		n.Then = p.parseStatement()

		tok := p.peek()
		if !(tok.Kind == token.KindKeyword && tok.Text == "else") {
			break
		}
		p.advance() // "else"
		if q, ok := p.atIf(); ok {
			curStart, curQualifier = p.pos, q
			if q != "" {
				p.advance() // the qualifier; parseIf's loop consumes "if"
			}
			continue
		}
		n.Else = p.parseStatement()
		break
	}

	// Link the chain from the end back, each node's span ending where the
	// whole statement does.
	var next ast.Stmt = tail
	for k := len(chain) - 1; k >= 0; k-- {
		l := chain[k]
		if next != nil && l.node.Else == nil {
			l.node.Else = next
		}
		next = finishStmt(p, l.start, l.node)
	}
	return next
}

// atIf reports whether the cursor is at "if", "unique if", "unique0 if", or
// "priority if", the start of an else-if link, and returns the qualifier.
func (p *parser) atIf() (qualifier string, ok bool) {
	tok := p.peek()
	if tok.Kind != token.KindKeyword {
		return "", false
	}
	switch tok.Text {
	case "if":
		return "", true
	case "unique", "unique0", "priority":
		if nx := p.peekAt(1); nx.Kind == token.KindKeyword && nx.Text == "if" {
			return tok.Text, true
		}
	}
	return "", false
}

// parseParenCond parses "( expr )", the condition of if, while, repeat, wait.
func (p *parser) parseParenCond() ast.Expr {
	if !p.expectKind(token.KindLParen, "'('") {
		return p.emptyBad()
	}
	e := p.parseExpr()
	p.expectKind(token.KindRParen, "')'")
	return e
}

// parseIfCond is parseParenCond for an if. It reports matches = true, without
// consuming the rest, if the condition uses pattern matching ("x matches
// pattern"), which is not modeled, so the caller can keep the statement raw.
func (p *parser) parseIfCond() (cond ast.Expr, matches bool) {
	if !p.expectKind(token.KindLParen, "'('") {
		return p.emptyBad(), false
	}
	e := p.parseExpr()
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "matches" {
		return e, true
	}
	p.expectKind(token.KindRParen, "')'")
	return e, false
}

// parseCase parses a case, casez, casex, or case-inside statement; the cursor
// is on the case keyword and any qualifier has been consumed. "matches" is kept raw.
func (p *parser) parseCase(start int, qualifier string) ast.Stmt {
	kw := p.advance()
	c := &ast.Case{Qualifier: qualifier, Kind: kw.Text}
	if !p.expectKind(token.KindLParen, "'('") {
		c.Subject = p.emptyBad()
	} else {
		c.Subject = p.parseExpr()
		p.expectKind(token.KindRParen, "')'")
	}
	if tok := p.peek(); tok.Kind == token.KindKeyword {
		switch tok.Text {
		case "inside":
			p.advance()
			c.Inside = true
		case "matches":
			return p.rawStatement(start)
		}
	}
	for {
		tok := p.peek()
		switch {
		case tok.Kind == token.KindKeyword && tok.Text == "endcase":
			p.advance()
			return finishStmt(p, start, c)
		case tok.Kind == token.KindEOF:
			p.errorf(tok, "unterminated case statement, expected endcase")
			return finishStmt(p, start, c)
		case isStmtStop(tok):
			p.errorf(tok, "expected endcase before %q", tok.Text)
			return finishStmt(p, start, c)
		}
		before := p.pos
		c.Items = append(c.Items, p.parseCaseItem(c.Inside))
		if p.pos == before {
			p.advance()
		}
	}
}

// parseCaseItem parses "expr, expr : stmt" or "default [:] stmt".
func (p *parser) parseCaseItem(inside bool) ast.CaseItem {
	start := p.pos
	item := &ast.CaseItem{}
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "default" {
		p.advance()
		item.Default = true
		if p.peek().Kind == token.KindColon {
			p.advance()
		}
	} else {
		for {
			if inside {
				item.Exprs = append(item.Exprs, p.parseRangeOrValue())
			} else {
				item.Exprs = append(item.Exprs, p.parseExpr())
			}
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
		p.expectKind(token.KindColon, "':' after the case item")
	}
	item.Body = p.parseStatement()
	return *finishStmt(p, start, item)
}

// parseFor parses "for (init; cond; step) stmt". The header is parsed by a
// sub-parser limited to its own parentheses, so a missing ')' cannot read into
// the body.
func (p *parser) parseFor(start int) ast.Stmt {
	p.advance() // "for"
	n := &ast.For{}
	if !p.expectKind(token.KindLParen, "'('") {
		n.Body = p.parseStatement()
		return finishStmt(p, start, n)
	}
	end := p.headerEnd()
	h := p.newSubParser(p.toks[p.pos:end])
	h.parseForHeader(n)
	p.mergeErrors(h)
	p.pos = end
	p.expectKind(token.KindRParen, "')' to close the for header")
	n.Body = p.parseStatement()
	return finishStmt(p, start, n)
}

// headerEnd returns the index of the ')' that closes the header the cursor is
// inside (just past its '('), or where the header must end if it is missing: a
// block end, a statement-starting keyword, a third ';' (a for header holds
// exactly two, so a third is the end of the whole statement), or the end of
// input.
func (p *parser) headerEnd() int {
	depth, semis := 0, 0
	for i := p.pos; i < len(p.toks); i++ {
		tok := p.toks[i]
		switch tok.Kind {
		case token.KindSemi:
			if depth == 0 {
				if semis++; semis == 3 {
					return i
				}
			}
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindRParen:
			if depth == 0 {
				return i
			}
			depth--
		case token.KindKeyword:
			if isStmtStop(tok) || (stmtStartKeywords[tok.Text] && depth == 0) {
				return i
			}
		}
	}
	return len(p.toks)
}

// parseForHeader parses the inside of a for's parentheses into n.
func (p *parser) parseForHeader(n *ast.For) {
	// init
	if p.peek().Kind != token.KindSemi {
		if tok := p.peek(); (tok.Kind == token.KindKeyword && (dataTypeKeywords[tok.Text] || tok.Text == "var" || tok.Text == "automatic" || tok.Text == "static")) ||
			(tok.Kind == token.KindIdent && p.looksLikeTypedDeclaration()) {
			groups, closed := p.splitByComma(token.KindSemi, false)
			if !closed {
				p.errorf(p.peek(), "expected ';' after the for initialisation")
			}
			// "int i = 0, j = 1" shares one type; "int i = 0, bit c = 1" starts
			// a new declaration (LRM 12.7.1), recognised by a group that begins
			// with a type.
			var typ ast.Type
			for k, g := range groups {
				decl := p.newSubParser(g)
				if k == 0 || decl.atForDeclarationStart() {
					if t := decl.peek(); t.Kind == token.KindKeyword && (t.Text == "var" || t.Text == "automatic" || t.Text == "static") {
						decl.advance()
					}
					typ = decl.parseDataType()
					p.mergeErrors(decl)
					g = g[decl.pos:]
				}
				if v, ok := p.parseVariableDeclarator(g, typ); ok {
					n.InitDecls = append(n.InitDecls, v)
				}
			}
		} else {
			n.Init = p.parseAssignList(token.KindSemi)
			p.expectKind(token.KindSemi, "';' after the for initialisation")
		}
	} else {
		p.advance()
	}
	// condition
	if p.peek().Kind != token.KindSemi && p.peek().Kind != token.KindEOF {
		n.Cond = p.parseExpr()
	}
	p.expectKind(token.KindSemi, "';' after the for condition")
	// step
	if p.peek().Kind != token.KindEOF {
		n.Step = p.parseAssignList(token.KindEOF)
	}
	if tok := p.peek(); tok.Kind != token.KindEOF {
		p.errorf(tok, "unexpected %s in the for header", describeTok(tok))
	}
}

// atForDeclarationStart reports whether a comma-separated group of a for
// initialisation begins a new declaration, with a type of its own, rather than
// declaring one more name of the previous type.
func (p *parser) atForDeclarationStart() bool {
	tok := p.peek()
	switch tok.Kind {
	case token.KindKeyword:
		return dataTypeKeywords[tok.Text] || aggregateKeywords[tok.Text] || tok.Text == "var"
	case token.KindIdent:
		return p.looksLikeTypedDeclaration()
	}
	return false
}

// parseAssignList parses comma-separated simple assignments or expression
// statements without their ';', as in a for header, stopping at stop or the end
// of the header.
func (p *parser) parseAssignList(stop token.Kind) []ast.Stmt {
	var out []ast.Stmt
	for {
		start := p.pos
		if k := p.peek().Kind; k == stop || k == token.KindEOF {
			return out
		}
		lhs := p.parseUnary()
		var s ast.Stmt
		if op := p.peek(); isAssignOp(op.Kind) || op.Kind == token.KindLe {
			p.advance()
			a := &ast.Assign{Op: op.Text, LHS: lhs}
			a.RHS = p.parseExpr()
			s = finishStmt(p, start, a)
		} else {
			s = finishStmt(p, start, &ast.ExprStmt{X: lhs})
		}
		out = append(out, s)
		if p.peek().Kind != token.KindComma {
			return out
		}
		p.advance()
	}
}

// parseForeach parses "foreach (arr[i, j]) stmt".
func (p *parser) parseForeach(start int) ast.Stmt {
	p.advance() // "foreach"
	n := &ast.Foreach{}
	if !p.expectKind(token.KindLParen, "'('") {
		n.Array = p.emptyBad()
		n.Body = p.parseStatement()
		return finishStmt(p, start, n)
	}
	// The array is a (possibly hierarchical or scoped) name; the '[' that
	// follows it holds loop variables, not an index expression, so the name is
	// read without postfix selects.
	arrStart := p.pos
	n.Array = p.parseHierName(arrStart)
	if p.expectKind(token.KindLBrack, "'[' before the loop variables") {
		for {
			if tok := p.peek(); tok.Kind == token.KindIdent {
				n.Vars = append(n.Vars, tok.Text)
				p.advance()
			} else {
				n.Vars = append(n.Vars, "") // a skipped dimension
			}
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
		p.expectKind(token.KindRBrack, "']'")
	}
	p.expectKind(token.KindRParen, "')'")
	n.Body = p.parseStatement()
	return finishStmt(p, start, n)
}

// parseHierName parses a name with '.' and '::' components but no selects or
// calls: "a", "a.b", "pkg::a".
func (p *parser) parseHierName(start int) ast.Expr {
	tok := p.peek()
	if tok.Kind != token.KindIdent && tok.Kind != token.KindSystemIdent {
		p.errorf(tok, "expected a name, found %s", describeTok(tok))
		return p.emptyBad()
	}
	p.advance()
	var e ast.Expr = finish(p, start, &ast.Ident{Name: tok.Text})
	for {
		switch p.peek().Kind {
		case token.KindDot:
			if nx := p.peekAt(1); nx.Kind == token.KindIdent {
				p.advance()
				p.advance()
				e = finish(p, start, &ast.MemberSelect{X: e, Member: nx.Text})
				continue
			}
		case token.KindColonColon:
			if nx := p.peekAt(1); nx.Kind == token.KindIdent {
				p.advance()
				p.advance()
				e = finish(p, start, &ast.ScopedName{Scope: e, Name: nx.Text})
				continue
			}
		}
		return e
	}
}

// parseWait parses "wait (cond) stmt" and "wait fork;".
func (p *parser) parseWait(start int) ast.Stmt {
	p.advance() // "wait"
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "fork" {
		p.advance()
		p.expectSemi()
		return finishStmt(p, start, &ast.WaitFork{})
	}
	cond := p.parseParenCond()
	return finishStmt(p, start, &ast.Wait{Cond: cond, Body: p.parseStatement()})
}

// parseWaitOrder parses "wait_order (a, b, c) [then] [else stmt]".
func (p *parser) parseWaitOrder(start int) ast.Stmt {
	p.advance() // "wait_order"
	n := &ast.WaitOrder{}
	if p.expectKind(token.KindLParen, "'('") {
		for {
			n.Events = append(n.Events, p.parseUnary())
			if p.peek().Kind != token.KindComma {
				break
			}
			p.advance()
		}
		p.expectKind(token.KindRParen, "')'")
	}
	if tok := p.peek(); !(tok.Kind == token.KindKeyword && tok.Text == "else") {
		n.Then = p.parseStatement()
	}
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "else" {
		p.advance()
		n.Else = p.parseStatement()
	}
	return finishStmt(p, start, n)
}

// parseProcAssign parses "assign|force lhs = rhs;" (withValue) and "deassign|
// release lhs;".
func (p *parser) parseProcAssign(start int, withValue bool) ast.Stmt {
	n := &ast.ProcAssign{Kind: p.advance().Text}
	n.LHS = p.parseUnary()
	if withValue {
		p.expectKind(token.KindAssign, "'='")
		n.RHS = p.parseExpr()
	}
	p.expectSemi()
	return finishStmt(p, start, n)
}

// parseImmediateAssert parses "assert|assume|cover [#0|final] (cond) [stmt]
// [else stmt]". A concurrent assertion ("assert property") is kept raw.
func (p *parser) parseImmediateAssert(start int) ast.Stmt {
	kind := p.advance().Text
	if tok := p.peek(); tok.Kind == token.KindKeyword && (tok.Text == "property" || tok.Text == "sequence") {
		p.pos = start
		return p.rawStatement(start)
	}
	n := &ast.ImmediateAssert{Kind: kind}
	switch tok := p.peek(); {
	case tok.Kind == token.KindHash && p.peekAt(1).Text == "0":
		p.advance()
		p.advance()
		n.Deferred = "#0"
	case tok.Kind == token.KindKeyword && tok.Text == "final":
		p.advance()
		n.Deferred = "final"
	}
	n.Cond = p.parseParenCond()
	if tok := p.peek(); !(tok.Kind == token.KindKeyword && tok.Text == "else") {
		n.Then = p.parseStatement()
	}
	if tok := p.peek(); tok.Kind == token.KindKeyword && tok.Text == "else" && kind != "cover" {
		p.advance()
		n.Else = p.parseStatement()
	}
	return finishStmt(p, start, n)
}

// parseRandCase parses "randcase weight : stmt ... endcase".
func (p *parser) parseRandCase(start int) ast.Stmt {
	p.advance() // "randcase"
	n := &ast.RandCase{}
	for {
		tok := p.peek()
		switch {
		case tok.Kind == token.KindKeyword && tok.Text == "endcase":
			p.advance()
			return finishStmt(p, start, n)
		case tok.Kind == token.KindEOF:
			p.errorf(tok, "unterminated randcase, expected endcase")
			return finishStmt(p, start, n)
		case isStmtStop(tok):
			p.errorf(tok, "expected endcase before %q", tok.Text)
			return finishStmt(p, start, n)
		}
		itemStart := p.pos
		item := &ast.RandCaseItem{Weight: p.parseExpr()}
		p.expectKind(token.KindColon, "':' after the randcase weight")
		item.Body = p.parseStatement()
		n.Items = append(n.Items, *finishStmt(p, itemStart, item))
		if p.pos == itemStart {
			p.advance()
		}
	}
}

// expectSemi consumes the ';' that ends a simple statement. If it is missing it
// records an error and skips to the next ';' (consumed) so the rest of the bad
// statement is not read as new ones, stopping unconsumed at a block end or at
// a keyword that begins a statement, which the missing ';' most likely preceded.
func (p *parser) expectSemi() {
	if p.peek().Kind == token.KindSemi {
		p.advance()
		return
	}
	tok := p.peek()
	p.errorf(tok, "expected ';', found %s", describeTok(tok))
	p.syncStatement()
}

// syncStatement skips forward past the end of the current (malformed)
// statement: through the next ';' outside brackets, but not past a block end or
// a statement-starting keyword, and not past the end of input.
func (p *parser) syncStatement() {
	depth := 0
	for {
		tok := p.peek()
		switch tok.Kind {
		case token.KindEOF:
			return
		case token.KindLParen, token.KindLBrack, token.KindLBrace, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrack, token.KindRBrace:
			if depth > 0 {
				depth--
			}
		case token.KindSemi:
			if depth == 0 {
				p.advance()
				return
			}
		case token.KindKeyword:
			if isStmtStop(tok) || (depth == 0 && stmtStartKeywords[tok.Text]) {
				return
			}
		}
		p.advance()
	}
}

// badStatement handles a token that cannot start a statement: it is reported,
// consumed (so the caller always makes progress) unless it is a block end, and
// the rest of the line skipped.
func (p *parser) badStatement(start int) ast.Stmt {
	tok := p.peek()
	if isStmtStop(tok) || tok.Kind == token.KindEOF {
		p.errorf(tok, "expected a statement, found %s", describeTok(tok))
		return p.badStmt(start)
	}
	p.errorf(tok, "expected a statement, found %s", describeTok(tok))
	p.advance()
	p.syncStatement()
	return p.badStmt(start)
}

func (p *parser) badStmt(start int) ast.Stmt {
	b := &ast.BadStmt{Tokens: p.toks[start:p.pos]}
	b.SetPos(p.spanFrom(start))
	return b
}

// rawStatement keeps the statement starting at token index start as a
// *ast.RawStmt without interpreting it, using the same extent rules the
// declaration parser applies when it skips a procedural construct. It records
// no error: a statement kept raw is a recognised construct, not a mistake.
func (p *parser) rawStatement(start int) ast.Stmt {
	p.pos = start
	errs := len(p.errs)
	if p.peek().Text == "randsequence" { // peek, not p.toks[start]: start can be the end of input
		// Delimited by endsequence, not by a ';' or a begin/end pair.
		p.skipToKeyword("endsequence", nil)
	} else {
		p.skipProceduralConstruct()
	}
	p.errs = p.errs[:errs]
	if p.pos == start {
		p.advance() // never stand still
	}
	r := &ast.RawStmt{Tokens: p.toks[start:p.pos]}
	r.SetPos(p.spanFrom(start))
	return r
}

// stmtTooDeep keeps a statement nested more than maxStmtDepth levels deep as a
// BadStmt, reporting it once, so the recursion unwinds.
func (p *parser) stmtTooDeep(start int) ast.Stmt {
	p.errorf(p.peek(), "statements nested more than %d levels deep", maxStmtDepth)
	p.rawStatement(start)
	return p.badStmt(start)
}

// parseSubroutineBody parses the tokens of a function or task body, the ones
// between the header's ';' and the end keyword, into its local declarations and
// statements. It runs in a parser of its own, limited to those tokens, so it
// cannot read past the end keyword however malformed the body is.
//
// A body written in the older style declares its arguments in the body,
// "function int f; input int a, b; ...". Those come back as args, to follow the
// ones in the header.
//
// Errors are not reported: the extent of the body was settled by skipBody
// without looking inside, and reading it is new. They come back as suppressed.
func (p *parser) parseSubroutineBody(toks []preprocessor.Token) (decls []ast.Decl, stmts []ast.Stmt, args []ast.Arg, suppressed []Error) {
	body := p.newSubParser(toks)
	for body.peek().Kind != token.KindEOF {
		body.skipAttributeInstances()
		if body.peek().Kind == token.KindEOF {
			break
		}
		before := body.pos
		switch tok := body.peek(); {
		case tok.Kind == token.KindKeyword && isTFPortDirection(tok.Text):
			ports, _ := body.parsePortDecl()
			for _, d := range ports {
				pd := d.(*ast.PortDecl)
				a := ast.Arg{Direction: pd.Direction, Type: pd.Type, Name: pd.Name, UnpackedDims: pd.UnpackedDims}
				a.Position = pd.Position
				args = append(args, a)
			}
		case body.atBlockDeclaration():
			decls = append(decls, body.parseBlockDeclaration()...)
		default:
			stmts = append(stmts, body.parseStatement())
		}
		if body.pos == before {
			body.advance() // guarantee progress
		}
	}
	return decls, stmts, args, body.errs
}

func isTFPortDirection(text string) bool {
	switch text {
	case "input", "output", "inout", "ref":
		return true
	}
	return false
}
