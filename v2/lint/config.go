package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/jfetkotto/svparse/v2/diag"
)

// Config selects and tunes the rules. It is read from JSON:
//
//	{
//	  "rules":  {"unused-signal": "off", "implicit-net": "error"},
//	  "ignore": ["third_party/**", "**/generated/*.sv"]
//	}
//
// A rule maps to "error", "warning", "info", "hint" (its severity) or "off".
// A rule not listed keeps its default severity. ignore lists globs of files
// whose findings are dropped: "*" matches within a path segment, "**" across
// segments, "?" one character.
type Config struct {
	Rules  map[string]string `json:"rules"`
	Ignore []string          `json:"ignore"`
}

// LoadConfig reads a Config and checks it: an unknown rule name or level is an
// error, so a typo does not silently disable nothing.
func LoadConfig(r io.Reader) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return Config{}, fmt.Errorf("lint config: %w", err)
	}
	return cfg, cfg.Validate()
}

// Validate reports unknown rule names, unknown levels and malformed globs.
func (c Config) Validate() error {
	var problems []string
	names := make([]string, 0, len(c.Rules))
	for name := range c.Rules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := Lookup(name); !ok {
			problems = append(problems, fmt.Sprintf("unknown rule %q", name))
			continue
		}
		if _, _, ok := parseLevel(c.Rules[name]); !ok {
			problems = append(problems, fmt.Sprintf("rule %q: level %q is not error, warning, info, hint or off", name, c.Rules[name]))
		}
	}
	for _, g := range c.Ignore {
		if _, err := globRegexp(g); err != nil {
			problems = append(problems, fmt.Sprintf("ignore pattern %q: %v", g, err))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("lint config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func parseLevel(s string) (sev diag.Severity, off, ok bool) {
	if s == "off" {
		return 0, true, true
	}
	sev, ok = diag.ParseSeverity(s)
	return sev, false, ok
}

// level is the severity a rule runs at, or off.
func (c Config) level(r Rule) (diag.Severity, bool) {
	if s, listed := c.Rules[r.Name]; listed {
		if sev, off, ok := parseLevel(s); ok {
			return sev, off
		}
	}
	return r.Severity, false
}

// ignoreMatcher returns a function reporting whether findings in a file are
// dropped.
func (c Config) ignoreMatcher() func(string) bool {
	var res []*regexp.Regexp
	for _, g := range c.Ignore {
		if re, err := globRegexp(g); err == nil {
			res = append(res, re)
		}
	}
	return func(file string) bool {
		file = strings.ReplaceAll(file, `\`, "/")
		for _, re := range res {
			if re.MatchString(file) {
				return true
			}
		}
		return false
	}
}

// globRegexp turns a glob into a regexp matching the end of a path, so
// "third_party/**" matches "/abs/third_party/x.sv" as well as a relative path.
func globRegexp(glob string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString(`(?:^|/)`)
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				sb.WriteString(`.*`)
				i++
			} else {
				sb.WriteString(`[^/]*`)
			}
		case '?':
			sb.WriteString(`[^/]`)
		default:
			sb.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	sb.WriteString(`$`)
	return regexp.Compile(sb.String())
}
