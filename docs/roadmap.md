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

- [ ] `feature/inline-aggregate-types`
- [ ] `feature/class-parameterization`
- [ ] `feature/nettype-interconnect-specparam`
- [ ] `feature/lexer-trivia` (comments retained for suppression and hover)

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
