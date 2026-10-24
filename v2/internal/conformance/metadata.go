// Package conformance runs the vendored sv-tests corpus (testdata/sv-tests)
// through svparse's front end and checks each file's outcome against what
// its own metadata says should happen.
//
// sv-tests tags every test with a header block of ":key: value" lines (see
// its tools/runner): the pipeline stages the test exercises (:type:),
// whether the tool is expected to reject it (:should_fail_because:), the
// top module, and -D defines. This package reads that metadata, runs
// whichever of those stages svparse implements so far, and compares. Files
// whose observed outcome differs from the expectation are recorded, with a
// reason, in testdata/conformance-baseline.txt; as later stages (expression
// and statement parsing, semantic checks, elaboration) land, MaxImplemented
// moves up and the baseline shrinks.
package conformance

import (
	"regexp"
	"strings"
)

// Stage is a pipeline stage an sv-tests file can exercise, in pipeline
// order: a later stage implies running every earlier one.
type Stage int

const (
	StagePreprocessing Stage = iota + 1
	StageParsing
	StageElaboration
	StageSimulation
)

var stageNames = map[string]Stage{
	"preprocessing": StagePreprocessing,
	"parsing":       StageParsing,
	"elaboration":   StageElaboration,
	"simulation":    StageSimulation,
}

func (s Stage) String() string {
	for name, st := range stageNames {
		if st == s {
			return name
		}
	}
	return "unknown"
}

// Metadata is one sv-tests file's parsed header.
type Metadata struct {
	Name        string
	Description string
	Tags        []string

	// Files and IncDirs are the :files: / :incdirs: entries; empty means
	// the upstream default (the test file itself, and its own directory).
	Files   []string
	IncDirs []string

	TopModule string

	// Types is the set of stages the test exercises. Upstream defaults an
	// unset :type: to "parsing elaboration".
	Types []Stage

	// UnknownTypes lists :type: words that are not a known stage, so a
	// harness test can fail loudly if upstream grows a new one.
	UnknownTypes []string

	// ShouldFail is true when the tool is expected to reject the file:
	// :should_fail_because: present, or :should_fail: 1.
	ShouldFail        bool
	ShouldFailBecause string

	// Defines are the :defines: entries ("NAME" or "NAME=value"), in the
	// shape preprocessor.Options.InitialMacros takes.
	Defines map[string]string
}

// metaLine is upstream's own header-line pattern (tools/runner): a line that
// starts with ":key:" followed by a non-empty value. Matching at the start of
// a line is what lets the header live inside a /* ... */ comment.
var metaLine = regexp.MustCompile(`^:([a-zA-Z_-]+):\s*(.+)`)

// ParseMetadata reads src's sv-tests header. It never fails: an unknown key
// is ignored and a missing one takes upstream's default, exactly as the
// reference runner does.
func ParseMetadata(src string) Metadata {
	var (
		m          Metadata
		typeValue  = "parsing elaboration"
		shouldFail string
	)
	for _, line := range strings.Split(src, "\n") {
		g := metaLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if g == nil {
			continue
		}
		value := strings.TrimSpace(g[2])
		switch strings.ToLower(g[1]) {
		case "name":
			m.Name = value
		case "description":
			m.Description = value
		case "tags":
			m.Tags = strings.Fields(value)
		case "files":
			m.Files = strings.Fields(value)
		case "incdirs":
			m.IncDirs = strings.Fields(value)
		case "top_module":
			m.TopModule = value
		case "type":
			typeValue = value
		case "should_fail":
			shouldFail = value
		case "should_fail_because":
			m.ShouldFailBecause = value
		case "defines":
			m.Defines = parseDefines(value)
		}
	}
	for _, word := range strings.Fields(typeValue) {
		if st, ok := stageNames[strings.ToLower(word)]; ok {
			m.Types = append(m.Types, st)
		} else {
			m.UnknownTypes = append(m.UnknownTypes, word)
		}
	}
	// Upstream: a :should_fail_because: forces should_fail on, even over an
	// explicit ":should_fail: 0".
	m.ShouldFail = m.ShouldFailBecause != "" || shouldFail == "1"
	return m
}

func parseDefines(value string) map[string]string {
	defs := make(map[string]string)
	for _, f := range strings.Fields(value) {
		name, val, _ := strings.Cut(f, "=")
		defs[name] = val
	}
	return defs
}

// HighestStage is the latest stage the test exercises, or 0 if it names none.
func (m Metadata) HighestStage() Stage {
	var hi Stage
	for _, s := range m.Types {
		hi = max(hi, s)
	}
	return hi
}
