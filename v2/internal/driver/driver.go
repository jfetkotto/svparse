// Package driver runs the whole front end on files from disk: preprocess,
// parse, semantic checks, elaboration and (optionally) lint. It is what the
// svparse command does, kept apart from the command's flags and output so it
// can be tested and reused.
package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/lint"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

// Options say what to analyse and how.
type Options struct {
	// Files are the source files, in compilation order.
	Files []string
	// IncludeDirs are searched, after the including file's own directory, for
	// `include files.
	IncludeDirs []string
	// Defines are the -D macros.
	Defines map[string]string
	// Tops names the modules to elaborate; empty means every uninstantiated one.
	Tops []string
	// DefaultNettype is elab.Options.DefaultNettype.
	DefaultNettype string
	// SingleUnit shares one $unit scope between all files (-mfcu).
	SingleUnit bool
	// Elaborate runs elaboration (needed by lint and by the elab command).
	Elaborate bool
	// Lint runs the lint rules with LintConfig; it implies Elaborate.
	Lint       bool
	LintConfig lint.Config
}

// Result is everything one analysis produced.
type Result struct {
	// Files are the parsed files, in Options.Files order.
	Files []*ast.File
	// Comp is the compilation of the files.
	Comp *sema.Compilation
	// Design is the elaboration, or nil if Elaborate was not set.
	Design *elab.Design
	// Comments are all comments read, for suppression directives.
	Comments []preprocessor.Comment
	// Diagnostics are every finding of every stage, sorted and de-duplicated.
	Diagnostics []diag.Diagnostic
}

// HasErrors reports whether any diagnostic is an error.
func (r *Result) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == diag.Error {
			return true
		}
	}
	return false
}

// Analyze reads and analyses the files. It returns an error only if a file
// cannot be read; problems in the source are diagnostics in the Result.
func Analyze(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{}
	var set diag.Set
	res.Comp = sema.NewCompilation(sema.Options{SingleUnit: opts.SingleUnit})

	// Each file is read, preprocessed and parsed on its own (macros do not
	// carry from one file to the next), so the files are handled by a pool of
	// workers. The results are taken in the order the files were given, which
	// is the order the compilation sees them in.
	parsed := parseFiles(opts)
	for i, p := range parsed {
		if p.err != nil {
			return nil, p.err
		}
		set.AddAll(p.diags)
		res.Comments = append(res.Comments, p.comments...)
		res.Files = append(res.Files, p.file)
		res.Comp.AddFile(p.file)
		parsed[i] = parsedFile{} // let the rest of the file's data go
	}

	set.AddAll(res.Comp.Diagnostics())
	model := res.Comp.Model()
	set.AddAll(model.Diagnostics())
	set.AddAll(model.CheckStatements())
	set.AddAll(model.CheckNames())

	if opts.Elaborate || opts.Lint {
		design, ds := elab.Elaborate(ctx, res.Comp, elab.Options{Tops: opts.Tops, DefaultNettype: opts.DefaultNettype})
		res.Design = design
		set.AddAll(ds)
	}
	if opts.Lint {
		set.AddAll(lint.Run(ctx, lint.Input{Comp: res.Comp, Design: res.Design, Comments: res.Comments}, opts.LintConfig))
	}
	res.Diagnostics = set.Sorted()
	return res, nil
}

// includeResolver finds `include files on disk: next to the including file,
// then in each include directory.
type includeResolver struct{ dirs []string }

func (r includeResolver) Resolve(includedPath, fromFile string) (text, resolvedPath string, err error) {
	candidates := []string{filepath.Join(filepath.Dir(fromFile), includedPath)}
	if filepath.IsAbs(includedPath) {
		candidates = []string{includedPath}
	}
	for _, d := range r.dirs {
		candidates = append(candidates, filepath.Join(d, includedPath))
	}
	var first error
	for _, c := range candidates {
		data, err := os.ReadFile(c)
		if err == nil {
			return string(data), filepath.Clean(c), nil
		}
		if first == nil {
			first = err
		}
	}
	if first == nil {
		first = fmt.Errorf("no candidates for %q", includedPath)
	}
	return "", "", first
}

// parsedFile is what handling one source file produces.
type parsedFile struct {
	file     *ast.File
	diags    []diag.Diagnostic
	comments []preprocessor.Comment
	err      error
}

// parseFiles reads, preprocesses and parses opts.Files in parallel, returning
// one result per file in the same order.
func parseFiles(opts Options) []parsedFile {
	out := make([]parsedFile, len(opts.Files))
	var wg sync.WaitGroup
	next := make(chan int)
	workers := min(runtime.GOMAXPROCS(0), len(opts.Files))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				out[i] = parseFile(opts, opts.Files[i])
			}
		}()
	}
	for i := range opts.Files {
		next <- i
	}
	close(next)
	wg.Wait()
	return out
}

func parseFile(opts Options, path string) parsedFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return parsedFile{err: err}
	}
	pre := preprocessor.PreprocessDetailed(path, string(data),
		includeResolver{dirs: opts.IncludeDirs},
		preprocessor.Options{InitialMacros: opts.Defines, KeepComments: true})
	file, perrs := parser.Parse(path, pre.Tokens)
	var set diag.Set
	set.AddAll(diag.FromPreprocessor(pre.Errors))
	set.AddAll(diag.FromParser(perrs))
	return parsedFile{file: file, diags: set.Sorted(), comments: pre.Comments}
}
