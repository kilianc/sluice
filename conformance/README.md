# Conformance suite

A language-agnostic corpus that defines what Sluice *is*. Implementations are
driven through a JSON Lines adapter protocol, so adding a language to the matrix
means adding one entry to [`adapters.json`](adapters.json) — nothing else in this
directory knows what language anything is written in.

The protocol is specified in [`../AGENTS.md`](../AGENTS.md) §11. This file covers
how to run the suite and how to add to it.

## Layout

```
schemas/machines.json         the schema every corpus file loads by name
corpus/001-lex.json           token streams and spans
corpus/002-parse.json         AST structure, precedence, associativity
corpus/003-diagnostics.json   diagnostic codes and spans
corpus/004-compile-*.json     exact SQL and arguments, per dialect
corpus/005-suggest.json       completion sets and ordering
corpus/006-security.json      inputs the origin implementation wrongly accepted
adapters.json                 implementation registry
```

## Running

```bash
go test ./conformance
```

The reference runner shells out to every registered adapter, feeds it each case,
and compares responses. To restrict the matrix:

```bash
go test ./conformance -run TestConformance/js -corpus 006-security
```

The JS adapter needs a JavaScript runtime, which is why it is invoked through the
registry rather than assumed present; on a host without one, run it in the
project's container:

```bash
make conformance-js
```

## Case format

Every corpus file carries file-level defaults — `op`, `schema`, `dialect` — that
individual cases may override. A case is otherwise a request (`input`, `cursor`,
`dynamic`, `ast`) plus the expected response keys.

```json
{
  "name": "case-1",
  "input": "phase = \"in-use\"",
  "sql": "LOWER(inv.phase) = $1",
  "args": ["in-use"],
  "fields": ["phase"]
}
```

Comparison follows AGENTS.md §11: `sql` is an exact string match; `args`,
`fields`, `ast`, `tokens`, and `suggestions` compare as ordered structures;
diagnostics compare on `code` and `span` only. **A case can never assert
diagnostic wording**, so messages stay free to improve without a corpus churn.

Keys the runner ignores: `name`, `description`, and `was` — the last records what
the input produced under the pre-Sluice implementation, and exists to make
`006-security.json` legible as history rather than a list of strings.

## Adding cases

- **Every behavior change lands with a case in the same commit.** This is the rule
  that keeps implementations from drifting, and it is the whole reason the suite
  is data rather than tests.
- **Assert language facts, never implementation facts.** If a case would fail on a
  correct implementation in another language, it is wrong.
- Prefer the narrowest `op` that can express the case — a lexer bug belongs in
  `001`, not in a compile case that happens to exercise it.
- Spans are 0-based codepoint offsets, half-open. Count them carefully; an
  off-by-one in the corpus is a bug every implementer inherits.

## Adding an implementation

1. Implement the adapter from AGENTS.md §11.
2. Register it in `adapters.json`.
3. Work the corpus in file order: `001` → `002` → `003` → `004` → `005`, then
   `006` last, as the check that the invariants actually hold.
4. Open a PR. A new language joins the CI matrix automatically once registered.

The checklist in AGENTS.md §13 is the definition of done.
