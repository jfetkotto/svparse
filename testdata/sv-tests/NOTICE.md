# Vendored from chipsalliance/sv-tests

The `.sv` files under this directory are copied, unmodified, from
[chipsalliance/sv-tests](https://github.com/chipsalliance/sv-tests)
(ISC license), a test suite designed to check tool compliance with the
SystemVerilog standard.

- **Source commit:** `682f19035fc21c85b653661d8e1a6e4fe1acbf08` (2026-07-25)
- **License:** ISC (see below) - each vendored file also carries its own
  copyright/license header, unmodified, satisfying the notice-preservation
  requirement per file.
- **Selection:** every single-file test under `tests/chapter-{5,6,7,8,13,22,23,24,25,26}/`
  in the source repo (lexical conventions, data types, aggregate types,
  classes, tasks/functions, compiler directives, modules, programs,
  interfaces, packages) - the chapters relevant to svparse's declaration-grade
  scope. Chapters covering procedural statements, expressions, assertions,
  randomization, and system tasks/functions (9-12, 14-16, 18, 20-21), and the
  large multi-file `uvm`/`testbenches` directories, were deliberately not
  vendored - out of scope for a single-file, declaration-grade parser. This
  is a one-time snapshot, not fetched at build/test time.

This directory is used by `svparse/parser`'s corpus tests
(`corpus_test.go`) to validate the parser against real, spec-representative
SystemVerilog, as a complement to its existing fuzz testing.

## sv-tests license (ISC)

```
Copyright (C) 2020 The Symbiflow Authors

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```
