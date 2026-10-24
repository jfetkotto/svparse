# svparse v2

An error-tolerant SystemVerilog front end for Go, built for language-server
style tooling: it never panics or stops on malformed or mid-edit source.

This is the `github.com/jfetkotto/svparse/v2` module. It is in development and
not yet tagged `v2.0.0`; the released declaration-grade parser is the v0.1 line
in the parent directory. The [top-level README](../README.md) compares the two
and explains how to move from one to the other.

## Capabilities

From source text svparse v2 produces:

- a **syntax tree** of the whole language (`ast`, `parser`): declarations,
  expressions, statements, `always`/`initial`/`assign`, generate constructs,
  gate and UDP primitives, assertions and sequences, constraints, covergroups,
  clocking blocks, `bind`, `defparam` and configurations. Declaration
  boundaries and error recovery are the same as in earlier versions, so a
  malformed construct is reported and the next one still parses. Errors inside
  function and task bodies, procedural blocks and expressions are reported too;
- a **semantic model** (`sema`): types and their LRM 6.22 relations, scopes and
  name lookup (imports, packages, classes, hierarchy), typed expressions with
  width and sign propagation, constant evaluation (parameters, system
  functions, constant functions), statement checks (assignable targets,
  break/continue/return, timing in functions, driver conflicts) and a check for
  names that are not declared;
- an **elaborated design** (`elab`): the instance hierarchy with resolved
  parameters, generate unrolling, port bindings, interfaces, `bind`,
  hierarchical names and implicit nets;
- **lint diagnostics** (`lint`): 19 rules with per-rule severity, ignore globs
  and `// svparse: disable=rule` comments, all reported as `diag.Diagnostic`.

| Package        | Role                                                             |
|----------------|-------------------------------------------------------------------|
| `token`        | Token vocabulary (`Token{Kind, Text, Line, Character}`)          |
| `lexer`        | Scans source into a token stream                                 |
| `preprocessor` | Macro expansion, conditional compilation, `` `include`` resolution, with source maps back to real positions |
| `parser`, `ast`| The syntax tree, with `Walk`, `Inspect`, `Dump` and expression/statement printers |
| `svint`        | Arbitrary-width four-state integers with the LRM 11 operators    |
| `sema`         | Types, scopes, binding, constant evaluation, statement and name checks |
| `elab`         | Instance hierarchy, parameters, generate, ports                  |
| `lint`         | Rule framework, configuration, suppression                       |
| `diag`         | The diagnostic type every stage reports                          |

## Command line

```sh
go install github.com/jfetkotto/svparse/v2/cmd/svparse@latest   # once v2.0.0 is released

svparse check    [flags] files...   syntax, semantic checks and elaboration
svparse lint     [flags] files...   check, plus lint rules
svparse elab     [flags] files...   print the elaborated instance hierarchy
svparse dump-ast [flags] files...   print the syntax tree
```

| Flag | Meaning |
|---|---|
| `-I dir` | add an include directory (repeatable) |
| `-D name[=value]` | define a macro (repeatable) |
| `--top module` | elaborate this module as a root (repeatable); by default every module nothing instantiates |
| `-f listfile` | read a file list: sources, nested `-f` lists, `+incdir+` and `+define+`; paths are relative to the list |
| `--config file` | lint configuration (default: `.svparse.json` if present) |
| `--format text\|json` | output format |
| `--werror` | warnings fail the run |
| `--mfcu` | share one `$unit` scope across all files |
| `--default-nettype wire\|none` | the default net type |
| `--list-rules` | (`lint`) print the available rules and exit |

The exit status is 0 without errors, 1 with errors, 2 for a usage or file
problem. Files are read and parsed in parallel.

```sh
svparse lint --top soc_top -f filelists/soc.f
```

### Lint

`svparse lint --list-rules` prints every rule. The default levels are:

| Rule | Level | Finds |
|---|---|---|
| `blocking-in-sequential` | warning | a blocking assignment in a clocked `always` block |
| `nonblocking-in-combinational` | warning | a nonblocking assignment in `always_comb` or `always @*` |
| `case-no-default` | warning | a `case` with no `default` and no `unique` or `priority` |
| `latch-inferred` | warning | a signal not assigned on every path of a combinational block |
| `incomplete-sensitivity` | warning | a signal read by `always @(list)` but missing from the list |
| `unreachable-code` | warning | statements after `return`, `break`, `continue` or `disable` |
| `unused-signal` | warning | a net or variable never read, or assigned but never read |
| `undriven-signal` | warning | a net or variable read but never driven |
| `unused-port` | warning | an input never read inside its module |
| `undriven-port` | warning | an output never driven inside its module |
| `unused-parameter` | info | a parameter or localparam never referenced |
| `width-truncation` | warning | an assignment that drops the upper bits of a wider value |
| `width-extension` | info | an assignment that pads a narrower value to a wider target |
| `sign-mixing` | warning | an operator mixing signed and unsigned operands |
| `implicit-net` | warning | a net declared by use, such as a misspelt name in a port connection |
| `overlapping-drivers` | warning | two processes driving overlapping bits of one variable |
| `shadowed-declaration` | warning | a declaration hiding one in an enclosing scope |
| `unconnected-input` | error | an instance leaving an input out of its connection list, or writing `.port()` for it |
| `unconnected-output` | warning | an instance leaving an output or inout out of its connection list (`.port()` leaves one open on purpose) |

A configuration maps a rule to `error`, `warning`, `info`, `hint` or `off`, and
lists ignore globs (`**` crosses directories):

```json
{"rules": {"implicit-net": "error", "unused-signal": "off"},
 "ignore": ["third_party/**"]}
```

A single finding is silenced in the source with `// svparse: disable=<rule>`
on its line, `disable-next-line=<rule>` or `disable-file=<rule>`.

## Install

```sh
go get github.com/jfetkotto/svparse/v2
```

## Usage

```go
import (
    "github.com/jfetkotto/svparse/v2/parser"
    "github.com/jfetkotto/svparse/v2/preprocessor"
)

toks, ppErrs := preprocessor.Preprocess(path, text, resolver)
file, parseErrs := parser.Parse(path, toks)
```

- `lexer.Lex(src string) ([]token.Token, []Error)`: tokenize only.
- `preprocessor.Preprocess(path, text string, resolver IncludeResolver) ([]Token, []Error)`:
  tokenize, expand macros, and resolve `` `include``s. `PreprocessWithOptions`
  takes an `Options` struct for finer control.
- `parser.Parse(path string, toks []preprocessor.Token) (*ast.File, []Error)`:
  parse preprocessed tokens into an `ast.File`. The errors include those found
  inside bodies and expressions.
- `parser.ParseTokens(path string, toks []token.Token) (*ast.File, []Error)`:
  parse raw lexer tokens directly, skipping preprocessing.
- `parser.ParseExpr(toks)` and `parser.ParseStmt(toks)`: parse tokens as one
  expression or one statement. Each returns a tree that is never nil: what it
  cannot read becomes a `BadExpr` or `BadStmt` plus an error, and parsing
  carries on. `ast.ExprString` and `ast.StmtString` print a tree back as
  source.
- `ast.Walk`, `ast.Inspect` and `ast.Dump`: traverse a tree in source order, or
  print it as a compact S-expression.
- `lexer.LexWithComments(src)`: `Lex` that also returns the comments it skipped
  (`token.Comment`: text, position, and whether it sits alone on its line).
- `preprocessor.PreprocessDetailed(path, text, resolver, opts)`: like
  `PreprocessWithOptions` but returns a `Result` that includes `Comments`
  (set `Options.KeepComments`), with file attribution through `` `include``.
  `preprocessor.NewCommentIndex(result.Comments)` answers "what doc comment is
  above line N" (`Leading`) and "what trails line N" (`Trailing`); `DocText`
  joins comment bodies. Keeping comments never changes the tokens.

None of these functions ever panic; malformed input is recorded in the
returned `[]Error` and parsing/lexing continues.

### Semantic analysis, elaboration and lint

```go
import (
    "context"

    "github.com/jfetkotto/svparse/v2/elab"
    "github.com/jfetkotto/svparse/v2/lint"
    "github.com/jfetkotto/svparse/v2/sema"
)

comp := sema.NewCompilation(sema.Options{})
comp.AddFile(file) // for each parsed file

model := comp.Model()
diags := append(comp.Diagnostics(), model.Diagnostics()...)
diags = append(diags, model.CheckStatements()...)
diags = append(diags, model.CheckNames()...)

design, elabDiags := elab.Elaborate(ctx, comp, elab.Options{Tops: []string{"top"}})
findings := lint.Run(ctx, lint.Input{Comp: comp, Design: design, Comments: comments}, lint.Config{})
```

- Every stage reports `diag.Diagnostic` values (code, severity, range, message,
  related locations) with stable kebab-case codes such as `undeclared-identifier`.
- `model.Bind` types an expression in a scope, `model.Eval` evaluates a
  constant one, and `model.NewInstance(env)` binds many expressions of one
  elaborated instance, resolving parameter-dependent types once.
- `model.CheckNamesIn(files)`, `model.CheckStatementsIn(files)` and
  `lint.Input.Files` limit the work and the findings to some files, for an
  editor that has changed one file of a large design.
- `elab.Elaborate` and `lint.Run` take a `context.Context` and stop early when
  it is cancelled.

## Status

svparse v2 is being built toward `v2.0.0`; [docs/roadmap.md](../docs/roadmap.md)
records each step and the conformance count at each. Known limits: one
`` `default_nettype`` per design, `defparam` paths with indices, type
parameters are not substituted in types, and arrays of interfaces are not
resolved. Classes, constraints, covergroups, assertions and clocking blocks are
parsed, but their bodies are not yet looked into by the name check.

## Testing

```sh
go test ./...
```

This runs unit and integration tests for every package, fuzz tests asserting
that the lexer, preprocessor, parser and the later stages never panic on
arbitrary input, and two corpus tests over the vendored sv-tests files under
`testdata/sv-tests/`: `parser/corpus_test.go` checks the parser against a
checked-in baseline, and `internal/conformance` runs each file through the
stage its metadata asks for (parsing, elaboration) and compares with the
expected result, against `testdata/conformance-baseline.txt`.

## License

BSD 3-Clause. See [LICENSE](LICENSE).
