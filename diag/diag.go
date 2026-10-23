// Package diag is the one diagnostic type every stage reports through: the
// preprocessor and parser today, and semantic analysis, elaboration and lint as
// they arrive. A consumer such as an LSP server converts a Diagnostic once
// instead of one per stage.
//
// Every diagnostic has a Code, a stable kebab-case identifier ("syntax-error",
// "undeclared-identifier"). Codes are what configuration files, suppression
// comments and tests refer to, so a code is never renamed or reused for a
// different problem; see Known.
package diag

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jfetkotto/svparse/ast"
)

// Severity ranks a diagnostic. The values are ordered from most to least
// serious, so a smaller value is more severe.
type Severity int

const (
	Error Severity = iota
	Warning
	Info
	Hint
)

var severityNames = [...]string{Error: "error", Warning: "warning", Info: "info", Hint: "hint"}

// String returns the lower-case name of s, or "unknown" for a value that is
// not a Severity.
func (s Severity) String() string {
	if s < 0 || int(s) >= len(severityNames) {
		return "unknown"
	}
	return severityNames[s]
}

// MarshalText implements encoding.TextMarshaler, so a Severity encodes as its
// name in JSON.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// ParseSeverity is the inverse of Severity.String. It reports false for a name
// that is not a severity.
func ParseSeverity(name string) (Severity, bool) {
	for i, n := range severityNames {
		if n == name {
			return Severity(i), true
		}
	}
	return 0, false
}

// Related points at a second location that explains a diagnostic, such as the
// first declaration a duplicate clashes with.
type Related struct {
	Range   ast.Position
	Message string
}

// Diagnostic is one problem found in the source.
//
// Range is zero-width, starting and ending at the same place, when the
// reporting stage knows only a point (as the parser and preprocessor do).
// Lines and columns are zero-based, columns in UTF-16 units, as in ast.
type Diagnostic struct {
	Code     string
	Severity Severity
	Range    ast.Position
	Message  string
	Related  []Related
}

// String formats d as "file:line:col: severity[code]: message" with one-based
// line and column, the form compilers print.
func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d:%d: %s[%s]: %s", d.Range.File, d.Range.Line+1, d.Range.Character+1, d.Severity, d.Code, d.Message)
}

// Codes of the diagnostics the front end itself produces. Later stages add
// theirs to Known.
const (
	CodeSyntaxError       = "syntax-error"
	CodePreprocessorError = "preprocessor-error"
	CodeInternalError     = "internal-error"

	CodeDuplicateDefinition  = "duplicate-definition"
	CodeDuplicateDeclaration = "duplicate-declaration"
	CodeUndeclared           = "undeclared-identifier"
	CodeNotConstant          = "not-constant"
	CodeIncompatibleTypes    = "incompatible-types"
	CodeCastRequired         = "cast-required"
	CodeInvalidType          = "invalid-type"
	CodeConstantLimit        = "constant-limit"
	CodeCircularParameter    = "circular-parameter"
	CodeDivideByZero         = "divide-by-zero"

	CodeInvalidLValue           = "invalid-lvalue"
	CodeAssignToInput           = "assign-to-input"
	CodeAssignToConstant        = "assign-to-constant"
	CodeProceduralNet           = "procedural-assign-to-net"
	CodeMultipleDrivers         = "multiple-drivers"
	CodeBreakOutsideLoop        = "break-outside-loop"
	CodeContinueOutsideLoop     = "continue-outside-loop"
	CodeReturnValueInVoid       = "return-value-in-void"
	CodeMissingReturnValue      = "missing-return-value"
	CodeReturnOutsideSubroutine = "return-outside-subroutine"
	CodeTimingInFunction        = "timing-in-function"
)

// Known maps every registered code to a one-line description. A code must be
// registered here before anything reports it; the tests enforce the kebab-case
// form and that the constants above are present.
var Known = map[string]string{
	CodeSyntaxError:          "the parser could not make sense of the source",
	CodePreprocessorError:    "a compiler directive is malformed or unbalanced",
	CodeInternalError:        "a stage failed unexpectedly; this is a bug in svparse",
	CodeDuplicateDefinition:  "a module, interface, program, primitive, package or configuration name is defined twice",
	CodeDuplicateDeclaration: "a name is declared twice in the same scope",
	CodeUndeclared:           "an identifier is not declared in any enclosing scope",
	CodeNotConstant:          "an expression required to be constant is not",
	CodeIncompatibleTypes:    "a value cannot be converted to the type required",
	CodeCastRequired:         "a conversion is legal only with an explicit cast",
	CodeInvalidType:          "a data type could not be resolved",
	CodeConstantLimit:        "constant evaluation exceeded its step or depth limit",
	CodeCircularParameter:    "a parameter's value depends on itself",
	CodeDivideByZero:         "a constant expression divides by zero",

	CodeInvalidLValue:           "the target of an assignment is not something that can be assigned",
	CodeAssignToInput:           "an input port is assigned from inside its module",
	CodeAssignToConstant:        "a parameter, enum constant or genvar is assigned",
	CodeProceduralNet:           "a net is assigned in a procedural statement",
	CodeMultipleDrivers:         "a variable has drivers the language does not allow together",
	CodeBreakOutsideLoop:        "break is used outside a loop",
	CodeContinueOutsideLoop:     "continue is used outside a loop",
	CodeReturnValueInVoid:       "return gives a value in a void function, a task or a constructor",
	CodeMissingReturnValue:      "return gives no value in a function that returns one",
	CodeReturnOutsideSubroutine: "return is used outside a function or task",
	CodeTimingInFunction:        "a function contains a delay, event control or wait",
}

// ValidCode reports whether code is well-formed: lower-case words joined by
// single hyphens.
func ValidCode(code string) bool {
	if code == "" {
		return false
	}
	for _, word := range strings.Split(code, "-") {
		if word == "" {
			return false
		}
		for _, r := range word {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}

// Less reports whether a sorts before b: by file, then position, then code,
// then message, so output is deterministic whatever order stages ran in.
func Less(a, b Diagnostic) bool {
	switch {
	case a.Range.File != b.Range.File:
		return a.Range.File < b.Range.File
	case a.Range.Line != b.Range.Line:
		return a.Range.Line < b.Range.Line
	case a.Range.Character != b.Range.Character:
		return a.Range.Character < b.Range.Character
	case a.Code != b.Code:
		return a.Code < b.Code
	}
	return a.Message < b.Message
}

// Sort orders ds with Less, in place.
func Sort(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool { return Less(ds[i], ds[j]) })
}

// Set accumulates diagnostics from several stages and drops exact duplicates,
// which arise when two stages notice the same problem or a file is analysed
// twice. The zero value is ready to use.
type Set struct {
	list []Diagnostic
	seen map[string]bool
}

// Add records d unless an identical diagnostic (same code, range and message)
// is already present. It reports whether d was new.
func (s *Set) Add(d Diagnostic) bool {
	key := fmt.Sprintf("%s|%s|%d|%d|%d|%d|%s", d.Code, d.Range.File, d.Range.Line, d.Range.Character, d.Range.EndLine, d.Range.EndCharacter, d.Message)
	if s.seen[key] {
		return false
	}
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.seen[key] = true
	s.list = append(s.list, d)
	return true
}

// AddAll records every diagnostic in ds.
func (s *Set) AddAll(ds []Diagnostic) {
	for _, d := range ds {
		s.Add(d)
	}
}

// Sorted returns the recorded diagnostics in Less order. The result is a copy.
func (s *Set) Sorted() []Diagnostic {
	out := append([]Diagnostic(nil), s.list...)
	Sort(out)
	return out
}

// Len is the number of distinct diagnostics recorded.
func (s *Set) Len() int { return len(s.list) }

// HasErrors reports whether any recorded diagnostic has Error severity.
func (s *Set) HasErrors() bool {
	for _, d := range s.list {
		if d.Severity == Error {
			return true
		}
	}
	return false
}
