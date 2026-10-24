package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jfetkotto/svparse/v2/diag"
)

// writeDiagnostics prints diagnostics in the compiler form
// "file:line:col: severity[code]: message", related places indented beneath,
// and a summary line; or as one JSON array with one-based positions.
func writeDiagnostics(w io.Writer, ds []diag.Diagnostic, format string) {
	if format == "json" {
		type related struct {
			File    string `json:"file"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
			Message string `json:"message"`
		}
		type item struct {
			File      string    `json:"file"`
			Line      int       `json:"line"`
			Column    int       `json:"column"`
			EndLine   int       `json:"endLine"`
			EndColumn int       `json:"endColumn"`
			Severity  string    `json:"severity"`
			Code      string    `json:"code"`
			Message   string    `json:"message"`
			Related   []related `json:"related,omitempty"`
		}
		out := make([]item, 0, len(ds))
		for _, d := range ds {
			it := item{
				File: d.Range.File, Line: d.Range.Line + 1, Column: d.Range.Character + 1,
				EndLine: d.Range.EndLine + 1, EndColumn: d.Range.EndCharacter + 1,
				Severity: d.Severity.String(), Code: d.Code, Message: d.Message,
			}
			for _, r := range d.Related {
				it.Related = append(it.Related, related{r.Range.File, r.Range.Line + 1, r.Range.Character + 1, r.Message})
			}
			out = append(out, it)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	counts := map[diag.Severity]int{}
	for _, d := range ds {
		fmt.Fprintln(w, d.String())
		for _, r := range d.Related {
			fmt.Fprintf(w, "  %s:%d:%d: note: %s\n", r.Range.File, r.Range.Line+1, r.Range.Character+1, r.Message)
		}
		counts[d.Severity]++
	}
	if len(ds) > 0 {
		fmt.Fprintf(w, "%d error(s), %d warning(s)\n", counts[diag.Error], counts[diag.Warning])
	}
}
