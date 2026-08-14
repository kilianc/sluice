# Plan

Sluice generalizes the filter language of a private internal tool. This document
records what has to change on the way out, and the order the work lands in. The
normative language specification lives in [`AGENTS.md`](AGENTS.md); this file is
the roadmap and the decision log.

---

## 1. Origin

The origin implementation is roughly 1,300 lines across two independent
implementations of one grammar.

**Go** (~470 LOC) — a regex tokenizer feeding a single-pass compiler that emits a
SQL `WHERE` fragment. Fields are a package-level slice; each field maps to a
per-backend SQL emitter, with Postgres and DuckDB backends.

**JavaScript** (~850 LOC) — a second implementation of the same grammar that
builds a DFA-shaped "suggestion graph" from the field list and drives Monaco
autocomplete plus inline validation with error spans. The Go field list is
serialized to JSON and injected into the browser as a generated ES module, with
dynamic enum values filled per request.

What is worth keeping is the shape of the idea: **one field declaration produces
both the compiler and the editor**. Filter-string-to-SQL libraries are common;
deriving typed autocomplete from the same declaration the compiler uses is the
part that is actually differentiated. Everything below is in service of
extracting that idea and making it safe to hand to strangers.

Nothing in Sluice depends on access to that source. `AGENTS.md` is written to
stand alone and the conformance corpus pins the behavior, which is what makes the
extraction checkable rather than a matter of memory.

Four of its predicates are not a column comparison — an `EXISTS` over a JSONB
column, a name that selects between two columns by its value, a derived
comparison over a last-opened timestamp, and a comparison between two columns.
Treat them as an acceptance test on the emitter interface rather than as code to
port: if they express cleanly through `Builder`, the escape hatch is sufficient.
If they do not, the interface needs rethinking before v0.1.0 rather than after a
caller depends on it.

---

## 2. What has to change

### 2.1 Parameter binding — the blocking issue

The current compiler interpolates. Measured against the live implementation:

| input | current output |
|---|---|
| `state = "shared" OR EXISTS (SELECT 1 FROM document)` | `LOWER(state) = 'shared' OR exists ( select 1 from document )` |
| `1=1` | `1 = 1` |
| `bogus_column = "x"` | `bogus_column = 'x'` |
| `name ~ "%"` | `LIKE '%%%'` — matches every row |

The cause is the final `else` in the compile loop: any token that is not a known
field, operator, keyword, or quoted literal is appended to the output verbatim.
Behind SSO, against a read replica, with a statement timeout, this has been
survivable. It cannot ship publicly, and it cannot be the thing a browser
compiles.

Fixes, all specified in `AGENTS.md`: values leave only as bound parameters
(invariant 1); no passthrough branch exists (invariant 2); bare identifiers are
not valid in value position (§5); `LIKE` metacharacters are escaped with an
explicit `ESCAPE` clause (§8.2).

### 2.2 Real parser

The regex-split tokenizer cannot report positions on the Go side, so server
diagnostics are stringly and imprecise. It also produces two bugs that a lexer
removes for free: `!~` is unreachable (the split pattern breaks it into `!` and
`~`), and `<`/`>` require surrounding whitespace. Replaced by lexer → parser →
AST → dialect emitter, with the AST doubling as the browser↔server wire format.

### 2.3 Instance scope

`init()` selects the DuckDB backend process-wide, `initBackend` panics on a schema
mismatch, and `ColumnsByName`/`SortColumns` are package variables. A library
cannot own process-global state or panic on caller input. Becomes
`New(schema, dialect) (*Compiler, error)`.

### 2.4 De-domaining

Fields become configuration — a native struct or the canonical JSON of `AGENTS.md`
§4.3. Host concepts leave the library entirely; the awkward ones (`operation` as
an `EXISTS` over JSONB, `blocked`, `active`) survive the move as custom emitters
(§8.4), which is the feature that proves the escape hatch is sufficient.

### 2.5 Drift

Go and JS have already diverged in operator handling. The fix is structural: one
spec, plus a language-agnostic corpus that every implementation runs through a
common adapter protocol (§10, §11). A behavior change that lands without a corpus
case is the bug re-introducing itself.

### 2.6 Deliberate behavior changes

Not everything carries over unchanged. These are decisions, not accidents:

- **Global lowercasing is gone.** Today every token is lowercased during
  tokenization, so values are case-folded whether or not that was wanted. Case
  folding becomes a per-field, per-schema option, ASCII-only so that host and
  database agree (§8.2).
- **`NOT` is added**, with explicit precedence. The current implementation has no
  precedence at all — it splices tokens and inherits SQL's.
- **Durations become integer seconds** at coercion time, so no interval string
  reaches SQL. Months and years are not units.
- **Empty input compiles to empty**, never `1=1`.

### 2.7 Deferred, on purpose

Value lists (`state = ("a", "b")`), relative timestamp literals (`> "now-7d"`),
free-text search across a designated set of fields, and `ORDER BY` expressions
composed in the language rather than named in the schema. Each is defensible; none
belongs in v0.1 while the parser is still settling.

---

## 3. Scope

Sluice compiles a **predicate** and an **ORDER BY**. It does not do joins,
projections, aggregation, or subqueries, and it will not grow into a SQL
replacement. When a query needs those, the host application writes SQL and drops a
Sluice predicate into its `WHERE`. The constraint is the product: it is what keeps
the language teachable in a tooltip and the library small enough to depend on.

---

## 4. Layout

```
sluice/                      Go module, MIT
  schema.go                  Schema, Field, Sort, Options, PublicSchema
  compile.go                 New() → *Compiler; Compile, CompileAST, OrderBy
  validate.go                Validate, Diagnostic
  suggest.go                 Suggest, Suggestion
  ast/                       lexer, parser, node types, JSON codec
  dialect/                   postgres, duckdb, sqlite, mysql
  cmd/sluice/                CLI: compile, validate, schema, conformance-adapter
  conformance/               language-agnostic corpus + runner + adapter registry
js/
  packages/core/             @sluice/core — parse, validate, suggest, compile
  packages/monaco/           @sluice/monaco — editor binding        (v0.2)
  packages/codemirror/       @sluice/codemirror                     (v0.3)
docs/
  language.md schema.md dialects.md editor.md security.md porting.md
```

`@sluice/core` has zero runtime dependencies, ships as ESM, and never imports an
editor. Editor bindings are separate packages precisely so that a headless client
compiling for DuckDB-WASM pays nothing for Monaco.

---

## 5. API surface

### Go

```go
schema, err := sluice.LoadSchema(jsonBytes)      // or a native sluice.Schema
c, err := sluice.New(schema, postgres.Dialect)

res, err := c.Compile(`state = "shared" AND team ~ "desi"`)
res.SQL      // (LOWER(doc.state) = $1 AND LOWER(grp.name) LIKE $2 ESCAPE '\')
res.Args     // []any{"shared", "%desi%"}
res.Fields   // []string{"state", "team"} — for join pruning
res.AST      // *ast.Node

res, err = c.CompileAST(node)                    // untrusted-AST entry point (§6)
res, err = c.WithDynamic(map[string][]string{"team": teams}).Compile(input)

diags := c.Validate(input)                       // all diagnostics, with spans
sugg  := c.Suggest(input, cursor)
order, err := c.OrderBy("name", sluice.Desc)

pub := c.PublicSchema(dynamic)                   // JSON for the browser
```

`Compiler` is immutable and safe for concurrent use; `WithDynamic` returns a
lightweight view rather than mutating.

### JavaScript

```js
import { createLanguage } from '@sluice/core'
import { postgres, duckdb } from '@sluice/core/dialects'

const lang = createLanguage(publicSchema, { dynamic: { team: teams } })

lang.validate(input)              // { ok, diagnostics: [{ code, message, span }] }
lang.suggest(input, cursor)       // [{ text, kind, detail, replaceSpan }]
lang.parse(input)                 // { ast, diagnostics }
lang.compile(input, duckdb)       // { sql, args, fields, ast }
```

`compile` is available client-side because Mode A (§7) needs it. The public schema
carries no column SQL, so a client-side compile requires the host to supply
`column` values it is willing to publish — which is exactly the case when the
database is in the browser.

### Wire contract

The server exposes `GET /sluice/schema.json` returning `PublicSchema` with dynamic
values resolved. This replaces the current arrangement, where a templ template
generates an ES module containing the field list.

---

## 6. Conformance suite

The corpus is JSON, stored in `conformance/corpus/`, and asserts only
language-level facts — never Go or JS specifics. Implementations are driven
through the adapter protocol in `AGENTS.md` §11: a JSON Lines executable on
stdin/stdout, registered in `conformance/adapters.json`.

Files, in the order a port should attack them:

| file | asserts |
|---|---|
| `001-lex.json` | token streams and spans, including operator adjacency and string escapes |
| `002-parse.json` | AST structure, precedence, associativity |
| `003-diagnostics.json` | diagnostic codes and spans for malformed and unresolvable input |
| `004-compile-postgres.json` | exact SQL and args |
| `004-compile-duckdb.json` | exact SQL and args |
| `005-suggest.json` | completion sets and ordering at given cursor positions |
| `006-security.json` | every injection attempt the original implementation accepted, asserted to produce a diagnostic and no SQL |

`006` is a regression corpus in the strict sense: each case is a query that
compiled to executable SQL in the origin implementation. It is the file a port
should run first when
it thinks it is finished.

The reference runner is `go test ./conformance`, which shells out to each
registered adapter. CI runs it against Go and JS on every commit; a port in any
other language joins the same matrix by adding one entry to the registry.

---

## 7. Client-side compilation

The requirement that shaped the design: the browser should be able to produce SQL
so that a database in the browser, or a thin SQL endpoint, is enough — no
compilation service in between. `AGENTS.md` §12 is the normative treatment; the
summary and the reasoning:

Because values leave only as bound parameters, the SQL *shape* is a pure function
of the schema and the AST's structure, drawn from a finite set the schema
enumerates. Shape is verifiable. This is what makes client-side compilation
defensible rather than reckless.

- **Mode A, local execution** — compile in the browser, execute in DuckDB-WASM or
  PGlite. No trust boundary is crossed. This is the no-backend case in its honest
  form, and the one the docs lead with.
- **Mode B, AST transport** — send the AST; the server compiles it under its own
  schema. Recommended whenever a database sits behind a server. A hostile client
  can express only what the server's schema permits.
- **Mode C, SQL with proof** — send `{ source, sql, args }`; the server recompiles
  and compares. The transmitted SQL is never trusted, which means Mode C is Mode B
  plus version-skew detection. Specified because people will build it regardless,
  and the specified version cannot be turned into an injection.

There is no mode in which received SQL is inspected and then executed. No helper
that suggests otherwise ships.

---

## 8. Milestones

**M1 — Go core.** Lexer, parser, AST + JSON codec, resolver, emitter, Postgres and
DuckDB dialects, diagnostics with spans, `OrderBy`. Corpus `001`–`004` and `006`
authored alongside. Exit: `go test ./...` green, `006` fully green. Start from
§1 "Reading the origin": the custom emitter is the one interface M1 cannot defer,
because a caller will be depending on it.

**M2 — JS core + conformance harness.** `@sluice/core` to parity, adapter protocol
implemented on both sides, runner and registry, CI matrix. Corpus `005` authored
against both. Exit: identical results from both adapters on the whole corpus.

**M3 — v0.1.0.** `docs/` written, `cmd/sluice` CLI, `LoadSchema` validation and
`schema_invalid` diagnostics, semver policy, `CONTRIBUTING.md`, publish to
pkg.go.dev and npm under the `@sluice` scope. Exit: a stranger can install it and
compile a query from the README without reading anything else.

**M4 — editor.** `@sluice/monaco`, reduced to a binding over `@sluice/core`.
Ships a live playground built on PGlite, which doubles as the Mode A reference.
Exit: v0.2.0.

**M5 — breadth.** SQLite and MySQL dialects, `@sluice/codemirror`, deferred
grammar features from §2.7 as demand justifies.

Ordering note: migrating the internal caller deliberately follows the v0.1.0 tag
rather than preceding it. Migrating before publishing would let host-specific
convenience leak back into the library, which is the failure mode this whole
exercise exists to avoid.

---

## 9. Open questions

- **Number type coverage.** No numeric fields exist in the origin schema, so
  `number` is specified but unexercised by real usage. It needs a caller before
  the operator set can be called settled.
- **`timestamp` literals.** RFC 3339 only is correct and unpleasant to type. The
  relative form (`> "now-7d"`) deferred in §2.7 is what people will actually want;
  the question is whether it belongs in the language or in the editor as an
  expansion.
- **Dynamic value size.** Team lists run to thousands of entries. Shipping them
  inline in `schema.json` is fine at the current scale and will not stay fine;
  a completion endpoint is the likely answer, but not before someone feels it.
- **Collation.** ASCII-only case folding is specified because it is the only rule
  under which the host language and every database agree. Whether a schema should
  be able to opt into database-native collation — and lose cross-implementation
  determinism in exchange — is unresolved.
