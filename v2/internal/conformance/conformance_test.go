package conformance

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	corpusDir    = "../../testdata/sv-tests"
	baselinePath = "../../testdata/conformance-baseline.txt"
)

// TestCorpusMatchesBaseline runs every vendored sv-tests file and checks the
// outcome against the file's own metadata: a file expected to be rejected
// (:should_fail_because:) must produce a diagnostic, any other must not.
// Files where that does not hold today are listed in the baseline with a
// reason, and the list must be exact in both directions: a new mismatch
// fails, and so does an entry that no longer mismatches (a fix landed, so
// the entry must be removed deliberately rather than linger).
//
// A failing file's message ends with a ready-to-paste baseline line; only
// paste one with a real reason.
func TestCorpusMatchesBaseline(t *testing.T) {
	f, err := os.Open(baselinePath)
	if err != nil {
		t.Fatalf("opening baseline: %v", err)
	}
	defer f.Close()
	baseline, err := ParseBaseline(f)
	if err != nil {
		t.Fatalf("%s: %v", baselinePath, err)
	}

	seen := make(map[string]bool)
	err = filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
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
		rel = filepath.ToSlash(rel)
		seen[rel] = true

		t.Run(rel, func(t *testing.T) {
			meta := ParseMetadata(string(data))
			if len(meta.UnknownTypes) > 0 {
				t.Fatalf("unknown :type: words %v: teach conformance.stageNames about them", meta.UnknownTypes)
			}
			res := Run(corpusDir, rel, string(data), meta)
			observed := outcomeOf(res.Failed)
			expected := outcomeOf(meta.ShouldFail)
			if res.Deferred {
				observed = expected // see Result.Deferred
			}
			entry, listed := baseline[rel]

			switch {
			case observed == expected && listed:
				t.Errorf("baseline entry is stale: %s now %s as expected; remove it (reason was: %s)", rel, observed, entry.Reason)
			case observed != expected && !listed:
				t.Errorf("expected %s, observed %s (ran through %s)\n%s\nbaseline line: %s %s # %s",
					expected, observed, res.Ran, strings.Join(res.Diagnostics, "\n"), rel, observed, suggestReason(meta, res))
			case observed != expected && entry.Observed != observed:
				t.Errorf("baseline says %s, observed %s\n%s", entry.Observed, observed, strings.Join(res.Diagnostics, "\n"))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusDir, err)
	}
	for path := range baseline {
		if !seen[path] {
			t.Errorf("baseline entry %q matches no vendored file", path)
		}
	}
}

// suggestReason drafts a one-line reason for a new baseline entry. It is
// only a starting point for whoever adds the entry.
func suggestReason(meta Metadata, res Result) string {
	if meta.ShouldFail {
		return "should_fail_because: " + meta.ShouldFailBecause + " (not detected through " + res.Ran.String() + ")"
	}
	first := "unexpected diagnostic"
	if len(res.Diagnostics) > 0 {
		first = res.Diagnostics[0]
	}
	return strings.ReplaceAll(first, "\n", " ")
}
