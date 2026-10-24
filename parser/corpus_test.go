package parser

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

// corpusDir is the vendored sv-tests subset -- see
// testdata/sv-tests/NOTICE.md for exactly what's included and why.
const corpusDir = "../testdata/sv-tests"

// corpusIncludeResolver resolves an “ `include “d path relative to the
// including file's own directory, rooted at corpusDir -- a handful of
// vendored files reference a companion file vendored right alongside them
// specifically to exercise this (see testdata/sv-tests/NOTICE.md), unlike
// FuzzParseNeverPanics's mutated input, which has no such structure to
// resolve against. An absolute-path “ `include “ (e.g. "/dev/null",
// platform-specific and outside corpusDir entirely) is left unresolved --
// see the baseline for that one file.
type corpusIncludeResolver struct{}

func (corpusIncludeResolver) Resolve(includedPath, fromFile string) (text, resolvedPath string, err error) {
	resolvedPath = filepath.Join(filepath.Dir(fromFile), includedPath)
	data, err := os.ReadFile(filepath.Join(corpusDir, resolvedPath))
	if err != nil {
		return "", "", err
	}
	return string(data), resolvedPath, nil
}

// walkCorpus calls fn, as its own subtest, once per vendored .sv file --
// relPath (relative to corpusDir, for readable subtest names/failure
// messages) and the file's contents.
func walkCorpus(t *testing.T, fn func(t *testing.T, relPath, text string)) {
	t.Helper()
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".sv" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(corpusDir, path)
		if err != nil {
			return err
		}
		t.Run(rel, func(t *testing.T) {
			fn(t, rel, string(data))
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusDir, err)
	}
}

// TestSVTestsCorpusNeverPanics runs every vendored sv-tests file through the
// full preprocess+parse pipeline and fails on any panic -- unconditional,
// regardless of how many files produce (expected or unexpected) parse
// errors; see TestSVTestsCorpusMatchesBaseline for that side of it.
// Complements the existing FuzzParseNeverPanics (integration_test.go),
// which explores mutated input rather than realistic, LRM-shaped source --
// this is the "does it handle the real thing" counterpart.
func TestSVTestsCorpusNeverPanics(t *testing.T) {
	walkCorpus(t, func(t *testing.T, relPath, text string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked: %v", r)
			}
		}()
		toks, _ := preprocessor.Preprocess(relPath, text, corpusIncludeResolver{})
		Parse(relPath, toks)
	})
}

// baselinePath is the committed, hand-triaged record of every vendored
// file expected to produce a nonzero error count, and why -- see the file
// itself for the full explanation and category breakdown.
const baselinePath = "../testdata/sv-tests-baseline.txt"

// loadBaseline parses baselinePath's "path count # reason" lines into
// path -> expected count, ignoring blank lines and lines starting with
// "#" (section-header commentary, not data).
func loadBaseline(t *testing.T) map[string]int {
	t.Helper()
	f, err := os.Open(baselinePath)
	if err != nil {
		t.Fatalf("opening %s: %v", baselinePath, err)
	}
	defer f.Close()

	baseline := make(map[string]int)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) < 2 {
			t.Fatalf("malformed baseline line %q: want \"path count [# reason]\"", line)
		}
		count, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("malformed baseline line %q: %v", line, err)
		}
		baseline[fields[0]] = count
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", baselinePath, err)
	}
	return baseline
}

// TestSVTestsCorpusMatchesBaseline is TestSVTestsCorpusNeverPanics's
// counterpart on the error-count side: every vendored file's actual
// preprocessor+parser error count must match the committed baseline
// exactly -- 0 (the implicit default for anything not listed) for
// everything svparse is expected to handle cleanly, or the specific
// count recorded (with its reason) for the deliberately out-of-scope or
// intentionally-invalid remainder. A mismatch in either direction --
// a new gap appearing, or an existing one silently going away without
// its baseline entry being deliberately removed -- fails the test,
// forcing every such change through a reviewed baseline update instead
// of silent drift.
func TestSVTestsCorpusMatchesBaseline(t *testing.T) {
	baseline := loadBaseline(t)
	seen := make(map[string]bool)
	walkCorpus(t, func(t *testing.T, relPath, text string) {
		seen[relPath] = true
		toks, ppErrs := preprocessor.Preprocess(relPath, text, corpusIncludeResolver{})
		_, parseErrs := Parse(relPath, toks)
		got := len(ppErrs) + len(parseErrs)
		want := baseline[relPath]
		if got != want {
			t.Errorf("error count mismatch: got %d, baseline says %d (pp: %+v, parse: %+v)", got, want, ppErrs, parseErrs)
		}
	})
	for path := range baseline {
		if !seen[path] {
			t.Errorf("baseline entry %q doesn't match any vendored file", path)
		}
	}
}

// TestSVTestsCorpusSpansAreWellFormed pins the invariant that every
// declaration's End is at or after its start.
//
// It is corpus-wide rather than example-based on purpose: the bug it
// guards against was not one wrong node but a whole convention that was
// only ever applied to the seven declarations that span a real body, so
// every other kind carried End{0,0} against a nonzero start -- a
// backwards range. A per-node test would have had to be written for each
// kind that was already missing one. Running it over all 387 vendored
// files means a newly added declaration kind is covered the day it can
// appear in real source.
func TestSVTestsCorpusSpansAreWellFormed(t *testing.T) {
	walkCorpus(t, func(t *testing.T, relPath, text string) {
		toks, _ := preprocessor.Preprocess(relPath, text, corpusIncludeResolver{})
		f, _ := Parse(relPath, toks)
		walkDeclSpans(t, f.Decls)
	})
}

func walkDeclSpans(t *testing.T, decls []ast.Decl) {
	t.Helper()
	for _, d := range decls {
		pos := d.Pos()
		if pos.EndLine < pos.Line || (pos.EndLine == pos.Line && pos.EndCharacter < pos.Character) {
			t.Errorf("%T %s: end %d:%d precedes start %d:%d",
				d, declName(d), pos.EndLine, pos.EndCharacter, pos.Line, pos.Character)
		}
		switch n := d.(type) {
		case *ast.Container:
			walkDeclSpans(t, n.Body)
		case *ast.Class:
			walkDeclSpans(t, n.Body)
		case *ast.Package:
			walkDeclSpans(t, n.Body)
		}
	}
}

func declName(d ast.Decl) string {
	switch n := d.(type) {
	case *ast.Container:
		return n.Name
	case *ast.Class:
		return n.Name
	case *ast.Package:
		return n.Name
	case *ast.Function:
		return n.Name
	case *ast.Task:
		return n.Name
	case *ast.Variable:
		return n.Name
	case *ast.Typedef:
		return n.Name
	case *ast.Parameter:
		return n.Name
	case *ast.Instantiation:
		return n.ModuleType
	}
	return "?"
}
