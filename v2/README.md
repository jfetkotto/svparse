# svparse

An error-tolerant SystemVerilog/Verilog parser library for Go, built for
language-server-style tooling: it never panics or stops on malformed or
mid-edit source.

## Capabilities

svparse is a SystemVerilog front end. From source text it produces:

- a **syntax tree** of the whole language (`ast`, `parser`): declarations,
  expressions, statements, generate constructs, primitives, assertions,
  constraints, covergroups, clocking blocks and configurations. Declaration
  boundaries and error recovery are unchanged from earlier versions, so a
  malformed construct is reported and the next one still parses;
- a **semantic model** (`sema`): types and their LRM 6.22 relations, scopes and
  name lookup (imports, packages, classes, hierarchy), typed expressions with
  width and sign propagation, constant evaluation (parameters, system
  functions, constant functions), and statement-level checks;
- an **elaborated design** (`elab`): the instance hierarchy with resolved
  parameters, generate unrolling, port bindings, interfaces, `bind` and
  hierarchical names;
- **lint diagnostics** (`lint`): rules with per-rule severity, ignore globs and
  `// svparse: disable=rule` comments, all reported as `diag.Diagnostic`.

Expression and statement errors inside constructs the earlier declaration-grade
parser skipped are not reported yet (they are kept internally); this is tracked
as `feature/expr-diagnostics` in [docs/roadmap.md](docs/roadmap.md).

| Package        | Role                                                             |
|----------------|-------------------------------------------------------------------|
| `token`        | Token vocabulary (`Token{Kind, Text, Line, Character}`)          |
| `lexer`        | Scans source into a token stream                                 |
| `preprocessor` | Macro expansion, conditional compilation, `` `include`` resolution, with source maps back to real positions |
| `parser`, `ast`| The syntax tree, with `Walk`, `Dump` and expression/statement printers |
| `svint`        | Arbitrary-width four-state integers with the LRM 11 operators    |
| `sema`         | Types, scopes, binding, constant evaluation, statement checks    |
| `elab`         | Instance hierarchy, parameters, generate, ports                  |
| `lint`         | Rule framework, configuration, suppression                       |
| `diag`         | The diagnostic type every stage reports                          |

## Command line

```sh
go install github.com/jfetkotto/svparse/cmd/svparse@latest

svparse check    [flags] files...   syntax, semantic checks and elaboration
svparse lint     [flags] files...   check, plus lint rules
svparse elab     [flags] files...   print the elaborated instance hierarchy
svparse dump-ast [flags] files...   print the syntax tree
```

Flags: `-I dir` (include directory), `-D name[=value]`, `--top module`,
`-f listfile`, `--config file` (default `.svparse.json`), `--format text|json`,
`--werror`, `--mfcu`, `--default-nettype wire|none`, and `--list-rules` for
`lint`. The exit status is 0 without errors, 1 with errors, 2 for a usage or
file problem. A lint configuration looks like:

```json
{"rules": {"implicit-net": "error", "unused-signal": "off"},
 "ignore": ["third_party/**"]}
```

## Install

```sh
go get github.com/jfetkotto/svparse
```

## Usage

```go
import (
    "github.com/jfetkotto/svparse/parser"
    "github.com/jfetkotto/svparse/preprocessor"
)

toks, ppErrs := preprocessor.Preprocess(path, text, resolver)
file, parseErrs := parser.Parse(path, toks)
```

- `lexer.Lex(src string) ([]token.Token, []Error)`: tokenize only.
- `preprocessor.Preprocess(path, text string, resolver IncludeResolver) ([]Token, []Error)`:
  tokenize, expand macros, and resolve `` `include``s. `PreprocessWithOptions`
  takes an `Options` struct for finer control.
- `parser.Parse(path string, toks []preprocessor.Token) (*ast.File, []Error)`:
  parse preprocessed tokens into an `ast.File`.
- `parser.ParseTokens(path string, toks []token.Token) (*ast.File, []Error)`:
  parse raw lexer tokens directly, skipping preprocessing.

- `parser.ParseExpr(toks)` and `parser.ParseStmt(toks)`: parse tokens as one
  expression or one statement (a block, an assignment, a loop, ...). Each
  returns a tree that is never nil: what it cannot read becomes a `BadExpr` or
  `BadStmt` plus an error, and parsing carries on. `ast.ExprString` and
  `ast.StmtString` print a tree back as source. Function and task bodies and
  `initial`/`always` blocks are not parsed into the file's tree yet.
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

## Testing

```sh
go test ./...
```

This runs unit and integration tests for every package, fuzz tests
asserting the lexer/preprocessor/parser never panic on arbitrary input,
and a corpus test (`parser/corpus_test.go`) that runs the parser over a
vendored real-world SystemVerilog corpus under `testdata/sv-tests/`
against a checked-in baseline.

## License

BSD 3-Clause. See [LICENSE](LICENSE).
