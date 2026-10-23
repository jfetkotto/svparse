package conformance

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Outcome is what the pipeline observed for a file.
type Outcome string

const (
	// OutcomePass means no stage reported a diagnostic.
	OutcomePass Outcome = "pass"
	// OutcomeFail means at least one stage reported a diagnostic.
	OutcomeFail Outcome = "fail"
)

func outcomeOf(failed bool) Outcome {
	if failed {
		return OutcomeFail
	}
	return OutcomePass
}

// BaselineEntry records a file whose observed outcome is known to differ
// from what its sv-tests metadata expects.
type BaselineEntry struct {
	// Observed is what the pipeline does today. The expectation is the
	// opposite by definition: an entry whose observed outcome matches the
	// expectation is stale.
	Observed Outcome
	Reason   string
}

// ParseBaseline reads "path pass|fail # reason" lines (blank lines and lines
// starting with '#' are comments). Every entry must carry a reason: an
// unexplained mismatch is just a hidden bug.
func ParseBaseline(r io.Reader) (map[string]BaselineEntry, error) {
	entries := make(map[string]BaselineEntry)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		head, reason, ok := strings.Cut(line, "#")
		reason = strings.TrimSpace(reason)
		fields := strings.Fields(head)
		if !ok || reason == "" || len(fields) != 2 {
			return nil, fmt.Errorf("line %d: want \"path pass|fail # reason\", got %q", n, line)
		}
		outcome := Outcome(fields[1])
		if outcome != OutcomePass && outcome != OutcomeFail {
			return nil, fmt.Errorf("line %d: outcome %q is neither pass nor fail", n, fields[1])
		}
		if _, dup := entries[fields[0]]; dup {
			return nil, fmt.Errorf("line %d: duplicate entry for %s", n, fields[0])
		}
		entries[fields[0]] = BaselineEntry{Observed: outcome, Reason: reason}
	}
	return entries, sc.Err()
}
