# Agent instructions

These rules apply to every coding agent and contributor working in this
repository. The long-term direction (full grammar, semantic model,
elaboration, lint) is tracked in [docs/roadmap.md](docs/roadmap.md): read it
at the start of every session, take the next unchecked item, and tick it off
(with the conformance count) when it merges.

## Git commits

Never add a `Co-Authored-By: Claude ...` trailer (or any other AI-attribution
line, such as "Generated with ...") to commit messages or pull request
descriptions. End the commit message after its last content line.

## Commit messages

Keep commit messages short: a concise, accurate subject line plus at most
1-3 sentences in the body stating what changed. Do not include root-cause
narratives, symptom walkthroughs, or alternatives-considered discussion.

## Branches

Never commit directly to `main`. Do each change on its own branch named for
the kind of change (`feature/<slug>`, `bugfix/<slug>`, `perf/<slug>`,
`ci/<slug>`, `chore/<slug>`), then merge it into `main` with a merge commit
(`git merge --no-ff`) once it is ready. One roadmap item per branch.

## Writing style

Never use the em-dash character in any output: prose, docs, commit messages,
code comments, all of it. Use a comma, colon, parentheses, or split into two
sentences instead.

Doc comments explain why, not just what, and cite the IEEE 1800-2023 clause
(e.g. "LRM 23.2.2.3") a construct comes from. Annex A's BNF is the grammar
reference for every parser change.

## Invariants (never regress these)

- No public entry point panics, on any input. Every new entry point gets a
  fuzz test in the style of `parser.FuzzParseNeverPanics`.
- Error tolerance: a malformed construct is recorded as an error and
  recovery happens at the smallest enclosing unit (expression, statement,
  item, declaration). An unclosed block must not swallow the rest of the
  file, and a missing `;` must not eat the next declaration.
- Positions are UTF-16 columns; a declaration with no body spans its name
  (`namePosition`/`endOfName` in `parser/parser.go`), never a zero-width or
  backwards range.
- Compatibility: sigils consumes `ast`/`parser`/`preprocessor`. Evolve in
  place: add new fields and node types beside existing ones, keep raw
  `[]preprocessor.Token` spans populated, and only mark them `Deprecated`
  once sigils no longer reads them.

## Baselines

`testdata/sv-tests-baseline.txt` (and the conformance baseline once it
exists) only change on purpose. Every entry carries a reason; removing one
means a fix landed with its own tests. Never regenerate a baseline wholesale.

## Before merging

Run what CI runs:

```sh
go build ./... && go vet ./... && go mod tidy -diff && go test ./... -race && test -z "$(gofmt -l .)"
```

Also run the sibling sigils checkout's tests against this tree (it uses a
local `go.work` pointing here) to catch API breaks early.
