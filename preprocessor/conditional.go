package preprocessor

import "github.com/jfetkotto/svparse/token"

// condFrame tracks one open “ `ifdef “/“ `ifndef “ chain: parentActive
// records whether the enclosing scope was already active when this frame
// opened (so a nested conditional correctly stays inactive throughout if
// an outer branch is already being skipped), anyBranchTaken records
// whether some branch in this chain has already fired -- forcing every
// subsequent “ `elsif“/“ `else “ in the same chain inactive regardless
// of its own condition -- and active is whether THIS branch is currently
// emitting.
type condFrame struct {
	parentActive   bool
	anyBranchTaken bool
	active         bool

	// Where the chain opened, so an unterminated one can be reported
	// against its own directive rather than against end of file -- see
	// reportUnterminatedConditionals.
	file      string
	line      int
	character int
}

// active reports whether the preprocessor is currently in an emitting
// (non-skipped) region. Checking just the top frame is sufficient: each
// frame's own active is computed as parentActive && <its own condition>,
// so it already folds in every ancestor transitively.
func (p *preprocessor) active() bool {
	if len(p.cond) == 0 {
		return true
	}
	return p.cond[len(p.cond)-1].active
}

func (p *preprocessor) handleIfdef(directiveTok Token, src *tokenSource, negate bool) {
	nameTok, ok := p.next(src)
	if !ok || nameTok.Kind != token.KindIdent {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`ifdef/`ifndef not followed by a macro name")
		// Still push a frame -- an `endif later in the file must balance
		// against SOMETHING, even for malformed input.
		p.cond = append(p.cond, condFrame{
			parentActive: p.active(), active: false,
			file: directiveTok.File, line: directiveTok.Line, character: directiveTok.Character,
		})
		return
	}

	_, defined := p.macros[nameTok.Text]
	if negate {
		defined = !defined
	}
	parentActive := p.active()
	p.cond = append(p.cond, condFrame{
		parentActive: parentActive, anyBranchTaken: defined, active: parentActive && defined,
		file: directiveTok.File, line: directiveTok.Line, character: directiveTok.Character,
	})
}

// reportUnterminatedConditionals records one error per conditional chain
// still open when the run finishes.
//
// Without it an unterminated conditional is the quietest possible failure:
// every token after the directive is skipped (active() stays false), so
// the file arrives at the parser with its declarations simply gone, and
// the error list is EMPTY -- nothing anywhere says why. The mirror case,
// an extra endif, has always been reported (see handleEndif); this is the
// missing direction, and the one an editor hits constantly while a
// conditional is still being typed.
func (p *preprocessor) reportUnterminatedConditionals() {
	for _, frame := range p.cond {
		p.errorf(frame.file, frame.line, frame.character, "unterminated `ifdef/`ifndef, expected `endif")
	}
}

func (p *preprocessor) handleElsif(directiveTok Token, src *tokenSource) {
	if len(p.cond) == 0 {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`elsif with no matching `ifdef/`ifndef")
		return
	}
	top := &p.cond[len(p.cond)-1]
	nameTok, ok := p.next(src)
	if !ok || nameTok.Kind != token.KindIdent {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`elsif not followed by a macro name")
		// A malformed `elsif's condition is treated as false (not taken),
		// same as a well-formed one naming an undefined macro -- it must
		// NOT leave the frame's active/anyBranchTaken untouched, or the
		// PREVIOUS branch (already decided at this point) would stay
		// active/inactive as if this `elsif had never appeared, letting
		// both branches' content emit if the previous branch happened to
		// be active.
		top.active = false
		return
	}

	if top.anyBranchTaken {
		top.active = false
		return
	}
	_, defined := p.macros[nameTok.Text]
	top.active = top.parentActive && defined
	if defined {
		top.anyBranchTaken = true
	}
}

func (p *preprocessor) handleElse(directiveTok Token) {
	if len(p.cond) == 0 {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`else with no matching `ifdef/`ifndef")
		return
	}
	top := &p.cond[len(p.cond)-1]
	if top.anyBranchTaken {
		top.active = false
		return
	}
	top.active = top.parentActive
	top.anyBranchTaken = true
}

func (p *preprocessor) handleEndif(directiveTok Token) {
	if len(p.cond) == 0 {
		p.errorf(directiveTok.File, directiveTok.Line, directiveTok.Character, "`endif with no matching `ifdef/`ifndef")
		return
	}
	p.cond = p.cond[:len(p.cond)-1]
}
