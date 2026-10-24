package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

// MaxImplemented is the latest pipeline stage svparse implements. Run clamps
// every test to it, so a test that exercises a later stage is judged on what
// can be checked today; raising it is how a new stage (semantic checks,
// elaboration) is switched on for the whole corpus at once.
const MaxImplemented = StageElaboration

// Result is the outcome of running one file.
type Result struct {
	// Ran is the latest stage actually executed (the file's highest declared
	// stage, clamped to MaxImplemented).
	Ran Stage

	// Failed is true if any executed stage reported a diagnostic.
	Failed bool

	// Diagnostics are the human-readable diagnostics behind Failed.
	Diagnostics []string
}

// Run pushes src (the file at rel, relative to root) through the pipeline up
// to the highest stage both the file's metadata and svparse support.
//
// Includes resolve first relative to the including file's directory, then
// against each of the metadata's :incdirs:, all under root.
func Run(root, rel, src string, meta Metadata) Result {
	ran := min(meta.HighestStage(), MaxImplemented)
	res := Result{Ran: ran}
	if ran == 0 {
		return res
	}

	toks, ppErrs := preprocessor.PreprocessWithOptions(rel, src,
		includeResolver{root: root, incDirs: meta.IncDirs},
		preprocessor.Options{InitialMacros: meta.Defines})
	for _, e := range ppErrs {
		res.Diagnostics = append(res.Diagnostics, fmt.Sprintf("preprocess %s:%d:%d: %s", e.File, e.Line+1, e.Character+1, e.Message))
	}

	if ran >= StageParsing {
		file, parseErrs := parser.Parse(rel, toks)
		for _, e := range parseErrs {
			res.Diagnostics = append(res.Diagnostics, fmt.Sprintf("parse %s:%d:%d: %s", e.File, e.Line+1, e.Character+1, e.Message))
		}
		if ran >= StageElaboration {
			res.Diagnostics = append(res.Diagnostics, elaborate(file, meta)...)
		}
	}
	res.Failed = len(res.Diagnostics) > 0
	return res
}

// elaborate runs the semantic stages over one parsed file: duplicate names,
// the statement-level rules, and elaboration of the hierarchy under the test's
// top module (or every uninstantiated one). Only errors count as a rejection;
// warnings are reported by the tool but do not fail a test.
func elaborate(file *ast.File, meta Metadata) []string {
	comp := sema.NewCompilation(sema.Options{})
	comp.AddFile(file)
	model := comp.Model()

	var all []diag.Diagnostic
	all = append(all, comp.Diagnostics()...)
	all = append(all, model.Diagnostics()...)
	all = append(all, model.CheckStatements()...)
	opts := elab.Options{MaxInstances: 100000, MaxSteps: 1_000_000}
	if meta.TopModule != "" {
		opts.Tops = []string{meta.TopModule}
	}
	_, ds := elab.Elaborate(context.Background(), comp, opts)
	all = append(all, ds...)
	diag.Sort(all)

	var out []string
	for _, d := range all {
		if d.Severity == diag.Error {
			out = append(out, "elaborate "+d.String())
		}
	}
	return out
}

// includeResolver implements preprocessor.IncludeResolver over a directory
// tree rooted at root.
type includeResolver struct {
	root    string
	incDirs []string
}

func (r includeResolver) Resolve(includedPath, fromFile string) (text, resolvedPath string, err error) {
	candidates := []string{filepath.Join(filepath.Dir(fromFile), includedPath)}
	for _, d := range r.incDirs {
		candidates = append(candidates, filepath.Join(d, includedPath))
	}
	var firstErr error
	for _, c := range candidates {
		data, err := os.ReadFile(filepath.Join(r.root, c))
		if err == nil {
			return string(data), c, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return "", "", firstErr
}
