# svparse

An error-tolerant SystemVerilog/Verilog front end for Go, built for
language-server-style tooling: it never panics or stops on malformed or
mid-edit source.

This repository holds two Go modules side by side. They share a name, a lexer
lineage and a design goal, but they do very different amounts of work, so pick
the one that matches what you need.

| | **v0.1** (this directory) | **v2** ([`v2/`](v2/)) |
|---|---|---|
| Import path | `github.com/jfetkotto/svparse` | `github.com/jfetkotto/svparse/v2` |
| Status | Released; latest `v0.1.11`. Bug fixes only. | In development on the `devel` branch; not yet tagged `v2.0.0`. |
| What it parses | **Declarations** only: ports, nets and variables, parameters, typedefs, struct/enum, function and task signatures, class members, imports, containers, instantiations. | **The whole language**: also expressions, statements, `always`/`initial`/`assign`, generate constructs, gate and UDP primitives, assertions and sequences, constraints, covergroups, clocking blocks, `bind`, `defparam`, configurations. |
| Expressions and statements | Skipped by balanced-token matching and kept as raw token spans. | Parsed into trees (`ast.Expr`, `ast.Stmt`); the raw spans stay populated but are deprecated. |
| Errors reported | Syntax errors in declarations. Errors inside skipped bodies are invisible. | Also errors inside bodies, procedural items and expressions. |
| Semantic model | None. | `sema`: types, scopes and name lookup, typed expressions with width and sign propagation, constant evaluation, statement checks, undeclared-name checks. |
| Elaboration | None. | `elab`: instance hierarchy, parameter overrides, generate unrolling, port bindings, interfaces, `bind`. |
| Lint | None. | `lint`: 19 rules with per-rule severity, ignore globs and `// svparse: disable=rule` comments. |
| Command line | None. | `svparse check`, `lint`, `elab` and `dump-ast`. |
| Packages | `token`, `lexer`, `preprocessor`, `parser`, `ast` | The same five, plus `svint`, `sema`, `elab`, `lint` and `diag`. |
| Conformance | A 387-file baseline of the sv-tests corpus (parse stage). | The whole sv-tests corpus (1,014 files) through elaboration, with a checked-in baseline of what is not yet handled. |
| Used by | The released [sigils](https://github.com/jfetkotto/sigils) language server. | The development version of sigils, which uses it for hover, diagnostics, lint, navigation and semantic tokens. |

Both modules keep the same promise: no public function panics, a malformed
construct is reported and the next one still parses, and positions are UTF-16
columns so they can go straight into the Language Server Protocol.

## Which one should I use?

- **Use v0.1** if you only need the outline of a design (what modules, ports,
  parameters, types and instances exist) and want the smallest, most
  conservative dependency. It is stable and only receives bug fixes.
- **Use v2** if you need what is inside the bodies (expressions and
  statements), meaning of names (types, widths, constant values), the
  instance tree with resolved parameters, or lint findings. It is a larger
  surface that is still changing until `v2.0.0`.

## Moving from v0.1 to v2

The packages you already use keep their roles and most of their API, so the
change is mostly mechanical:

- **Import path**: add `/v2` (`github.com/jfetkotto/svparse/v2/parser`, and so
  on). Go requires the suffix for a major version above 1.
- **New node types in old places**: containers now hold more kinds of
  declaration (`Always`, `Initial`, `ContinuousAssign`, `Generate*`,
  `GateInstantiation`, `AssertionItem`, `Covergroup`, ...). Code that switches
  on declaration types should ignore the ones it does not know.
- **Raw token spans are deprecated**: `Variable.Initial`, `Parameter.Default`,
  `Dim.Left` and `Dim.Right`, `EnumMember.Value`, `ParamOverride.Value`,
  `PortConnection.Expr`, `Port.Default`, `Arg.Default` and `Class.ExtendsArgs`
  still work, but each has a parsed sibling (`InitialExpr`, `DefaultExpr`,
  `LeftExpr`, `ValueExpr`, `ConnExpr`, ...) to use instead. Print one with
  `ast.ExprString`.
- **More errors**: `parser.Parse` now reports errors from inside function and
  task bodies, procedural blocks and expressions, so a file that parsed without
  errors before may report some now.
- **Lexer**: `32 'd 1` is one literal, `8'(x)` is a size cast, `<->` is a token,
  and `token.Token` has an `End` method.
- **Comments on request**: `lexer.LexWithComments` and
  `preprocessor.PreprocessDetailed` can return the comments they skip, for doc
  text and lint suppression.
- **New entry points**: `ast.Walk`, `ast.Inspect` and `ast.Dump`; `parser.ParseExpr`
  and `parser.ParseStmt`; and the `sema`, `elab` and `lint` packages.

The details are in [v2/README.md](v2/README.md) and
[docs/roadmap.md](docs/roadmap.md).

## Branches and releases

- `main` holds released code: the v0.1 line (`v0.1.11`), and later the v2
  release.
- `devel` is where development happens. Each change is made on its own branch
  and merged into `devel` with a merge commit.
- A fix that also applies to v0.1 is made on a `bugfix/` branch cut from `main`,
  released as the next `v0.1.x`, and merged back into `devel`. New features are
  v2 only.
- Tags `v0.1.x` belong to the root module. A future `v2.0.0` will belong to the
  module in `v2/`.

---

# v0.1 (this directory)

## Capabilities

svparse v0.1 is **declaration-grade**, not a full expression/statement parser:

- Parsed for real: ports, nets/variables, parameters, `typedef`/struct/enum,
  function/task signatures, class members, package imports, containers
  (`module`/`interface`/`program`), and instantiations.
- Intentionally *not* parsed: expression and statement grammar, and
  procedural block bodies (`always`, function/task bodies, constraint
  blocks). These are recognized and skipped via balanced-token matching,
  and kept as raw token spans on the AST rather than an expression tree.

The pipeline is layered across four packages:

| Package        | Role                                                             |
|----------------|-------------------------------------------------------------------|
| `token`        | Token vocabulary (`Token{Kind, Text, Line, Character}`)          |
| `lexer`        | Scans source into a token stream                                 |
| `preprocessor` | Macro expansion, conditional compilation, `` `include`` resolution, with source maps back to real positions |
| `parser`       | Builds the `ast.File` declaration tree                           |

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

## Releases

`v0.1.11` is a bug-fix release of the declaration-grade parser.

- An immediate assertion (`assert`, `assume`, `cover`, `restrict`) after an event
  control or an `if`/`else` stays in its `always` block, as in
  `always @(posedge clk) assert (a);`. It used to report "expected ';' before
  assert".

`v0.1.10` fixed:

- Size casts such as `16'(a)` and `8'(N)` lex as a size literal and a cast tick
  instead of a "malformed numeric literal" error.
- `ast.ParamOverride` and `ast.PortConnection` have real spans: they used to end
  at 0:0, before their start.
- A procedural `assign` after `else`, an `if`/`while` header, a label or `begin`
  stays in its `always` block instead of reporting "expected ';' before assign".

---

# v2 ([`v2/`](v2/))

The full front end: syntax tree of the whole language, semantic model,
elaboration and lint, with a command-line tool. See [v2/README.md](v2/README.md)
for its API and CLI, and [docs/roadmap.md](docs/roadmap.md) for how it got
here and what is left.

```sh
go install github.com/jfetkotto/svparse/v2/cmd/svparse@latest   # once v2.0.0 is released
svparse lint --top top -f files.f
```

## License

BSD 3-Clause. See [LICENSE](LICENSE).
