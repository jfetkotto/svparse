package sema

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// checkStream reports a streaming concatenation wider than the integral
// variable it is packed into (LRM 11.4.14.3). An element whose width is not
// known, or one with an array range, makes the width unknown and the check is
// skipped.
func (c *checker) checkStream(s *Scope, target Type, st *ast.Streaming) {
	if s == nil || !IsIntegral(target) {
		return
	}
	tw, ok := Bits(target)
	if !ok {
		return
	}
	total := 0
	for _, el := range st.Elems {
		if _, with := el.(*ast.WithRange); with {
			return
		}
		be, _ := c.m.Bind(s, el)
		if be == nil {
			return
		}
		w, ok := Bits(be.Type())
		if !ok || !IsIntegral(be.Type()) {
			return
		}
		total += w
	}
	if total > tw {
		c.errorf(diag.CodeInvalidExpression, st.Pos(), "the stream is %d bits wide and does not fit the %d-bit target (LRM 11.4.14.3)", total, tw)
	}
}
