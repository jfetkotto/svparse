# svparse

An error-tolerant SystemVerilog/Verilog parser library for Go, built for
language-server-style tooling: it never panics or stops on malformed or
mid-edit source.

## Capabilities

svparse is **declaration-grade**, not a full expression/statement parser:

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

## License

BSD 3-Clause. See [LICENSE](LICENSE).
