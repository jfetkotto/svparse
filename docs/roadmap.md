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

- [x] `feature/stmt-ast`: `ast/stmt.go`, 34 node types (statements plus the
      procedural items `Initial`, `Final`, `Always`, `ContinuousAssign`, which
      are Decls). Consolidations from the plan: one `Assign` (nonblocking is
      `Op == "<="`), one `Block` for `begin` and `fork`, `Labeled` as a wrapper.
      `RawStmt` is for a construct recognised but not modeled yet (pattern
      matching `matches`, `randsequence`): tokens kept, never an error, so the
      parser can replace the skip helpers without regressing the baselines.
      `BadStmt` is a real failure. `TimingControl`/`EventTerm` model `#`, `##`,
      `@(...)`, `@*`, intra-assignment control. Walk, Dump and the
      `TestEveryStmtTypeIsExercised` guard are in. Block-local declarations and
      statements are kept in two lists (the LRM orders them), so a block that
      mixes them loses their relative order.
- [x] `feature/stmt-parser`: `parser.ParseStmt` (`parser/stmt.go`) and
      `ast.StmtString`. Not wired into function/task bodies or procedural blocks
      yet (the next two items); those still skip. A simple statement reads its
      left side at primary level, so `a <= b;` is a nonblocking assign, not a
      comparison. Block, if (else-if chains are iterative, not recursive), case,
      for (header parsed by a bounded sub-parser), foreach, loops, timing and
      event controls, wait, triggers, jumps, procedural assigns, immediate
      assertions, randcase, and block-local declarations all parse. `matches`
      patterns, `randsequence`, `expect` and concurrent assertions stay
      `RawStmt` (via the old skip helpers, errors discarded). Corpus check:
      708 of 715 `initial`/`always`/`final` blocks parse; the other 7 are four
      deliberate negative tests and three missing macros
      (`knownRejectedBlocks`). Also fixed along the way: `@(*)` lexed as an
      attribute opener; `q.find_first with (...)` without call parentheses;
      `virtual iface` as a type in an expression (new `Type.Virtual`, also set by
      the declaration parser); a pasted-macro span caveat is documented in the
      corpus test. Fuzzing found a panic at the depth limit, two printer
      disagreements, and a token-adjacency bug in `tokenSource`; all fixed with
      regression seeds.
- [x] `feature/function-task-bodies`: `Function`/`Task` gain `Decls` and `Stmts`.
      `skipBody` still finds where a body ends, so declaration boundaries and
      recovery did not move; the tokens in between are parsed by a bounded
      sub-parser (`parseSubroutineBody`), so it cannot read past `endfunction`.
      Old-style arguments declared in the body (`function f; input int a;`) become
      `Args`. Body errors are NOT reported yet: they are kept in
      `parser.suppressed` for `feature/expr-diagnostics`. Conformance unchanged
      (165). Corpus: all 377 bodies parse; the only suppressed errors are 240
      undefined `uvm_*` macros (UVM is not vendored). Added on the way: inline
      constraint blocks `randomize() with {...}` / `with (v) {...}` as an unparsed
      `ast.ConstraintBlock` (replaced when `feature/constraints` lands), and a
      `for` initialisation with several typed declarations
      (`for (int i = 0, bit c = 1; ...)`). sigils gains argument lists for
      old-style subroutines in hover and outline (see `demosv/rtl/old_style.sv`).
- [x] `feature/procedural-blocks`: `initial`, `final`, `always*` and `assign` now
      become `Initial`, `Final`, `Always`, `ContinuousAssign` nodes in
      `Container.Body` (`parser/procedural.go`). Extents still come from
      `skipProceduralConstruct`; the consumed tokens are parsed by a bounded
      sub-parser and its errors go to `parser.suppressed`. Conformance stays
      165. Concurrent assertions are still skipped (M5).

## M4: Hierarchy-level constructs (LRM 23, 25, 27, 28, 29)

- [x] `feature/generate`: `generate` regions, blocks, `if`/`else if`, `for` and
      `case` are `GenerateRegion`, `GenerateBlock`, `GenerateIf`, `GenerateFor`,
      `GenerateCase` nodes (`parser/generate.go`) instead of being flattened.
      Conditions and headers are parsed with errors suppressed; an `else`
      the skip swallowed after a bare `assign`/`always` is handed back
      (`giveBackElse`). Nesting is capped at 256. Consumers must now descend
      into these nodes: sigilsv2 branch `feature/generate-nodes` does, and
      sigils pinned to v0.1.9 is unaffected until it bumps.
- [x] `feature/primitives`: gate and switch instantiations become
      `GateInstantiation`/`GateInstance` (strength, delay, optional names,
      arrays, terminals as expressions) and `primitive ... endprimitive`
      becomes `UDP` with `UDPRow` table entries (`parser/primitives.go`).
      UDPs used to produce a cascade of syntax errors; they now parse clean.
      The vendored sv-tests has no chapter 28/29 files, so coverage is
      hand-written tests only.
- [x] `feature/misc-items`: `TimeUnit`, `Defparam`, `Bind`, `Let`, `Alias`,
      `Export`, `DPIExport`, `DefaultClocking`, `DefaultDisable`, `Clocking`
      (items with directions, skews and signals; global and default forms) and
      `Config` (design cells and rules as text) in `parser/misc.go`,
      `clocking.go`, `config.go`. Values are parsed with errors suppressed;
      a missing `;` stops before the next declaration. Property, sequence
      and let items inside a clocking block are still skipped (M5). Fixed
      conformance entry chapter-14/14.3--default-clocking-block.sv.

## M5: Verification constructs (LRM 16, 18, 19)

- [x] `feature/sva`: property and sequence expressions (`parser/sva.go`:
      `SeqDelay`, `SeqRepeat`, `PropPrefix`, `PropIf`, `PropCase`, `ClockedProp`,
      `DisableIff`, `SeqMatch`, plus `Binary` for the infix operators, with the
      LRM 16 table 16-1 precedence), `PropertyDecl`/`SequenceDecl`,
      `ConcurrentAssert` (assert, assume, cover, cover sequence, restrict,
      expect) as a statement and `AssertionItem` at module scope. Extents still
      come from the old skips, errors are suppressed, and no lexer change was
      needed (`#-#` and `#=#` are read as three tokens). Not covered: `dist`
      in properties (constraints item), `checker` bodies, and property/sequence
      items inside clocking blocks (skipped). Corpus: 6 properties, 21
      sequences, 39 assertions, no BadExpr.
- [x] `feature/constraints`: constraint blocks are parsed into
      `ConstraintItem` nodes (`ConstraintExpr` with optional `soft` and `dist`,
      `ConstraintImplies`, `ConstraintIf`, `ConstraintForeach`,
      `ConstraintSolve`, `ConstraintDisableSoft`, `ConstraintUnique`,
      `ConstraintSet`, `ConstraintRaw` for the unparseable) in
      `Constraint.Items` and in the inline `with { }` `ConstraintBlock.Items`,
      which keeps its `Tokens`. Errors are suppressed; extents still come from
      `skipBracedBlock`. Corpus: 125 constraints, 13 inline blocks, 5 dist,
      no BadExpr or ConstraintRaw. Fuzzing found a panic on a constraint at
      the end of input (seed `constraint A`), fixed with a regression seed.
- [x] `feature/covergroups`: `Covergroup` (ports, sampling event, `with function
      sample`, `extends`), `CoverOption`, `CoverPoint` (label, type, `iff`),
      `CoverBin` (values, `with`, set, transitions, default, default sequence,
      wildcard, arrays, cross selections), `TransSet`/`TransStep`, `CoverCross`
      in `parser/coverage.go`. Cross select expressions reuse `Binary`
      (`intersect`, `with`, `matches`), `Unary` and a `binsof` `Call`.
      Extents still come from `skipToKeyword("endgroup")`; errors are
      suppressed. `@@(begin f)` is flagged but its contents are not modeled.
      The vendored sv-tests has no covergroup file, so tests are hand-written.

## M6: Semantic model

- [x] `diag/` unified diagnostics with stable codes: `Diagnostic`, `Severity`,
      `Related`, the `Known` code registry (kebab-case enforced), `Sort`, a
      deduplicating `Set`, and `FromParser`/`FromPreprocessor` adapters. sigils
      can adopt it in M9 in place of its own conversion.
- [x] `svint/` 4-state arbitrary-width values: `Value` (two bit planes, width up
      to `MaxWidth`, invalid zero value that propagates), `ParseLiteral` (LRM
      5.7.1 incl. x/z/? digits, extension rules, unbased unsized via `Fill`),
      all LRM 11.4 operators (bitwise, reduction, logical, arithmetic with
      big.Int, `**` rules, shifts, relational, `==`/`===`/`==?`, `?:` merge),
      concat/replicate/slice/index, `$clog2`, real and string conversion.
      Tested against uint64/int64/big.Int references and 4-state truth tables.
- [x] `sema/types.go`: the type model (`Integral` with reg/logic folding and
      packed range, `Real`, `String`/`Chandle`/`Event`/`Void`/`Null`/`Invalid`
      singletons, `Alias`, packed/unpacked/dynamic/queue/assoc arrays,
      `Struct`, `Union`, `Enum`, `Class`, `VirtualInterface`), classification,
      `Bits` ($bits), and the LRM 6.22 relations `Matches`, `Equivalent`,
      `Assignment` (Assignable/CastRequired/Incompatible) and `Castable`.
      Building these from `ast.Type` waits for scopes and lookup.
- [x] `sema/compilation.go`: `Compilation` over many files (`AddFile` replaces
      by path, `RemoveFile`), `Definitions`, `Lookup`/`Package`/`Config` in
      their separate name spaces (LRM 3.13), per-file or shared (`SingleUnit`,
      -mfcu) `$unit` scopes with enum constants, `Tops`, and
      `duplicate-definition`/`duplicate-declaration` diagnostics with the
      original as related location. Needed `Function`/`Task`/`Constraint`
      `Scope` (the class of an out-of-block `C::m`) so those are not taken for
      free subroutines.
- [x] `sema/scope.go`, `lookup.go`: `Compilation.Model()` builds the scope tree
      (`Scope`, `Symbol`, 30 `SymKind`s): $unit, packages, modules and nested
      modules, classes, subroutines (out-of-block `C::m` nested in the class),
      named and unnamed blocks, for/foreach scopes, generate blocks (with
      genblkN naming and loop genvars), covergroups, properties, sequences,
      clocking blocks; duplicate-declaration diagnostics inside scopes (a port
      re-declared as a variable is folded). `Scope.Lookup` follows LRM 23.9
      (own, explicit imports, wildcard imports with ambiguity, inherited class
      members, then outward). `LookupQualified` (`pkg::`, `Class::`, `$unit::`,
      nested, exports incl. `*::*`), `LookupPath` (hierarchical through
      instances, blocks, `$root`) and `ClassifyName` (the TypeOrExpr
      ambiguity). Not yet: position-to-scope (`ScopeAt`), type parameters of
      parameterized classes, interface ports and modport members.
- [x] `sema/bind.go` (LRM 11.6/11.8 width and sign propagation): `Model.Bind`
      and `BindAssign` produce a typed `BoundExpr` tree (`bound.go`, `WalkBound`);
      `resolve.go` turns `ast.Type` into `sema.Type` (typedefs, enums with
      values, structs, unions, classes with base, arrays, type parameters,
      interface ports, built-in std classes) and gives `TypeOf` for every value
      symbol. Operators size their operands per LRM 11.8 (arithmetic, bitwise,
      ?:, shifts, relational pairs), assignment context widens them, real
      promotes, calls resolve to user functions, class methods or built-in
      methods and system functions; diagnostics `undeclared-identifier`,
      `incompatible-types`, `cast-required`, `invalid-type`.
- [x] `sema/consteval.go` (with constant functions in `constfunc.go`): `Eval`, `EvalAssign`, `ParamValue`, `Env` (parameter
      overrides, step budget, circular-parameter detection) evaluate bound
      expressions with `svint`: literals, parameters, enum constants, all
      operators, selects, casts, inside, and the system functions ($clog2,
      $bits, $size/$left/..., math, $signed, $countones, ...). Corpus: 90% of
      declarations typed, 89% of parameters evaluate (the rest need UVM).
      Constant functions (LRM 13.4.3) run through a small statement
      interpreter (locals, whole/bit/part assignment, compound ops, ++/--,
      if, case/casez/casex/inside, for/while/do/repeat/forever, break,
      continue, return, recursion to depth 200, default and named arguments)
      under the same step budget; anything that reads simulation state is
      `not-constant`. Still to do at elaboration: per-instance types
      (dimensions follow default parameter values until then).
- [x] `sema/stmt.go`: `Model.CheckStatements` applies the LRM statement rules
      to every process, subroutine and continuous assignment: assignable
      targets (`invalid-lvalue`, `assign-to-input`, `assign-to-constant`,
      `procedural-assign-to-net`), `break`/`continue` outside loops, `return`
      with or without a value (`return-value-in-void`, `missing-return-value`,
      `return-outside-subroutine`), `timing-in-function`, and variable drivers
      (`multiple-drivers`: a whole variable driven by a continuous assignment
      or always_comb/latch/ff has no other driver; an initial block may still
      set an always_* variable). On the corpus it flags exactly the five
      deliberately illegal files (6.5 x2, 10.3, 13.4.1, 14.3) and nothing
      else; wiring these into the conformance harness comes with M7.
      Also fixed a parser extent bug it exposed: a procedural `assign` inside
      an if/else was split off the always block (conformance 164 to 163).

## M7: Elaboration

- [x] `feature/elab-instances`: `elab.Elaborate(ctx, comp, opts)` builds the
      instance tree: tops (explicit or uninstantiated), per-instance
      parameters from positional and named `#()` overrides, `defparam`
      (relative, forwarded below, beats `#()`), defaults and dependent
      localparams, type-parameter overrides, instance arrays (multi-dim,
      either direction), generate if/else-if/case/for with `genblkN` naming
      (numbered per construct, LRM 27.6), genvar-dependent block parameters,
      recursion and depth/instance/iteration limits, cancellation, and the
      diagnostics `unknown-module`, `unknown-parameter`, `too-many-parameters`,
      `local-parameter-override`, `recursive-instantiation`,
      `instantiation-depth`, `instance-limit`, `generate-limit`,
      `unknown-top`, `no-top`. Corpus: all 1014 files elaborate; two gaps
      remain (`$isunbounded`, aggregate parameters).
- [x] `feature/elab-ports`: `PortBinding`s on every instance (`Node.Ports`):
      positional, named, `.name`, `.*` and `.p()` connections; unknown,
      too many, duplicate and mixed connections; output/inout drivability,
      type compatibility, and width mismatch (a warning) checked against the
      instance's own port types (`TypeOfIn`/`BindIn` resolve array bounds with
      the instance's parameters); interface ports bound to interface
      instances and modports, including pass-through and generic `interface`
      ports; implicit nets from port connections and continuous assignments
      (`Options.DefaultNettype`, "none" makes them errors; the preprocessor
      does not carry `default_nettype`, so it is one setting per design);
      `bind` directives (by module, by hierarchical instance, or `bind m : a`)
      elaborated into their targets; `Node.ResolveHier` / `Design.ResolveHier`
      for hierarchical names with upward search and `$root`.
- [x] `feature/elab-conformance`: `conformance.MaxImplemented` is now
      `StageElaboration`: a test that exercises elaboration (or simulation, which
      is judged on elaboration) also runs the duplicate-name checks, the
      statement rules and `elab.Elaborate` (the named `:top_module:`, else every
      uninstantiated module); errors reject, warnings do not. Six negative tests
      are now detected (6.5 x3, 10.3 bad, 13.4.1 bad, 14.3 error). Two valid
      files it wrongly rejected were fixed in sema: `parameter int i = $` with
      `$isunbounded` (`ConstUnbounded`) and aggregate parameters (assignment
      patterns for arrays and structs, `ConstArray`, indexing and member
      selects of constant aggregates). Baseline 163 to 157; the 61 remaining
      "undetected" negatives each need a rule of their own (real bit-select,
      enum type checking, undeclared names in expressions, class rules, ...).

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
| stmt-ast (no pipeline change) | 165 |
| stmt-parser (not wired into declarations yet) | 165 |
| function-task-bodies (errors suppressed on purpose) | 165 |
| procedural-blocks (errors suppressed on purpose) | 165 |
| generate (structured nodes) | 165 |
| primitives (UDP syntax errors gone, none in corpus) | 165 |
| misc-items (default clocking test fixed) | 164 |
| sva (concurrent assertions as nodes) | 164 |
| constraints | 164 |
| covergroups (none in corpus) | 164 |
| sema-stmt (procedural assign extent fix) | 163 |
| elab-instances (no conformance stage yet) | 163 |
| elab-ports | 163 |
| elab-conformance (elaboration stage enabled) | 157 (61 undetected negatives, 7 parser gaps, 89 need UVM) |
