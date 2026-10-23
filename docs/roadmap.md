# Roadmap: full SystemVerilog front end

svparse is growing from a declaration-grade parser into a full front end:
complete grammar, semantic model, hierarchy elaboration, and lint. Each
checkbox is one branch. Take the next unchecked item, tick it when it merges,
and record the conformance mismatch count where relevant. If a session stops
partway through an item, leave a short "Status:" note under it.

## M0: Setup

- [x] sigilsv2 checkout at `~/proj/sigilsv2` building against this tree via a
      local, uncommitted `go.work`
- [x] `AGENTS.md`, `CLAUDE.md`, this roadmap
- [x] `chore/vendor-full-sv-tests`: vendor remaining sv-tests chapters at the
      NOTICE.md commit; update NOTICE.md. Only chapters 9-12, 14-16, 18, 20,
      21 and `generic` exist upstream at that commit (627 files, 1014 total).
      `parser/corpus_test.go` stays scoped to the original 387 via
      `declarationGradeChapters`.
- [x] `feature/conformance-harness`: `internal/conformance` honouring sv-tests
      metadata (`:type:`, `:should_fail_because:`, `:top_module:`,
      `:defines:`, `:incdirs:`), with `testdata/conformance-baseline.txt`.
      Raise `conformance.MaxImplemented` when a new stage lands, then fix or
      explain the entries that appear. Open question: 89 baseline entries
      only need the UVM library (tests tagged `uvm`; a git submodule upstream,
      not vendored). Vendoring it would let those files be judged for real.
- [x] `feature/ast-walk-dump`: `ast.Walk`/`ast.Inspect` and `ast.Dump`.
      New nodes must be added to `Walk` (walk.go) and `Dump` (dump.go,
      `describe`); the corpus tests in `ast/walk_corpus_test.go` fail if either
      misses one. `Node` is the wider interface (Decl plus Type, Instance, ...).

## M1: Expressions (LRM 11, Annex A.8)

- [x] `feature/expr-ast`: sealed `ast.Expr` node set incl. `BadExpr`, `TypeOrExpr`.
      37 node types in `ast/expr.go`, all in `Walk` and `Dump` (expressions dump
      inline, e.g. `(+ a (* b c))`). Only `ParamOverride.ValueExpr` exists so far;
      the other `*Expr` sibling fields arrive with `feature/expr-fields`.
      `TestEveryExprTypeIsExercised` fails if a new node lacks a test case.
      Note for the parser: operators are source text (`Op string`); `|->`,
      `|=>` and `->>` already lex.
- [x] `feature/expr-parser`: error-tolerant Pratt parser (`parser/expr.go`).
      `parser.ParseExpr(toks)` never returns nil; bad input becomes `BadExpr`.
      Also added: `ast.ExprString` (source printer), `token.Token.End()` (exact
      token end, replaces `Character+UTF16Len(Text)`), `<->` and the `8'(x)`
      size-cast tick in the lexer. Fuzzing found six real bugs (span nesting,
      CR width, raw dims, escaped identifiers, ...); the fuzz inputs live in
      `parser/testdata/fuzz` as regression seeds. Not parsed yet: `dist`
      (with `feature/constraints`), `randomize() with {...}`, `matches`/`&&&`
      (statements), SVA operators. Known limit: a chain of a million binary
      operators builds a left-nested tree that deep, so tree walks recurse
      that deeply; fine for real code.
- [x] `bugfix/lexer-spaced-based-literals`: `32 'd 1` and `'b 0` are one
      `IntLiteral` (Text keeps the spaces). Departs from LRM 5.7.1 on purpose in
      one case: a line break between the base and its digits is not joined, so a
      bare `'h` at the end of a line being typed cannot swallow the next line.
      The 11 spaced-literal spans left `knownRejectedSpans`.
- [x] `feature/expr-fields`: every raw expression span now has a parsed sibling
      (`InitialExpr`, `DefaultExpr`, `ValueExpr`, `ConnExpr`, `Dim.LeftExpr/
      RightExpr`, `Class.ExtendsArgExprs`); the raw spans stay populated. One
      pass, `fillExprs` (`parser/fill.go`), runs at the end of `Parse`.
      Expression errors are deliberately NOT reported yet (a bad span becomes a
      `BadExpr` over the whole span), so no baseline moved; surfacing them is a
      separate, deliberate change. `[*]` is `Dim.Wildcard`; a lone-name dim and a
      `parameter type` default are `TypeOrExpr`. Dimension arrays are cloned per
      owner so the tree stays a tree. `rawSpanSiblings` in `parser/fill_test.go`
      must list any new raw `[]preprocessor.Token` field. Still BadExpr in the
      corpus: `virtual x_if` type defaults (M2) and one undefined macro.
- [ ] `feature/expr-diagnostics` (new): report expression syntax errors from the
      fill pass as real parser errors, with the conformance and corpus baselines
      updated deliberately. Do this after M3 so statements are covered too.

## M2: Types and declarations (Annex A.2)

- [x] `feature/inline-aggregate-types`: anonymous `struct`/`union`/`enum` as a
      variable, port, argument, return, parameter or member type. `ast.Type`
      gains `Inline` (Name is the keyword); `Union` gains `Tagged`. One shared
      body parser serves typedefs and inline use (`parseInlineAggregate`);
      `parseDataType` replaces the type+dims pairs. `ownTypes` gives each
      declarator its own copy of the aggregate and of `ParamOverrides` (it also
      fixed a latent double-visit for `cls #(8) c, d;`). Struct/enum bodies now
      stop at a missing `}` instead of swallowing the rest of the file.
      Conformance 201 -> 185: 20 fixed, 4 negatives that were only "detected"
      by the old syntax error are now honestly `undetected`.
      Known gaps: an enum member's repeat count (`C[3]`) is parsed and dropped;
      an inline aggregate is not accepted inside an expression (`$bits(struct
      ...)`); sigils hover prints only `struct name`.
- [x] `bugfix/subnode-spans`: `ParamOverride` and `PortConnection` (the only
      two affected; `Instance` and `ModportPort` were already right) now have
      real spans via `tokensSpan`. `TestEveryNodeSpanIsWellFormedAcrossTheCorpus`
      checks every node Walk reaches, not just declarations, which is how this
      escaped. Named forms stay positioned on the name alone (sigils relies on it).
- [x] `feature/class-parameterization`: `extends Base #(...)`, `extends
      pkg::Base #(...)`, `implements Pkg::I #(...)`, and an interface class's
      `extends a#(T), b#(T)` list, as `Class.ExtendsTypes`/`ImplementsTypes`
      (`[]Type`; the bare-name `Extends`/`Implements` stay for sigils). A
      parameterized class is also accepted as a port, argument, return and
      struct-member type (`parseClassTypeSuffix`), and `cls #(8)::inner_t` puts
      the class in `Type.PackageQualifier` with its overrides in the new
      `Type.QualifierOverrides`. Keyword types never take a list, so `wire
      #(5) w` is not mistaken for one. Unclosed `#(`/constructor-argument lists
      now stop at the statement's `;` (`parenGroupEnd`) instead of running on.
      Conformance 185 -> 175 (12 fixed, 2 negatives now honestly `undetected`).
      Not done: `type` parameters already worked; a chained `C#(1)::D#(2)::T`
      and an out-of-block method of a parameterized class are not handled.
- [x] `bugfix/aggregate-leftovers` (found after inline-aggregate-types):
      forward typedefs with a keyword (`typedef struct|union|enum Name;`, new
      `Typedef.ForwardKind`, now also set for `typedef class`) and `rand`/`randc`
      struct members. Conformance 175 -> 171.
- [x] `feature/nettype-interconnect-specparam`: `nettype` (new `ast.Nettype`,
      alias and `with` forms), `interconnect` nets, `specparam`
      (`Parameter.IsSpecparam`), `vectored`/`scalared`, drive/charge strengths,
      net delays (`Variable.NetQualifier`/`DriveStrength`/`Delay`), and the
      `type(expr)` data type (`Type.TypeRef`). The suffix after a net type is
      one small loop (`parseNetSuffix`) taking strength, qualifier, dimensions
      and delay in the orders LRM 6.7 allows; strength and delay are net-only,
      so `logic #5 x` stays an error. Shared delay and `type()` nodes are
      deep-copied per declarator (`cloneExpr`, reflection-based) to keep a tree.
      Conformance 171 -> 165 (8 fixed, 2 negatives now `undetected`).
      Not done: continuous-assignment delay/strength (`assign #5 ...`) is M3;
      sigils does not index `nettype` yet.
- [x] `feature/lexer-trivia`: comments are kept on request. `token.Comment`
      (text, position, `OwnLine`, `Body()`), `lexer.LexWithComments`,
      `preprocessor.PreprocessDetailed` / `Options.KeepComments` / `Result`, and
      `preprocessor.CommentIndex` (`Leading`, `Trailing`, `DocText`). A side
      list rather than a field on every token: `Token` stays small and
      untouched, and a fuzzer pins that keeping comments never changes tokens or
      errors. `OwnLine` is what separates a doc comment from a trailing one.
      Not done: parsing `// svparse: disable=<rule>` markers (that is the lint
      framework, M8), and sigils showing docs on hover (M9).

## M3: Statements and procedural code (LRM 9, 10, 12, 13)

- [ ] `feature/stmt-ast`
- [ ] `feature/stmt-parser`
- [ ] `feature/function-task-bodies`
- [ ] `feature/procedural-blocks`

## M4: Hierarchy-level constructs (LRM 23, 25, 27, 28, 29)

- [ ] `feature/generate`
- [ ] `feature/primitives`
- [ ] `feature/misc-items` (bind, defparam, config, let, clocking, alias, ...)

## M5: Verification constructs (LRM 16, 18, 19)

- [ ] `feature/sva`
- [ ] `feature/constraints`
- [ ] `feature/covergroups`

## M6: Semantic model

- [ ] `diag/` unified diagnostics with stable codes
- [ ] `svint/` 4-state arbitrary-width values
- [ ] `sema/types.go`
- [ ] `sema/compilation.go`
- [ ] `sema/scope.go`, `lookup.go`
- [ ] `sema/bind.go` (LRM 11.6/11.8 width and sign propagation)
- [ ] `sema/consteval.go`
- [ ] `sema/stmt.go`

## M7: Elaboration

- [ ] `elab/` instance tree, parameters, generate unrolling, port binding,
      hierarchical references; conformance `elaboration` stage enabled

## M8: Lint

- [ ] `lint/` rule framework and config
- [ ] Rule families (one branch each): width, unused/undriven, multi-driver,
      always_comb/latch, blocking/nonblocking misuse, implicit nets, case
      default, unreachable/shadowing/sign mixing
- [ ] `cmd/svparse` CLI

## M9: sigilsv2 adoption

- [ ] After M1: bump svparse, no behaviour change
- [ ] After M3: semantic tokens, outline for procedural/generate blocks
- [ ] After M6: sema diagnostics, typed hover, sema-backed resolution
- [ ] After M7/M8: background elaboration, lint diagnostics
- [ ] Mark raw-span AST fields `Deprecated`

## Conformance log

| Merged item | Conformance mismatches |
|-------------|------------------------|
| conformance-harness (parse stage; 1014 files) | 201 (59 undetected negatives, 53 parser gaps, 89 need UVM) |
| ast-walk-dump (no pipeline change) | 201 |
| expr-ast (no pipeline change) | 201 |
| expr-parser (parser not wired in yet) | 201 |
| lexer-spaced-based-literals | 201 |
| expr-fields (errors suppressed on purpose) | 201 |
| inline-aggregate-types | 185 (63 undetected negatives, 33 parser gaps, 89 need UVM) |
| subnode-spans (no pipeline change) | 185 |
| class-parameterization | 175 (65 undetected negatives, 21 parser gaps, 89 need UVM) |
| aggregate-leftovers | 171 (65 undetected negatives, 17 parser gaps, 89 need UVM) |
| nettype-interconnect-specparam | 165 (67 undetected negatives, 9 parser gaps, 89 need UVM) |
| lexer-trivia (no pipeline change) | 165 |
