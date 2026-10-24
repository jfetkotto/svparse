package diag

import (
	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
)

// FromParser converts parser errors to diagnostics with code "syntax-error".
// The parser reports a point, so each Range is zero-width.
func FromParser(errs []parser.Error) []Diagnostic {
	out := make([]Diagnostic, len(errs))
	for i, e := range errs {
		out[i] = point(CodeSyntaxError, e.File, e.Line, e.Character, e.Message)
	}
	return out
}

// FromPreprocessor converts preprocessor errors to diagnostics with code
// "preprocessor-error".
func FromPreprocessor(errs []preprocessor.Error) []Diagnostic {
	out := make([]Diagnostic, len(errs))
	for i, e := range errs {
		out[i] = point(CodePreprocessorError, e.File, e.Line, e.Character, e.Message)
	}
	return out
}

func point(code, file string, line, char int, msg string) Diagnostic {
	return Diagnostic{
		Code:     code,
		Severity: Error,
		Range:    ast.Position{File: file, Line: line, Character: char, EndLine: line, EndCharacter: char},
		Message:  msg,
	}
}
