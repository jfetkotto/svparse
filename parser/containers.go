package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
	"github.com/jfetkotto/svparse/token"
)

// externMethodQualifierKeywords are the method qualifiers that can appear
// between "extern" and "function"/"task" in an out-of-class-body method
// prototype (LRM 8.24), e.g. "extern virtual function void f();". None is
// tracked on ast.Function/Task, matching how "virtual"/"static" are
// consumed-but-untracked everywhere else this parser sees them.
var externMethodQualifierKeywords = map[string]bool{
	"virtual": true, "static": true, "protected": true, "local": true, "pure": true,
}

// gatePrimitiveKeywords are the built-in gate/switch primitive names (LRM
// 28.3, 28.4, 28.6, 28.12) that can start a primitive instantiation at
// module scope ("and g1 (y, a, b);") -- structurally similar to a module
// instantiation, but never modeled as one: a gate has no declaration of
// its own to resolve against (there's no "module and" anywhere to jump
// to), so its instances are recognized only well enough to be skipped
// without corrupting whatever declaration follows, same reasoning as
// "modport"/"defparam" above.
var gatePrimitiveKeywords = map[string]bool{
	"and": true, "nand": true, "or": true, "nor": true, "xor": true, "xnor": true,
	"buf": true, "not": true, "bufif0": true, "bufif1": true, "notif0": true, "notif1": true,
	"nmos": true, "pmos": true, "cmos": true, "rnmos": true, "rpmos": true, "rcmos": true,
	"tran": true, "tranif0": true, "tranif1": true, "rtran": true, "rtranif0": true, "rtranif1": true,
	"pullup": true, "pulldown": true,
}

// memberQualifierKeywords are the class-member/variable qualifiers this
// parser recognizes in a leading run, in any order or combination --
// real-world SV code writes these in every order ("protected virtual
// function", "virtual protected function", "static local task", "local
// static int x;", ...), which parseDecl's original shape (one case per
// keyword, each checking only ONE fixed following token) couldn't
// accommodate: a qualifier appearing in an unexpected slot either
// corrupted the parse (parseTypeBase happily consumes a stray qualifier
// keyword AS a type name -- isTypeNameToken accepts any keyword) or
// silently dropped a flag. See parseQualifiedDecl.
//
// "pure"/"extern" are deliberately NOT included here -- they keep their
// own existing, already order-tolerant dispatch (see the "extern" case's
// own qualifier scan, and "pure"'s own case), and folding them into this
// generic path would incorrectly imply e.g. "extern int x;" is a real
// declaration shape.
var memberQualifierKeywords = map[string]bool{
	"virtual": true, "static": true, "local": true, "protected": true,
	"rand": true, "randc": true, "const": true, "var": true,
}

// concurrentAssertionKeywords are the keywords that begin a concurrent
// assertion statement (LRM 16.14): "assert"/"assume"/"cover"/"restrict"
// [property], each optionally preceded by a "block_identifier :" label.
// Used both by parseDecl's unlabeled dispatch (a plain switch case) and
// its labeled-form lookahead, which needs this as a lookup rather than a
// switch since it's checking a token two positions ahead of the label.
var concurrentAssertionKeywords = map[string]bool{
	"assert": true, "assume": true, "cover": true, "restrict": true,
}

// parseBody repeatedly dispatches on the next token via parseDecl until
// it consumes endKeyword (matched=true), hits a different recognized end
// keyword without consuming it (an implicitly unterminated body,
// tolerated the same way nols's own scanner tolerates a stray end-
// keyword, just inverted -- missing rather than extra), or runs out of
// input. endKeyword == "" means top-level: there's no enclosing
// container, so parsing simply runs to EOF and end/matched are
// meaningless to the caller.
func (p *parser) parseBody(endKeyword string) (decls []ast.Decl, end preprocessor.Token, matched bool) {
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			if endKeyword != "" {
				p.errorf(tok, "unexpected end of file, expected %s", endKeyword)
			}
			return decls, tok, false
		}
		if endKeyword != "" && tok.Kind == token.KindKeyword {
			if tok.Text == endKeyword {
				p.advance()
				p.skipEndLabel()
				return decls, tok, true
			}
			if isEndKeyword(tok.Text) {
				p.errorf(tok, "expected %s, found %s", endKeyword, tok.Text)
				return decls, tok, false
			}
		}

		before := p.pos
		newDecls, ok := p.parseDecl()
		if ok {
			decls = append(decls, newDecls...)
		} else {
			// A sub-parser invoked from parseDecl may already have
			// recorded a more specific error (e.g. expectIdent's
			// "expected an identifier"); this generic one is added
			// unconditionally anyway rather than trying to distinguish
			// that case from "nothing matched at all" -- occasionally
			// double-reporting one syntax problem is a minor cosmetic
			// cost, worth it for guaranteeing every recovery has some
			// recorded error rather than silently skipping.
			p.errorf(tok, "unexpected token %q, skipping to the next declaration", tok.Text)
			// recover() only applies when parseDecl honored its "ok=false
			// means the cursor didn't move" contract. A parse function that
			// consumed part (or all) of a malformed declaration before
			// giving up has already made forward progress, and the cursor
			// now sits at whatever follows -- very often the NEXT
			// declaration's first token. recover() unconditionally consumes
			// the token it starts on and then skips to the following ';',
			// so running it there deletes that next declaration outright
			// (a half-typed "logic" above a module used to take the whole
			// module with it). Skipping recover() can't spin the loop:
			// either the cursor moved here, or recover() runs and consumes
			// at least one token.
			if p.pos == before {
				p.recover()
			}
		}
	}
}

// parseDecl attempts to parse one declaration starting at the cursor,
// dispatching on the current token, and returns every ast.Decl it
// produced -- almost always one, but a multi-name variable or parameter
// declaration ("logic a, b, c;") produces several from a single
// statement, which is why this returns a slice rather than one Decl.
// Returns ok=false (consuming nothing) if nothing recognized starts
// here -- parseBody's caller handles that via recover.
func (p *parser) parseDecl() ([]ast.Decl, bool) {
	p.skipAttributeInstances()

	tok := p.peek()
	if tok.Kind == token.KindSemi {
		// A lone top-level ';' -- most often trailing a closing keyword
		// that doesn't itself take one ("endmodule;", "end;"), which isn't
		// legal SV but is harmless enough that tools commonly tolerate it
		// as a no-op empty declaration rather than erroring; either way,
		// silently accepting it here is cheaper and no less correct than
		// reporting a spurious error over stray punctuation with no
		// information content.
		p.advance()
		return nil, true
	}
	if tok.Kind == token.KindIdent && p.peekAt(1).Kind == token.KindColon && concurrentAssertionKeywords[p.peekAt(2).Text] {
		// "a1: assert property (...);" -- a block-identifier-labeled
		// concurrent assertion statement (LRM 16.14), which would
		// otherwise reach parseVariableOrInstantiation and misparse as a
		// malformed variable/instantiation starting with "a1" (the ':'
		// isn't a name/dims/paren, so it fails there instead). A bare
		// "Ident Colon" has no other legal meaning at this dispatch level
		// -- everything else that could produce one (a case-statement
		// label, a ternary expression) only occurs inside an
		// already-skipped procedural/generate-case construct -- so this
		// check is narrow enough to only ever fire here.
		p.advance() // label
		p.advance() // ":"
		p.advance() // "assert"/"assume"/"cover"/"restrict"
		p.skipProceduralConstruct()
		return nil, true
	}
	if tok.Kind == token.KindIdent {
		return p.parseVariableOrInstantiation()
	}
	if tok.Kind != token.KindKeyword {
		return nil, false
	}
	switch tok.Text {
	case "module", "macromodule":
		return oneDecl(p.parseContainer(ast.KindModule, "endmodule"))
	case "program":
		return oneDecl(p.parseContainer(ast.KindProgram, "endprogram"))
	case "interface":
		if p.peekAt(1).Text == "class" {
			return oneDecl(p.parseClass())
		}
		return oneDecl(p.parseContainer(ast.KindInterface, "endinterface"))
	case "class":
		return oneDecl(p.parseClass())
	case "virtual", "static", "rand", "randc", "const", "var", "local", "protected":
		// A class member or module-scope variable introduced by one or
		// more of these qualifiers, in any order -- see parseQualifiedDecl
		// and memberQualifierKeywords for why a single unified lookahead
		// replaces what used to be one case per keyword (each of which
		// only checked ONE fixed following token, so a qualifier
		// combination in an unexpected order either corrupted the parse or
		// silently dropped a flag).
		return p.parseQualifiedDecl()
	case "pure":
		// "pure constraint name;" (LRM 18.5.2, an abstract class's
		// constraint prototype) -- checked first since it has no "virtual"
		// in it at all, unlike "pure virtual function/task ...;" below.
		if p.peekAt(1).Text == "constraint" {
			p.advance() // "pure"
			return oneDecl(p.parseConstraintPrototype())
		}
		// "pure virtual function/task ...;" -- an abstract class method,
		// grammatically a prototype (no possible body) the same way extern
		// and a DPI import are, just spelled differently.
		if p.peekAt(1).Text != "virtual" {
			return nil, false
		}
		switch p.peekAt(2).Text {
		case "function":
			p.advance() // "pure"
			p.advance() // "virtual"
			return oneDecl(p.parseFunction(true))
		case "task":
			p.advance()
			p.advance()
			return oneDecl(p.parseTask(true))
		}
		return nil, false
	case "package":
		return oneDecl(p.parsePackage())
	case "typedef":
		return oneDecl(p.parseTypedef())
	case "parameter", "localparam":
		return p.parseParameterDecl()
	case "function":
		return oneDecl(p.parseFunction(false))
	case "task":
		return oneDecl(p.parseTask(false))
	case "extern":
		// "extern function/task ..." (LRM 8.24) or "extern constraint
		// name;" (LRM 18.5.1), optionally preceded by one or more method
		// qualifiers ("extern virtual function ...", "extern static task
		// ...", ...) -- looked ahead without consuming first, so a
		// malformed "extern" followed by neither a qualifier nor function/
		// task/constraint still consumes nothing, matching this function's
		// "ok=false means the cursor didn't move" contract.
		lookahead := 1
		for externMethodQualifierKeywords[p.peekAt(lookahead).Text] {
			lookahead++
		}
		switch p.peekAt(lookahead).Text {
		case "function":
			p.advance() // "extern"
			for externMethodQualifierKeywords[p.peek().Text] {
				p.advance() // virtual/static/protected/local/pure -- consumed, not tracked, same treatment as elsewhere in this parser
			}
			return oneDecl(p.parseFunction(true))
		case "task":
			p.advance() // "extern"
			for externMethodQualifierKeywords[p.peek().Text] {
				p.advance()
			}
			return oneDecl(p.parseTask(true))
		case "constraint":
			p.advance() // "extern"
			for externMethodQualifierKeywords[p.peek().Text] {
				p.advance()
			}
			return oneDecl(p.parseConstraintPrototype())
		}
		return nil, false
	case "import":
		if p.peekAt(1).Kind == token.KindStringLiteral {
			return oneDecl(p.parseDPIImport())
		}
		return p.parseImport()
	case "initial", "always", "always_comb", "always_ff", "always_latch", "final", "assign":
		// A procedural block, or a continuous assignment ("assign a = b;",
		// LRM 10.3 -- grammatically a statement too, not a procedural
		// block, but the exact same "one semicolon-terminated statement,
		// content never parsed" shape skipProceduralConstruct already
		// handles), at container scope -- never parsed, same declaration-
		// grade reasoning as a function/task body (see
		// skipProceduralConstruct), just without a keyword of its own
		// introducing a name to record. Recognizing and skipping these
		// explicitly, rather than falling through to the generic
		// unrecognized-token recovery, avoids that recovery cascading into
		// several spurious errors for one ordinary, extremely common
		// construct (every statement inside an unrecognized "initial
		// begin ... end" otherwise looks like its own bad top-level
		// declaration once recover() -- which doesn't understand begin/end
		// nesting -- stops at the first semicolon it finds).
		p.advance()
		p.skipProceduralConstruct()
		return nil, true
	case "assert", "assume", "cover", "restrict":
		// A module-scope concurrent assertion statement (LRM 16.14),
		// e.g. "assert property (@(posedge clk) req |-> ##1 ack) else
		// $error(...);" -- same "recognized and skipped, statement/
		// expression grammar out of scope" treatment as "assign" above,
		// and the identical skipProceduralConstruct shape handles its
		// action_block (an ordinary statement, ending at a top-level ';',
		// optionally itself starting with "else") with no changes needed.
		// A labeled form ("a1: assert property (...);") is dispatched
		// separately, before parseVariableOrInstantiation ever sees the
		// label -- see parseDecl's KindIdent check above.
		p.advance()
		p.skipProceduralConstruct()
		return nil, true
	case "constraint":
		return oneDecl(p.parseConstraint())
	case "covergroup":
		// A covergroup declaration (LRM 19.3): "covergroup name [(...)]
		// [@(...) | @@(...)]; coverpoint/cross/option items... endgroup [:
		// name]" -- never parsed, out of declaration-grade scope
		// (coverpoint/bins expression grammar is entirely out of scope,
		// same reasoning as a constraint block's body). Skipped wholesale,
		// header included, rather than trying to parse just the header the
		// way a container does, since the header's own "(...)"/"@(...)"
		// hold expression content this parser doesn't model either.
		p.advance()
		p.skipToKeyword("endgroup")
		p.skipEndLabel()
		return nil, true
	case "property":
		// A property declaration (LRM 16.6): "property name [(...)]; ...
		// endproperty [: name]" -- assertion expression grammar is entirely
		// out of scope, so the whole thing is skipped wholesale, same
		// treatment as "covergroup" above. Unambiguous with "assert
		// property (...)" (LRM 16.14's concurrent assertion statement,
		// found only inside/via a procedural construct or the also-skipped
		// "assert"/"assume"/"cover" module items), since that form always
		// starts with "assert"/"assume"/"cover", never with a bare
		// "property" token reaching this dispatch.
		p.advance()
		p.skipToKeyword("endproperty")
		p.skipEndLabel()
		return nil, true
	case "sequence":
		// A sequence declaration (LRM 16.11): "sequence name [(...)]; ...
		// endsequence [: name]" -- same treatment as "property" above.
		p.advance()
		p.skipToKeyword("endsequence")
		p.skipEndLabel()
		return nil, true
	case "clocking":
		// A clocking block (LRM 14.3): "clocking [name] @(...); ...
		// endclocking [: name]" -- skipped wholesale, header (including its
		// clocking-event expression) and all, same reasoning as
		// "covergroup"/"property" above. "default clocking name;" -- a
		// one-line reference to an already-declared clocking block, not a
		// declaration of its own -- is a different shape entirely, starting
		// with "default", not "clocking"; see the "default" case below.
		p.advance()
		p.skipToKeyword("endclocking")
		p.skipEndLabel()
		return nil, true
	case "default":
		// "default clocking name;" (LRM 14.3, a reference to an
		// already-declared clocking block) / "default disable iff expr;"
		// (LRM 16.15.2) -- both one-line statements ending at ';', not a
		// body of their own. ("default input|output ...;" skew
		// declarations exist too, LRM 14.3, but only ever appear nested
		// inside a clocking block's own body, which "clocking" above
		// already skips wholesale -- they never reach this dispatch
		// directly.) Anything else starting with "default" (e.g. a bare
		// "default:" case-statement label) only occurs inside a
		// procedural/generate-case construct already skipped before
		// parseDecl ever sees it, so no other shape reaches here.
		if p.peekAt(1).Text != "clocking" && p.peekAt(1).Text != "disable" {
			return nil, false
		}
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "checker":
		// A checker declaration (LRM 17.2): "checker name [(...)]; ...
		// endchecker [: name]" -- structurally a lot like a class/module (it
		// can contain some of the same declarations), but modeling that
		// overlap precisely is out of scope; skipped wholesale like the
		// other verification constructs above.
		p.advance()
		p.skipToKeyword("endchecker")
		p.skipEndLabel()
		return nil, true
	case "specify":
		// A specify block (LRM 31.2): "specify ... endspecify" -- timing
		// checks/path delays, entirely out of scope. No block label to
		// consume ("endspecify" has none, unlike the constructs above).
		p.advance()
		p.skipToKeyword("endspecify")
		return nil, true
	case "defparam":
		// "defparam u1.WIDTH = 8;" (LRM 23.10, legacy pre-parameter-override
		// hierarchical parameter assignment) -- never parsed, out of
		// declaration-grade scope (its target is a hierarchical path, not a
		// name this package's model resolves), skipped explicitly rather
		// than falling through to the generic unrecognized-token recovery.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "modport":
		// An interface's modport declaration (LRM 25.5): "modport name
		// (direction port [, ...]) [, name (...)];" -- never parsed, out of
		// declaration-grade scope (it names ports, not a new declaration of
		// its own this package models), but skipped explicitly rather than
		// falling through to the generic unrecognized-token recovery, since
		// nearly every interface has at least one and recover() offers no
		// special treatment for it.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "bind":
		// "bind target_scope [#(...)] bind_instantiation ...;" (LRM 23.11) --
		// a hierarchical binding directive (commonly used to attach
		// verification checkers to an existing instance), never parsed, out
		// of declaration-grade scope for the same reason "defparam" is
		// (its target is a hierarchical reference, not a declaration).
		// skipHeaderToSemi handles whatever shape follows uniformly: it's
		// depth-aware, so the bind_instantiation's own port-connection
		// parens don't confuse it.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "timeunit", "timeprecision":
		// "timeunit 1ns [/ 1ps];" / "timeprecision 1ps;" (LRM 3.14.2, a
		// module/interface/package/program/checker time-unit declaration)
		// -- never parsed, out of declaration-grade scope (timing metadata,
		// same reasoning as "'timescale"), skipped like every other
		// header-only construct here.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "export":
		// "export pkg::name;" / "export pkg::*;" (LRM 26.4, re-exporting an
		// imported package member) or "export \"DPI-C\" ... ;" (a DPI
		// export) -- neither introduces a declaration this package models
		// (the former references an existing import; DPI export's own
		// name/signature duplicates a function/task already declared
		// elsewhere), skipped like "import"'s DPI form's own qualifiers are
		// consumed-but-untracked elsewhere in this parser.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "let":
		// "let name(args) = expr;" (LRM 11.12, a macro-like expression
		// shorthand) -- never parsed, expression grammar out of scope same
		// as everywhere else in this parser; skipped explicitly rather
		// than falling through to the generic unrecognized-token recovery.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "alias":
		// "alias w1 = w2;" (LRM 10.11, a net alias) -- never parsed, same
		// reasoning as "let" above.
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	case "global":
		// "global clocking [name] @(...); ... endclocking" (LRM 14.14) --
		// only recognized in this specific "global clocking" shape (unlike
		// "clocking" above, "global" has other legitimate uses this parser
		// never reaches -- e.g. inside an already-skipped procedural/
		// generate construct -- so it's deliberately not treated as a
		// standalone recognized keyword the way "clocking" itself is).
		if p.peekAt(1).Text != "clocking" {
			return nil, false
		}
		p.advance() // "global"
		p.advance() // "clocking"
		p.skipToKeyword("endclocking")
		p.skipEndLabel()
		return nil, true
	case "generate", "endgenerate":
		// "generate"/"endgenerate" (LRM 27.3) are pure scope brackets around
		// an otherwise ordinary run of module items (instantiations,
		// variable/parameter declarations, generate for/if/case
		// constructs, ...) -- consumed as no-ops so everything between them
		// parses exactly as it would outside a generate region.
		p.advance()
		return nil, true
	case "begin", "end":
		// A generate block's own delimiters (LRM 27.3's "generate_block" ::
		// begin [ : name ] ... end [ : name ]), reached here only at
		// container/generate scope -- a procedural block's begin/end is
		// already consumed whole by skipProceduralConstruct/skipBody before
		// parseDecl ever sees it. Consumed as a no-op (plus an optional
		// ": name" block label) so a generate block's contents parse as
		// ordinary declarations, same reasoning as "generate"/"endgenerate"
		// above.
		p.advance()
		p.skipEndLabel()
		return nil, true
	case "else":
		// A generate-if's "else" (LRM 27.5): consumed as a no-op. What
		// follows -- "begin"/"if"/a bare single item -- is left for the
		// next parseBody iteration to dispatch on normally, which is what
		// naturally chains an "else if" and reaches the "begin"/"if" cases
		// above/below without this case needing to recurse.
		p.advance()
		return nil, true
	case "for", "if":
		// A generate for-loop or generate-if's condition/header (LRM 27.4,
		// 27.5) -- never parsed, same "declaration-grade, not full
		// expression/statement grammar" reasoning as everywhere else in
		// this parser. Skipped as a single balanced "(...)" group; what
		// follows (a "begin ... end" generate block, or -- LRM allows it --
		// a single bare generate item with no begin/end) is left for the
		// next parseBody iteration, which reaches the "begin" case above
		// or parses the single item as an ordinary declaration.
		p.advance()
		p.skipParenGroup()
		return nil, true
	case "case", "casex", "casez":
		// A generate-case construct (LRM 27.5): unlike for/if, its per-item
		// "expr : item" / "default : item" shape (item possibly a bare
		// statement with no begin/end at all) doesn't fit this parser's
		// declaration-oriented dispatch loop cleanly, so it's skipped
		// wholesale -- header and body together -- down to the matching
		// "endcase", rather than letting its items parse individually the
		// way for/if's do.
		p.advance()
		p.skipParenGroup()
		p.skipToKeyword("endcase")
		return nil, true
	}
	if gatePrimitiveKeywords[tok.Text] {
		p.advance()
		p.skipHeaderToSemi()
		return nil, true
	}
	if isVariableStartKeyword(tok.Text) {
		return p.parseVariableDecl()
	}
	return nil, false
}

// parseQualifiedDecl parses a class member or module-scope variable
// introduced by one or more of memberQualifierKeywords, in any order: a
// method ("[quals] function/task ..."), a constraint ("[quals] constraint
// name { ... }"), a virtual interface handle ("[quals] virtual
// [interface] Name[.modport] name;" -- see parseVirtualInterfaceDecl), or
// an ordinary (possibly rand/randc/static-qualified) variable/net
// declaration. The cursor is expected to be at the first qualifier
// keyword -- parseDecl's dispatch already confirmed that.
func (p *parser) parseQualifiedDecl() ([]ast.Decl, bool) {
	// "virtual class"/"virtual interface class" (a class DECLARATION, not
	// a class MEMBER) only ever starts with "virtual" as the very first
	// token -- a class declaration takes no qualifier prefix of its own.
	// Checked unconditionally before the qualifier-run scan below, same
	// as this parser's original "virtual" case did.
	if p.peek().Text == "virtual" &&
		(p.peekAt(1).Text == "class" || (p.peekAt(1).Text == "interface" && p.peekAt(2).Text == "class")) {
		return oneDecl(p.parseClass())
	}

	quals := make(map[string]bool)
	n := 0
	for memberQualifierKeywords[p.peekAt(n).Text] {
		quals[p.peekAt(n).Text] = true
		n++
	}
	next := p.peekAt(n)

	switch next.Text {
	case "function":
		for range n {
			p.advance() // qualifiers -- consumed, not tracked (rand/randc/const/var/local/protected never combine with a method; virtual/static do, same treatment as before this dispatch was unified)
		}
		return oneDecl(p.parseFunction(false))
	case "task":
		for range n {
			p.advance()
		}
		return oneDecl(p.parseTask(false))
	case "constraint":
		for range n {
			p.advance()
		}
		return oneDecl(p.parseConstraint())
	}

	if quals["virtual"] {
		// A virtual interface handle, possibly preceded by other
		// qualifiers ("protected virtual my_if vif;"). Consume everything
		// up to (not including) "virtual" itself -- parseVirtualInterfaceDecl
		// expects to start there, same as when it's reached directly with
		// no leading qualifiers. A qualifier written AFTER "virtual"
		// (e.g. "virtual protected my_if vif;") isn't specially supported.
		for p.peek().Text != "virtual" {
			p.advance()
		}
		if p.peekAt(1).Text == "interface" && p.peekAt(2).Kind == token.KindIdent {
			return p.parseVirtualInterfaceDecl()
		}
		if p.peekAt(1).Kind == token.KindIdent {
			return p.parseVirtualInterfaceDecl()
		}
		return nil, false
	}

	for range n {
		p.advance()
	}
	return p.parseVariableDeclQualified(quals["rand"], quals["randc"], quals["static"])
}

// oneDecl adapts a single-Decl parse function's result to parseDecl's
// slice-returning contract.
func oneDecl(d ast.Decl, ok bool) ([]ast.Decl, bool) {
	if !ok {
		return nil, false
	}
	return []ast.Decl{d}, true
}

// skipAttributeInstances consumes zero or more "(* name [= value], ... *)"
// attribute instance groups (LRM 5.12) preceding a declaration --
// synthesis/tool metadata, entirely out of scope for a declaration-grade
// parser, recognized only well enough to be skipped without corrupting
// the declaration that follows. The lexer already tokenizes "(*"/"*)" as
// their own fixed KindAttrOpen/KindAttrClose kinds (never confused with a
// regular '(' / ')', e.g. in an attribute value like "attr = foo(bar)"),
// so finding the matching close is just a scan to the next KindAttrClose,
// no depth tracking needed.
func (p *parser) skipAttributeInstances() {
	for p.peek().Kind == token.KindAttrOpen {
		p.advance() // "(*"
		for p.peek().Kind != token.KindAttrClose && p.peek().Kind != token.KindEOF {
			p.advance()
		}
		if p.peek().Kind == token.KindAttrClose {
			p.advance() // "*)"
		}
	}
}

// skipEndLabel consumes an optional ": identifier" suffix immediately
// following an already-consumed end keyword (LRM's "block identifiers" --
// endmodule/endinterface/endprogram/endpackage/endclass/endfunction/
// endtask may all optionally repeat the declaration's name after the
// closing keyword, e.g. "endmodule : top", purely for readability). This
// parser doesn't check that the repeated name actually matches the
// declaration's own -- purely syntactic, same as everywhere else -- it
// only needs to consume the suffix so it doesn't corrupt whatever
// declaration follows.
func (p *parser) skipEndLabel() {
	if p.peek().Kind != token.KindColon {
		return
	}
	p.advance() // ":"
	// The label is ordinarily a plain identifier, except for a class
	// constructor's "endfunction : new" -- "new" is lexed as a keyword
	// (it's the reserved new operator everywhere else), same dual-role
	// case parseFunction already special-cases when parsing the name
	// itself.
	if next := p.peek(); next.Kind == token.KindIdent || (next.Kind == token.KindKeyword && next.Text == "new") {
		p.advance()
	}
}

func (p *parser) parseContainer(kind ast.ContainerKind, endKeyword string) (ast.Decl, bool) {
	p.advance() // "module"/"macromodule"/"interface"/"program"
	if p.peek().Text == "automatic" || p.peek().Text == "static" {
		p.advance() // optional lifetime qualifier (LRM 23.2.1/24.3/26.2) -- consumed, not tracked, same treatment as a function/task's own lifetime qualifier
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	c := &ast.Container{ContainerKind: kind, Name: nameTok.Text}
	c.Position = namePosition(nameTok)

	// A "package_import_declaration" (LRM 26.3) may appear directly in a
	// module/interface/program header, before the parameter/port lists
	// ("module m import my_pkg::*; (...);") -- distinct from an ordinary
	// body-level import, which parseBody/parseDecl already handles once
	// the body starts below. Not consuming these here left parsePortList
	// looking at "import" (which it doesn't recognize at all) instead of
	// '(', dropping the entire port list and misparsing the header.
	var headerImports []ast.Decl
	for p.peek().Text == "import" {
		imports, ok := p.parseImport()
		if !ok {
			break
		}
		headerImports = append(headerImports, imports...)
	}

	c.Params = p.parseParamPortList()
	c.Ports = p.parsePortList()
	p.skipHeaderToSemiStrict() // consumes the header's trailing ';' (and, defensively, anything unexpected still remaining before it)

	body, end, _ := p.parseBody(endKeyword)
	c.Body = append(headerImports, body...)
	c.EndLine, c.EndCharacter = end.Line, end.Character
	return c, true
}

func (p *parser) parsePackage() (ast.Decl, bool) {
	p.advance() // "package"
	if p.peek().Text == "automatic" || p.peek().Text == "static" {
		p.advance() // optional lifetime qualifier (LRM 26.2) -- consumed, not tracked
	}
	nameTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	pkg := &ast.Package{Name: nameTok.Text}
	pkg.Position = namePosition(nameTok)

	p.skipHeaderToSemiStrict()

	body, end, _ := p.parseBody("endpackage")
	pkg.Body = body
	pkg.EndLine, pkg.EndCharacter = end.Line, end.Character
	return pkg, true
}

// skipHeaderToSemi consumes tokens up to and including the next top-
// level ';'. Shared by every header-only or single-statement construct
// this parser recognizes but doesn't parse -- "default clocking"/
// "disable iff", defparam, modport, bind, timeunit/timeprecision, export,
// let, alias, and gate/switch primitive instantiations (see their own
// cases in parseDecl's switch below) -- whose remaining content can
// legitimately itself start with what would otherwise look like a new
// declaration: e.g. an "export \"DPI-C\" function name;" DPI re-export's
// own required "function"/"task" keyword. This deliberately has no
// isDeclBoundaryKeyword awareness for that reason -- see
// skipHeaderToSemiStrict below for the narrower variant used where no
// such legitimate content is possible. Depth-aware ( { [ regardless, so
// a port list's or instantiation's own parens can't confuse it.
func (p *parser) skipHeaderToSemi() {
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			return
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		case token.KindSemi:
			if depth == 0 {
				p.advance()
				return
			}
		}
		p.advance()
	}
}

// skipHeaderToSemiStrict is skipHeaderToSemi generalized with awareness
// of isDeclBoundaryKeyword, for the subset of skipHeaderToSemi's callers
// -- parseContainer's header, parsePackage (which has no header content
// of its own at all), and all four parseTypedef* variants' trailing ';'
// -- where, by the time this runs, everything the header/typedef can
// legitimately contain (ports, parameter ports, header imports, the
// typedef's own name) has already been parsed, so nothing besides the
// terminator itself or malformed trailing content can remain. Without
// this, a missing ';' here silently runs straight through the next
// declaration's own tokens and steals them -- the same failure mode
// skipProceduralConstruct had, fixed the same way: a decl-boundary
// keyword at depth zero stops the scan (unconsumed, left for the next
// parseBody iteration to parse fresh) and records an error, and EOF with
// no ';' ever found is now an error too, rather than returning silently.
// No isTypeCastKeyword guard is needed here (contrast collectExprUntil)
// -- nothing expression-shaped, and so nothing that could contain a
// cast, is ever legitimately left for this to scan over.
func (p *parser) skipHeaderToSemiStrict() {
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected ';'")
			return
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		case token.KindSemi:
			if depth == 0 {
				p.advance()
				return
			}
		case token.KindKeyword:
			if depth == 0 && isDeclBoundaryKeyword(tok.Text) {
				p.errorf(tok, "expected ';' before %q", tok.Text)
				return
			}
		}
		p.advance()
	}
}

// skipParenGroup consumes a single balanced "(...)" group, the cursor
// expected to be at the opening '('. Used for a generate for/if/case
// construct's condition/header (LRM 27.4, 27.5) -- never parsed, same
// "declaration-grade, not full expression grammar" reasoning as
// everywhere else in this parser. A no-op if the cursor isn't at '(' at
// all (a malformed "for"/"if"/"case" with no header), left for whatever
// follows to fail its own dispatch and recover normally.
func (p *parser) skipParenGroup() {
	if p.peek().Kind != token.KindLParen {
		return
	}
	p.advance() // '('
	p.collectUntil(token.KindRParen)
	if p.peek().Kind == token.KindRParen {
		p.advance()
	}
}

// skipToKeyword consumes tokens up to and including the next occurrence
// of keyword at bracket/paren/brace depth zero. Used for a generate-case
// construct's body (see the "case" dispatch in parseDecl) and for the
// verification constructs (covergroup/property/sequence/clocking/
// specify/checker) parseDecl recognizes only well enough to skip --
// none of them are parsed item-by-item, matching skipBody's own
// "declaration-grade, not full grammar" treatment of a function/task
// body, including its same unexpected-EOF error if keyword is never
// reached.
func (p *parser) skipToKeyword(keyword string) {
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == token.KindEOF {
			p.errorf(tok, "unexpected end of file, expected %s", keyword)
			return
		}
		if depth == 0 && tok.Kind == token.KindKeyword && tok.Text == keyword {
			p.advance()
			return
		}
		switch tok.Kind {
		case token.KindLParen, token.KindLBrace, token.KindLBrack, token.KindTickLBrace:
			depth++
		case token.KindRParen, token.KindRBrace, token.KindRBrack:
			if depth > 0 {
				depth--
			}
		}
		p.advance()
	}
}
