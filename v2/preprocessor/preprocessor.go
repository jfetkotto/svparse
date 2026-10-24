// Package preprocessor implements SV compiler-directive handling on top
// of svparse/lexer's token stream: macro definition and expansion,
// conditional compilation, and “ `include “ resolution, all with
// source maps so an expanded or included token still resolves back to a
// real, editor-true position rather than "somewhere inside a macro
// definition" or "the position “ `include “ itself sat at". Like the
// lexer, it's error-tolerant: malformed directives are recorded and
// skipped, never fatal.
//
// This package is purely syntactic, same as the rest of svparse: it has
// no notion of a workspace, multiple files beyond what “ `include “
// itself pulls in, or which macros "should" already be defined coming
// in. A caller wanting company-workspace-wide `+define+`/`+incdir+`
// semantics (as sigils's filelist convention allows) builds that on top,
// via IncludeResolver and whatever it seeds into a fresh call to
// Preprocess.
package preprocessor

import (
	"fmt"
	"strconv"

	"github.com/jfetkotto/svparse/v2/lexer"
	"github.com/jfetkotto/svparse/v2/token"
)

// Token is one preprocessed token. Kind/Text/Line/Character (embedded
// from token.Token) always describe its true position in File -- they
// are never rewritten by expansion. MacroName/InvokedFrom are set iff
// this token is the result of expanding a macro, letting a caller walk
// back to the real invocation site (or, if that invocation was itself
// inside another expansion, further back still) instead of stopping at
// "somewhere inside a macro definition".
type Token struct {
	token.Token
	File        string
	MacroName   string
	InvokedFrom *Token
}

// Error is a non-fatal problem noticed while preprocessing (a malformed
// directive, an unbalanced “ `ifdef“/“ `endif“, ...). Preprocess never
// stops or panics because of one.
type Error struct {
	File      string
	Line      int
	Character int
	Message   string
}

// IncludeResolver resolves an “ `include “d path (as written in source)
// against the file that referenced it, returning the included file's
// text and a canonical path used both for source-map attribution and
// include-cycle detection. Preprocess never touches a filesystem
// directly -- this is the only seam a workspace-specific caller
// implements, mirroring how sigils's own workspace.FilelistDiscoverer
// already resolves and dedupes paths in its own domain.
type IncludeResolver interface {
	Resolve(includedPath, fromFile string) (text, resolvedPath string, err error)
}

// tokenSource is one entry in the preprocessor's expansion stack: a
// flat, already position-tagged token slice plus a cursor. A lexed file,
// an expanded macro invocation, and an “ `include “d file's tokens are
// all represented this way, so one iterative main loop handles any of
// them uniformly instead of recursing.
type tokenSource struct {
	toks  []Token
	pos   int
	onPop func() // e.g. clearing include-cycle or macro-self-reference tracking
}

type preprocessor struct {
	keepComments bool
	comments     []Comment // see Options.KeepComments

	stack       []*tokenSource
	macros      map[string]*macroDef
	expanding   map[string]bool
	including   map[string]bool // resolved paths currently on the active include stack -- see handleInclude
	cond        []condFrame
	resolver    IncludeResolver
	designDepth int               // open module/interface/... declarations, for `resetall
	keywords    []map[string]bool // open `begin_keywords regions; nil = all SystemVerilog keywords

	out  []Token
	errs []Error

	// steps counts main-loop iterations, i.e. tokens consumed from any
	// source, and maxSteps bounds it -- see maxPreprocessSteps.
	steps    int
	maxSteps int
}

// maxPreprocessSteps bounds the total work one Preprocess call may do.
//
// The expanding/including guards stop a macro or include that references
// ITSELF, but nothing stops non-recursive exponential growth: "`define A
// `B `B", "`define B `C `C", thirty levels deep, expands to 2^30 tokens
// with no cycle anywhere. This library promises never to panic or stop on
// malformed source, and without a budget it keeps that promise the wrong
// way -- it doesn't panic, it just never returns, and a language server
// indexing a whole workspace loses a worker and its memory to one file.
//
// The limit is deliberately far above any real input: a 10k-line
// SystemVerilog file is on the order of 100k tokens, so even a deep
// include tree stays orders of magnitude under this.
const maxPreprocessSteps = 20_000_000

// Preprocess preprocesses text (path's contents, already read by the
// caller -- e.g. an open editor buffer or a disk read, same "open buffer
// wins" pattern a caller likely already applies before calling this) and
// every file it transitively includes via resolver.
func Preprocess(path, text string, resolver IncludeResolver) ([]Token, []Error) {
	return PreprocessWithOptions(path, text, resolver, Options{})
}

// Options carries preprocessing configuration beyond the always-required
// path/text/resolver, kept separate so adding a new option never breaks
// Preprocess's existing callers.
type Options struct {
	// InitialMacros seeds object-like macros as if each had been
	// `` `define ``d before the first token of path -- the preprocessing
	// equivalent of a compiler's "-D" / "+define+" command-line flags. A
	// map value of "" defines the name with an empty body (`` `define FOO ``,
	// no replacement text); a non-empty value is lexed into the macro's
	// body tokens (`` `define FOO <value> ``). There is no way to seed a
	// function-like macro this way, matching +define+'s own CLI-level
	// semantics -- it only ever defines a name to a flat token sequence.
	InitialMacros map[string]string

	// KeepComments makes PreprocessDetailed return the comments of the file and
	// of everything it includes. It has no effect on PreprocessWithOptions,
	// which has nowhere to return them, and it never changes the tokens.
	KeepComments bool
}

// Comment is a source comment with the file it is in. Its position is its true
// position in File, like a Token's.
type Comment struct {
	token.Comment
	File string
}

// Result is everything a preprocessing run produces.
type Result struct {
	Tokens   []Token
	Errors   []Error
	Comments []Comment // only with Options.KeepComments, in the order the sources were read
}

// PreprocessDetailed is PreprocessWithOptions that returns a Result, so it can
// also hand back the comments (see Options.KeepComments). The tokens and errors
// are exactly what PreprocessWithOptions returns.
//
// Comments come from every source text read: the file, and each `include in
// the order it was reached. A comment inside an inactive `ifdef branch is
// still returned, since it is still in the file; one inside a `define body is
// returned once, where it is written, not once per expansion. A file that is
// included twice has its comments returned twice; see CommentIndex.
func PreprocessDetailed(path, text string, resolver IncludeResolver, opts Options) Result {
	p := newPreprocessor(resolver)
	p.keepComments = opts.KeepComments
	p.seedInitialMacros(opts.InitialMacros)
	p.including[path] = true
	p.pushFile(path, text)
	p.run()
	p.reportUnterminatedConditionals()
	return Result{Tokens: p.out, Errors: p.errs, Comments: p.comments}
}

// PreprocessWithOptions is Preprocess with additional configuration -- see
// Options.
func PreprocessWithOptions(path, text string, resolver IncludeResolver, opts Options) ([]Token, []Error) {
	// Comments have nowhere to go here, so don't spend time collecting them.
	opts.KeepComments = false
	r := PreprocessDetailed(path, text, resolver, opts)
	return r.Tokens, r.Errors
}

// newPreprocessor builds a preprocessor with the default step budget.
// Separate from PreprocessWithOptions so a test can lower maxSteps
// without having to drive a 20-million-token expansion to prove the
// budget works.
func newPreprocessor(resolver IncludeResolver) *preprocessor {
	return &preprocessor{
		macros:    make(map[string]*macroDef),
		expanding: make(map[string]bool),
		including: make(map[string]bool),
		resolver:  resolver,
		maxSteps:  maxPreprocessSteps,
	}
}

// initialMacroFile tags the defFile/File of anything seeded via
// Options.InitialMacros, distinguishing it from a real source position in
// error messages and source-map attribution.
const initialMacroFile = "<command-line>"

func (p *preprocessor) seedInitialMacros(macros map[string]string) {
	for name, value := range macros {
		body := p.lexToTokens(initialMacroFile, value)
		// lexToTokens (like lexer.Lex) always appends a trailing KindEOF
		// sentinel -- fine for a tokenSource (run's main loop skips
		// KindEOF explicitly), but macroDef.body is consumed directly by
		// substitute with no such filter, so it must not carry one here
		// the way a `` `define ``-parsed body (built by collectLogicalLine,
		// which stops before EOF) never does.
		if n := len(body); n > 0 && body[n-1].Kind == token.KindEOF {
			body = body[:n-1]
		}
		p.macros[name] = &macroDef{
			name:    name,
			body:    body,
			defFile: initialMacroFile,
		}
	}
}

// lexToTokens lexes text (from file) into preprocessor Tokens, all
// tagged with File and no macro attribution -- shared by the root push
// and “ `include “, the only two places raw text becomes a token
// source (a macro expansion's source is built by substitution instead,
// see expandMacro).
func (p *preprocessor) lexToTokens(file, text string) []Token {
	var lexToks []token.Token
	var lexErrs []lexer.Error
	if p.keepComments && file != initialMacroFile {
		var comments []token.Comment
		lexToks, comments, lexErrs = lexer.LexWithComments(text)
		for _, c := range comments {
			p.comments = append(p.comments, Comment{Comment: c, File: file})
		}
	} else {
		lexToks, lexErrs = lexer.Lex(text)
	}
	for _, e := range lexErrs {
		p.errorf(file, e.Line, e.Character, "%s", e.Message)
	}
	toks := make([]Token, len(lexToks))
	for i, t := range lexToks {
		toks[i] = Token{Token: t, File: file}
	}
	return toks
}

func (p *preprocessor) pushFile(path, text string) {
	p.stack = append(p.stack, &tokenSource{toks: p.lexToTokens(path, text)})
}

func (p *preprocessor) run() {
	for len(p.stack) > 0 {
		src := p.stack[len(p.stack)-1]
		if src.pos >= len(src.toks) {
			p.stack = p.stack[:len(p.stack)-1]
			if src.onPop != nil {
				src.onPop()
			}
			continue
		}
		p.steps++
		if p.steps > p.maxSteps {
			tok := src.toks[src.pos]
			p.errorf(tok.File, tok.Line, tok.Character,
				"preprocessing exceeded %d tokens; giving up (runaway macro expansion?)", p.maxSteps)
			p.stack = nil
			return
		}

		tok := src.toks[src.pos]
		src.pos++

		switch {
		case tok.Kind == token.KindEOF:
			// Each source's own EOF sentinel is just a boundary marker
			// (lexer.Lex always appends one) -- never emitted.
		case tok.Kind == token.KindDirective:
			p.handleDirective(tok, src)
		case tok.Kind == token.KindPaste || tok.Kind == token.KindMacroQuote || tok.Kind == token.KindMacroEscQuote:
			// A “ `` “/“ `" “ reaching the main loop directly (rather than
			// via expandMacro pushing a macroDef.body -- see substitute,
			// which always resolves these away before the result is
			// pushed) means one appeared outside any `define body, where
			// it's meaningless. Dropped like any other token would be
			// inside an inactive `ifdef branch (no error there, same as
			// everywhere else); recorded as an error when active, rather
			// than silently passed through to a parser that has never
			// heard of either kind.
			if p.active() {
				p.errorf(tok.File, tok.Line, tok.Character, "%q is only meaningful inside a `define body", tok.Text)
			}
		case p.active():
			p.emit(tok)
		}
	}
}

// knownArgumentDirectives are compiler directives (LRM chapter 22) this
// preprocessor recognizes by name but doesn't parse the argument grammar
// of -- synthesis/tool/timing metadata, entirely out of scope for a
// declaration-grade parser, same reasoning as skipAttributeInstances in
// the parser package. Recognizing them by name (rather than treating them
// like any other backtick-prefixed token) is what lets handleDirective
// safely discard the rest of their line: an unrecognized backtick-
// prefixed token might just be an inline reference to an undefined macro
// (e.g. “ `WIDTH “ inside "logic [`WIDTH-1:0] data;"), which must NOT
// have the rest of its line eaten, so only directives named here get that
// treatment.
var knownArgumentDirectives = map[string]bool{
	"timescale":           true,
	"default_nettype":     true,
	"unconnected_drive":   true,
	"nounconnected_drive": true,
	"pragma":              true,
	"line":                true,
	"celldefine":          true,
	"endcelldefine":       true,
	"resetall":            true,
}

// handleDirective dispatches a KindDirective token by name. Every SV
// macro invocation is ALSO a KindDirective token at the lexical level
// (“ `WIDTH“, “ `FOO(a,b)“ -- a macro reference is backtick-prefixed,
// lexically indistinguishable from a real directive until its name is
// looked up), which is why macro expansion lives in this dispatch's
// fallback case rather than being triggered by KindIdent tokens.
func (p *preprocessor) handleDirective(tok Token, src *tokenSource) {
	switch tok.Text {
	case "ifdef":
		p.handleIfdef(tok, src, false)
	case "ifndef":
		p.handleIfdef(tok, src, true)
	case "elsif":
		p.handleElsif(tok, src)
	case "else":
		p.handleElse(tok)
	case "endif":
		p.handleEndif(tok)
	default:
		if !p.active() {
			return // define/undef/macro-expansion are all skipped inside a false `ifdef branch; conditional-control directives above are the only ones always processed, to keep `ifdef/`endif nesting balanced through the skip.
		}
		switch tok.Text {
		case "define":
			p.handleDefine(tok, src)
		case "undef":
			p.handleUndef(tok, src)
		case "undefineall":
			p.macros = make(map[string]*macroDef)
		case "include":
			p.handleInclude(tok, src)
		case "begin_keywords":
			p.handleBeginKeywords(tok, src)
		case "end_keywords":
			p.handleEndKeywords(tok, src)
		case "__FILE__":
			// LRM 22.13: the name of the file being compiled, as a string.
			t := tok
			t.Kind, t.Text = token.KindStringLiteral, `"`+tok.File+`"`
			p.emit(t)
		case "__LINE__":
			// LRM 22.13: the line number, counted from 1.
			t := tok
			t.Kind, t.Text = token.KindIntLiteral, strconv.Itoa(tok.Line+1)
			p.emit(t)
		default:
			if def, ok := p.macros[tok.Text]; ok && !p.expanding[tok.Text] {
				p.expandMacro(def, tok, src)
				return
			}
			if knownArgumentDirectives[tok.Text] {
				// A recognized directive whose argument grammar this
				// preprocessor doesn't parse, but whose name and argument
				// tokens (the rest of this logical line, possibly empty --
				// e.g. `resetall`/`celldefine` take none) still shouldn't
				// leak into the output stream, where a parser has no way to
				// make sense of them ("wire" left dangling after
				// `default_nettype wire`). Consistent with every
				// conditional/macro-handled directive above, none of which
				// produce output tokens of their own either.
				p.checkDirective(tok, p.collectLogicalLine(src, tok.Line))
				return
			}
			if v, ok := coverageConstants[tok.Text]; ok {
				// LRM 20.14 predefines these; emitted at the reference's own
				// position, as a literal, so the tokens sit inside whatever
				// expression holds them. A macro of the same name wins (above).
				if v < 0 {
					minus := tok
					minus.Kind, minus.Text = token.KindSub, "-"
					p.emit(minus)
					v = -v
				}
				num := tok
				num.Kind, num.Text = token.KindIntLiteral, strconv.Itoa(v)
				p.emit(num)
				return
			}
			// A truly unrecognized directive, or a bare reference to an
			// undefined/currently-self-expanding macro (indistinguishable
			// from each other lexically -- both are just a backtick-
			// prefixed identifier): pass through as a single token rather
			// than erroring or consuming the rest of the line, since this
			// might just be an inline macro reference inside a larger
			// expression ("logic [`WIDTH-1:0] data;" with WIDTH never
			// `defined), not a line-level directive.
			p.out = append(p.out, tok)
		}
	}
}

// next returns the next token from src, or ok=false if src is exhausted
// (including hitting its own EOF sentinel). Directive-argument parsing
// (a macro name, a body, ...) never crosses into a different source on
// the stack -- a directive that runs off the end of its own file is
// simply malformed, and its argument list ends there.
func (p *preprocessor) next(src *tokenSource) (tok Token, ok bool) {
	if src.pos >= len(src.toks) || src.toks[src.pos].Kind == token.KindEOF {
		return Token{}, false
	}
	tok = src.toks[src.pos]
	src.pos++
	return tok, true
}

// peek returns the next token from src without consuming it, or
// ok=false on the same terms as next.
func (p *preprocessor) peek(src *tokenSource) (tok Token, ok bool) {
	if src.pos >= len(src.toks) || src.toks[src.pos].Kind == token.KindEOF {
		return Token{}, false
	}
	return src.toks[src.pos], true
}

func (p *preprocessor) errorf(file string, line, char int, format string, args ...any) {
	p.errs = append(p.errs, Error{File: file, Line: line, Character: char, Message: fmt.Sprintf(format, args...)})
}

// coverageConstants are the constants of the coverage control system functions
// (LRM 20.14, "Coverage system functions").
var coverageConstants = map[string]int{
	"SV_COV_START": 0, "SV_COV_STOP": 1, "SV_COV_RESET": 2, "SV_COV_CHECK": 3,
	"SV_COV_MODULE": 10, "SV_COV_HIER": 11,
	"SV_COV_ASSERTION": 20, "SV_COV_FSM_STATE": 21, "SV_COV_STATEMENT": 22, "SV_COV_TOGGLE": 23,
	"SV_COV_ERROR": -2, "SV_COV_NOCOV": -1, "SV_COV_OK": 0, "SV_COV_PARTIAL": 2,
}
