# Porting Sluice

A new implementation is a real possibility rather than a courtesy: the whole
project is arranged so that the language is defined by data, not by the Go code.
[`AGENTS.md`](../AGENTS.md) is normative and written to be implementable without
reading any reference source, and [`conformance/`](../conformance/) decides
whether you succeeded.

Two implementations already exist — Go at the module root and `@sluice/core` in
`js/packages/core` — and they drifted zero times, because neither is allowed to
change behaviour without a corpus case.

## The shape

```
input string
  → lex      → tokens
  → parse    → AST          ← also the wire format
  → resolve  → AST bound to schema fields
  → emit     → { sql, args, fields }
```

Each stage is separately addressable in the adapter protocol, so you can build
and validate one at a time rather than debugging the whole pipeline at once.

## Order of work

Work the corpus in file order. Each file is a stage, and the numbering is the
recommended sequence:

| File | What it decides |
|---|---|
| `001-lex.json` | token streams and spans, operator adjacency, string escapes |
| `002-parse.json` | AST structure, precedence, associativity |
| `003-diagnostics.json` | diagnostic codes and spans |
| `004-compile-postgres.json` | exact SQL and arguments |
| `004-compile-duckdb.json` | the same predicates, showing what a dialect may change |
| `005-suggest.json` | completion sets and their order |
| `006-security.json` | every input the origin implementation wrongly accepted |

Run `006` first when you think you are finished. It is a regression corpus in the
strict sense: every case is a query that compiled to executable SQL in the
implementation this project generalizes.

Three things are worth knowing before you start, because each is a whole class of
bug:

- **Positions are codepoint offsets**, half-open `[start, end)`. If your language
  counts UTF-16 units or bytes, convert at the boundary — `001-lex.json` has a
  case with an astral character that will tell you immediately.
- **Nothing unrecognized may enter the token stream.** Recovery emits diagnostics,
  never passthrough tokens. The lexer is where invariant 2 starts.
- **Output is byte-identical across implementations.** Every binary node is
  parenthesized unconditionally so that no precedence reasoning is needed to
  predict output, and dictionary iteration order must never be observable.

## The adapter

Ship an executable that speaks JSON Lines on stdin and stdout: one request per
line, one response per line, in order, exit 0 when stdin closes. No banner
output; anything for humans goes to stderr.

```
→ {"id":"1","op":"compile","schema":"machines","dialect":"postgres","input":"phase = \"in-use\""}
← {"id":"1","sql":"LOWER(inv.phase) = $1","args":["in-use"],"fields":["phase"],"diagnostics":[]}

→ {"id":"2","op":"suggest","schema":"machines","input":"phase = ","cursor":8}
← {"id":"2","suggestions":[{"text":"in-use","kind":"value","replaceSpan":[8,8]}]}
```

`op` is one of `lex`, `parse`, `compile`, `validate`, `suggest`, `schema`.
`schema` is either an inline object or the basename of a file in
`conformance/schemas/`, which the runner points at with
`SLUICE_CONFORMANCE_SCHEMAS`. A response must not omit `diagnostics` when
diagnostics were produced, and must not include `sql` when they were. §11 of
AGENTS.md is the full protocol; the two existing adapters are about 150 lines
each.

## Joining the matrix

Add one entry to [`conformance/adapters.json`](../conformance/adapters.json):

```json
{ "name": "rust",
  "command": ["target/release/sluice-conformance-adapter"],
  "dialects": ["postgres"],
  "ops": ["lex", "parse", "compile", "validate", "suggest", "schema"] }
```

Nothing else in the suite is language-aware. Declare only the ops and dialects
you implement — cases outside them are skipped rather than failed, so a
lexer-only implementation is a legitimate first pull request.

If your runtime is not something every contributor will have installed, add a
container beside the command:

```json
"container": { "image": "sluice-tools", "build": "make tools" }
```

The runner uses the host command when it works and the image when it does not,
mounting the repository read-only. That is how the JS adapter runs on a machine
with no Node installed.

## Done

The checklist in AGENTS.md §13, in order: lexer, parser, resolver, emitter,
suggester, registered adapter green in CI — and then a re-read of the invariants
against your own diff. That last step is not a formality. Search your
implementation for every site where a value could reach a SQL string, and confirm
each one goes through the placeholder path.
