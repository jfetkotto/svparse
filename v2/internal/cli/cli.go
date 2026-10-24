// Package cli implements the svparse command: argument handling and output,
// over internal/driver.
package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/elab"
	"github.com/jfetkotto/svparse/v2/internal/driver"
	"github.com/jfetkotto/svparse/v2/lint"
)

const usage = `usage: svparse <command> [flags] files...

commands:
  check      syntax, semantic checks and elaboration
  lint       everything check does, plus lint rules
  elab       print the elaborated instance hierarchy
  dump-ast   print the syntax tree

flags:
  -I dir              add an include directory (repeatable)
  -D name[=value]     define a macro (repeatable)
  --top name          elaborate this module as a root (repeatable)
  -f listfile         read a file list: sources, -f lists, +incdir+, +define+;
                      paths are relative to the list
  --config file       lint configuration (default: .svparse.json if present)
  --format text|json  output format (default text)
  --werror            warnings fail the run
  --mfcu              share one $unit scope across all files
  --default-nettype wire|none
  --list-rules        (lint) print the available rules and exit
`

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error { *l = append(*l, s); return nil }

type settings struct {
	includes, defines, tops, listFiles listFlag
	config, format, nettype            string
	werror, mfcu, listRules            bool
	files                              []string
}

// Run executes the command line (without the program name) and returns the
// exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stderr, usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	cmd := args[0]
	switch cmd {
	case "check", "lint", "elab", "dump-ast":
	default:
		fmt.Fprintf(stderr, "svparse: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	s, err := parseFlags(cmd, args[1:], stderr)
	if err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintf(stderr, "svparse: %v\n", err)
		}
		return 2
	}
	if s.format != "text" && s.format != "json" {
		fmt.Fprintf(stderr, "svparse: --format must be text or json, not %q\n", s.format)
		return 2
	}
	if s.nettype != "wire" && s.nettype != "none" {
		fmt.Fprintf(stderr, "svparse: --default-nettype must be wire or none, not %q\n", s.nettype)
		return 2
	}
	if cmd == "lint" && s.listRules {
		return listRules(stdout)
	}
	for _, lf := range s.listFiles {
		more, err := readList(lf)
		if err != nil {
			fmt.Fprintf(stderr, "svparse: %v\n", err)
			return 2
		}
		s.files = append(s.files, more.files...)
		s.includes = append(s.includes, more.incdirs...)
		s.defines = append(s.defines, more.defines...)
	}
	if len(s.files) == 0 {
		fmt.Fprintf(stderr, "svparse %s: no input files\n", cmd)
		return 2
	}

	opts := driver.Options{
		Files:          s.files,
		IncludeDirs:    s.includes,
		Defines:        defineMap(s.defines),
		Tops:           s.tops,
		DefaultNettype: s.nettype,
		SingleUnit:     s.mfcu,
		Elaborate:      cmd == "check" || cmd == "lint" || cmd == "elab",
		Lint:           cmd == "lint",
	}
	if opts.Lint {
		cfg, err := loadConfig(s.config)
		if err != nil {
			fmt.Fprintf(stderr, "svparse: %v\n", err)
			return 2
		}
		opts.LintConfig = cfg
	}
	res, err := driver.Analyze(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(stderr, "svparse: %v\n", err)
		return 2
	}

	switch cmd {
	case "dump-ast":
		for _, f := range res.Files {
			fmt.Fprint(stdout, ast.Dump(f))
		}
		// A syntax tree is only worth printing if it came from valid input, so
		// only syntax problems fail this command.
		syntax := filterCodes(res.Diagnostics, diag.CodeSyntaxError, diag.CodePreprocessorError)
		writeDiagnostics(stderr, syntax, s.format)
		return exitStatus(syntax, s.werror)
	case "elab":
		if res.Design != nil {
			printHierarchy(stdout, res.Design)
		}
	}
	writeDiagnostics(stdout, res.Diagnostics, s.format)
	return exitStatus(res.Diagnostics, s.werror)
}

// parseFlags reads flags and files in any order.
func parseFlags(cmd string, args []string, stderr io.Writer) (*settings, error) {
	s := &settings{}
	fs := flag.NewFlagSet("svparse "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Var(&s.includes, "I", "include directory")
	fs.Var(&s.defines, "D", "macro definition")
	fs.Var(&s.tops, "top", "top module")
	fs.Var(&s.listFiles, "f", "file list")
	fs.StringVar(&s.config, "config", "", "lint configuration")
	fs.StringVar(&s.format, "format", "text", "output format")
	fs.StringVar(&s.nettype, "default-nettype", "wire", "default net type")
	fs.BoolVar(&s.werror, "werror", false, "warnings fail the run")
	fs.BoolVar(&s.mfcu, "mfcu", false, "single compilation unit")
	fs.BoolVar(&s.listRules, "list-rules", false, "list lint rules")

	// Everything after "--" is a file name.
	var after []string
	for i, a := range args {
		if a == "--" {
			args, after = args[:i], args[i+1:]
			break
		}
	}
	args = splitAttached(args)
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		s.files = append(s.files, fs.Arg(0))
		args = fs.Args()[1:]
	}
	s.files = append(s.files, after...)
	return s, nil
}

func defineMap(defs []string) map[string]string {
	if len(defs) == 0 {
		return nil
	}
	m := make(map[string]string, len(defs))
	for _, d := range defs {
		name, val, _ := strings.Cut(d, "=")
		m[name] = val
	}
	return m
}

// fileList is what a "-f" file contributes: source files, include directories
// and macro definitions, from the file and from the lists it names.
type fileList struct {
	files, incdirs, defines []string
}

// maxListDepth bounds nesting of "-f" lists, so a list that names itself ends
// in an error rather than a stack overflow.
const maxListDepth = 32

// readList reads a simulator-style file list, the kind a design keeps next to
// its sources:
//
//	// comment            (also # at the start of a line, and " //" after text)
//	+incdir+a/inc+b/inc   include directories
//	+define+W=8+DEBUG     macro definitions
//	-f other.f            another list, read in place (-F too)
//	-v lib.v              a library source file
//	rtl/top.sv            a source file
//
// Relative paths are relative to the directory of the list that names them,
// so a list works from wherever svparse is run. Other "+" and "-" options
// are ignored: a list written for a simulator carries flags that mean nothing
// here.
func readList(path string) (fileList, error) {
	var out fileList
	err := readListInto(path, &out, map[string]bool{}, 0)
	return out, err
}

func readListInto(path string, out *fileList, active map[string]bool, depth int) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if depth > maxListDepth || active[abs] {
		return fmt.Errorf("file list %s includes itself", path)
	}
	active[abs] = true
	defer delete(active, abs)

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dir := filepath.Dir(path)
	relTo := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, " //"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+incdir+"):
			for _, d := range strings.Split(strings.TrimPrefix(line, "+incdir+"), "+") {
				if d != "" {
					out.incdirs = append(out.incdirs, relTo(d))
				}
			}
		case strings.HasPrefix(line, "+define+"):
			for _, d := range strings.Split(strings.TrimPrefix(line, "+define+"), "+") {
				if d != "" {
					out.defines = append(out.defines, d)
				}
			}
		case strings.HasPrefix(line, "-f ") || strings.HasPrefix(line, "-F "):
			if err := readListInto(relTo(strings.TrimSpace(line[3:])), out, active, depth+1); err != nil {
				return err
			}
		case strings.HasPrefix(line, "-v "):
			out.files = append(out.files, relTo(strings.TrimSpace(line[3:])))
		case strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-"):
			// A simulator option with no meaning here.
		default:
			out.files = append(out.files, relTo(line))
		}
	}
	return sc.Err()
}

// loadConfig reads the named lint configuration, or .svparse.json in the
// current directory if none is named and the file exists.
func loadConfig(path string) (lint.Config, error) {
	if path == "" {
		if _, err := os.Stat(".svparse.json"); err != nil {
			return lint.Config{}, nil
		}
		path = ".svparse.json"
	}
	f, err := os.Open(path)
	if err != nil {
		return lint.Config{}, err
	}
	defer f.Close()
	cfg, err := lint.LoadConfig(f)
	if err != nil {
		return lint.Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func listRules(w io.Writer) int {
	for _, r := range lint.Rules() {
		fmt.Fprintf(w, "%-24s %-8s %s\n", r.Name, r.Severity, r.Doc)
	}
	return 0
}

func filterCodes(ds []diag.Diagnostic, codes ...string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		for _, c := range codes {
			if d.Code == c {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

// exitStatus is 1 if there is an error, or a warning with werror.
func exitStatus(ds []diag.Diagnostic, werror bool) int {
	for _, d := range ds {
		if d.Severity == diag.Error || (werror && d.Severity == diag.Warning) {
			return 1
		}
	}
	return 0
}

// printHierarchy prints the instance tree, with the overridable parameters of
// each instance.
func printHierarchy(w io.Writer, d *elab.Design) {
	var rec func(n *elab.Node, depth int)
	rec = func(n *elab.Node, depth int) {
		var sb strings.Builder
		sb.WriteString(strings.Repeat("  ", depth) + n.Name)
		if n.Def != nil {
			sb.WriteString(" (" + n.Def.Name + ")")
		}
		for _, p := range n.Params {
			if p.Local || !p.Valid {
				continue
			}
			val := ""
			if p.IsType {
				val = p.TypeValue.String()
			} else {
				val = p.Value.String()
			}
			mark := "="
			if p.Overridden {
				mark = "=*"
			}
			sb.WriteString(" " + p.Name + mark + val)
		}
		fmt.Fprintln(w, sb.String())
		for _, c := range n.Children {
			rec(c, depth+1)
		}
	}
	for _, t := range d.Tops {
		rec(t, 0)
	}
}

// splitAttached turns the conventional attached forms "-DNAME=1" and "-Idir"
// into "-D NAME=1" and "-I dir", which the flag package understands.
func splitAttached(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if len(a) > 2 && (strings.HasPrefix(a, "-D") || strings.HasPrefix(a, "-I")) && !strings.HasPrefix(a, "--") {
			out = append(out, a[:2], a[2:])
			continue
		}
		out = append(out, a)
	}
	return out
}
