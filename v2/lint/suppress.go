package lint

import (
	"regexp"
	"strings"

	"github.com/jfetkotto/svparse/v2/preprocessor"
)

// A suppression comment names the rules it silences:
//
//	// svparse: disable=rule[,rule]            this line (a trailing comment), or
//	                                           the next (a comment on its own line)
//	// svparse: disable-line=rule              this line
//	// svparse: disable-next-line=rule         the next line
//	// svparse: disable-file=rule              the whole file
//
// "all" stands for every rule.
var suppressRE = regexp.MustCompile(`svparse:\s*(disable(?:-next-line|-line|-file)?)\s*=\s*([A-Za-z0-9_,\s-]+)`)

// Suppressions is the set of places rules are silenced.
type Suppressions struct {
	files map[string]*fileSuppressions
}

type fileSuppressions struct {
	whole map[string]bool
	lines map[int]map[string]bool
}

// ParseSuppressions reads the directives out of comments.
func ParseSuppressions(comments []preprocessor.Comment) *Suppressions {
	s := &Suppressions{files: map[string]*fileSuppressions{}}
	for _, c := range comments {
		m := suppressRE.FindStringSubmatch(c.Text)
		if m == nil {
			continue
		}
		fs := s.files[c.File]
		if fs == nil {
			fs = &fileSuppressions{whole: map[string]bool{}, lines: map[int]map[string]bool{}}
			s.files[c.File] = fs
		}
		var names []string
		for _, n := range strings.FieldsFunc(m[2], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
			names = append(names, n)
		}
		add := func(line int) {
			if fs.lines[line] == nil {
				fs.lines[line] = map[string]bool{}
			}
			for _, n := range names {
				fs.lines[line][n] = true
			}
		}
		switch m[1] {
		case "disable-file":
			for _, n := range names {
				fs.whole[n] = true
			}
		case "disable-next-line":
			add(c.Line + 1)
		case "disable-line":
			add(c.Line)
		default: // "disable"
			if c.OwnLine {
				add(c.Line + 1)
			} else {
				add(c.Line)
			}
		}
	}
	return s
}

// Suppressed reports whether the rule is silenced at a zero-based line of file.
func (s *Suppressions) Suppressed(rule, file string, line int) bool {
	if s == nil {
		return false
	}
	fs := s.files[file]
	if fs == nil {
		return false
	}
	if fs.whole[rule] || fs.whole["all"] {
		return true
	}
	l := fs.lines[line]
	return l[rule] || l["all"]
}
