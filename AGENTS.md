# Sluice — implementation specification

This document is normative. It is written so that an implementer with no access to
the reference source can produce a conforming Sluice implementation in any
language, and so that an agent working in this repository knows what may and may
not change.

Key words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are used in the RFC 2119
sense.

An implementation is conforming when it passes the corpus in [`conformance/`](conformance/)
via the adapter protocol in §10. Nothing else counts as conforming, including
"it looks right".

---

## 1. Invariants

These are the reasons the project exists. An implementation that breaks any of
them is not Sluice, however convenient the shortcut.

1. **No interpolation.** Values originating in the input string **MUST NOT** be
   concatenated into SQL text under any circumstance. They leave the compiler
   only through the parameter list. This holds for `LIKE` patterns, durations,
   UUIDs, numbers, and booleans alike.
2. **No passthrough.** There is no branch that copies an unrecognized token into
   the output. Unknown input is a diagnostic (§8), always.
3. **Identifiers come from the schema only.** Column SQL, table aliases, casts,
   and sort expressions are supplied by the host application in the schema. No
   part of the input string is ever treated as an identifier.
4. **Determinism.** For a given (schema, dialect, input), the emitted SQL string
   and argument list are byte-for-byte identical across implementations,
   platforms, and runs. Map/dictionary iteration order **MUST NOT** be observable
   in output.
5. **Bounded cost.** Parsing is O(n) in input length, and the limits in §4.2 are
   enforced before any allocation proportional to the input.

---

## 2. Processing model

```
input string
  → lex      (§3)  → []Token
  → parse    (§5)  → AST                  ← the wire format (§6)
  → resolve  (§7)  → AST bound to schema fields
  → emit     (§8)  → { sql, args, fields }
```

`suggest` (§9) operates on the token stream and the schema, not on a successful
parse — it **MUST** work on incomplete and invalid input, since that is the state
an editor is in while a user types.

Each stage is separately addressable in the adapter protocol (§10), so a port can
be built and validated stage by stage.

---

## 3. Lexical grammar

Input is UTF-8 text. **Positions are 0-based Unicode codepoint offsets**, and a
span is a half-open `[start, end)` pair. Implementations **MAY** additionally
expose native offsets (UTF-16 for JavaScript, bytes for Go) under different field
names, but the conformance protocol speaks codepoints.

### 3.1 Tokens

| Token | Pattern |
|---|---|
| `IDENT` | `[A-Za-z_][A-Za-z0-9_.]*` |
| `STRING` | `"` … `"` with escapes, see §3.2 |
| `NUMBER` | `-?[0-9]+(\.[0-9]+)?` |
| `TRUE` / `FALSE` | `true` / `false`, case-insensitive |
| `AND` / `OR` / `NOT` | case-insensitive |
| `OP` | `=` `!=` `~` `!~` `<` `<=` `>` `>=` |
| `LPAREN` / `RPAREN` | `(` / `)` |
| `EOF` | end of input |

Whitespace (space, tab, CR, LF) separates tokens and is otherwise insignificant.
There are no comments.

Operators **MUST** be matched longest-first: `!=` before `!`, `<=` before `<`,
`!~` before `!`. A bare `!` is `unexpected_token`.

Operators **MUST NOT** require surrounding whitespace. `state="shared"` and
`edited>"2 days"` lex identically to their spaced forms.

`AND`, `OR`, `NOT`, `true`, and `false` are reserved: they lex as keywords even
where a field of the same name exists. A schema **MUST NOT** declare a field with
a reserved name; loading such a schema is a `schema_invalid` error.

### 3.2 String literals

Delimited by `"`. Inside a string, `\` introduces an escape; the only valid
escapes are `\"`, `\\`, `\n`, `\t`, and `\r`. Any other character after `\` is
`invalid_escape`. A newline inside a string literal is permitted. Reaching EOF
before the closing quote is `unterminated_string`, spanning from the opening
quote to EOF.

Single quotes are **not** string delimiters. A `'` outside a string is
`unexpected_token`. This is deliberate: it removes the class of bug where a SQL
quoting habit silently produces a valid-looking Sluice query.

The token's *value* is the unescaped content; its *span* covers the delimiters.

---

## 4. Schema

The schema is data, supplied by the host application. Its canonical serialization
is JSON (§4.3); a language binding **SHOULD** also offer a native struct/class
form, and **MUST** produce identical behavior from either.

### 4.1 Fields

| Key | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | yes | Identifier used in queries. Matched case-insensitively. **MUST** match `[a-z_][a-z0-9_]*` after lowercasing. |
| `type` | enum | yes | One of `string`, `enum`, `boolean`, `number`, `uuid`, `duration`, `timestamp`. |
| `column` | string | yes¹ | Raw SQL expression the field resolves to. Trusted, host-supplied. |
| `description` | string | no | Shown in autocomplete. |
| `values` | string[] | no | Permitted values for `enum`. |
| `dynamic` | bool | no | `enum` whose `values` are supplied at request time (§4.4). |
| `operators` | string[] | no | Permitted operators. Defaults per type, see §4.5. |
| `caseInsensitive` | bool | no | Per-field override of `options.caseInsensitive`. |

¹ `column` is required unless the field supplies a custom emitter (§8.4), which is
a native-code-only feature and therefore absent from the JSON form.

### 4.2 Options

| Key | Default | Meaning |
|---|---|---|
| `caseInsensitive` | `true` | `string`/`enum` comparisons fold case (§8.2). |
| `maxLength` | `4096` | Input codepoints. Exceeding it is `input_too_long`, checked before lexing. |
| `maxDepth` | `16` | Parenthesis/expression nesting. Exceeding it is `depth_exceeded`. |
| `maxPredicates` | `64` | Predicate count. Exceeding it is `too_many_predicates`. |

The limits exist to bound work on input that arrives from a browser. They are
enforced by the parser, never by the caller.

### 4.3 Canonical JSON form

```json
{
  "name": "documents",
  "options": { "caseInsensitive": true, "maxDepth": 16 },
  "fields": [
    { "name": "state", "type": "enum", "column": "doc.state",
      "values": ["shared", "restricted"], "description": "Lifecycle state" }
  ],
  "sorts": [ { "key": "name", "sql": "doc.name" } ]
}
```

The browser-facing schema is this document with `column` and `sorts[].sql`
**removed** — the client needs field names, types, values, and descriptions to
drive autocomplete, and has no use for your table aliases. Implementations
**MUST** provide a `PublicSchema()` (or equivalent) that performs this reduction,
and the JS implementation **MUST NOT** require `column` to be present.

### 4.4 Dynamic enums

A field with `dynamic: true` has its `values` supplied per request — teams,
tenants, and other sets that come from the database rather than the source code.
Values are supplied as a `map[fieldName][]string` at compile time and **MUST NOT**
be cached inside the compiler. A dynamic field whose values were not supplied
accepts any string value and offers no completions; it does not error.

A dynamic field **MAY** also carry `values` inline. That is what `PublicSchema()`
produces — the request's values resolved into the document the browser loads — so
declaring `dynamic` alongside `values` **MUST NOT** be a `schema_invalid` error in
any implementation. Values supplied for a request win; the inline list is the
fallback. The field stays dynamic either way, so membership is still not enforced
(§7.1) and a client never rejects a value its server would accept.

The round trip that motivates this is `PublicSchema() → schema.json →
createLanguage()`. Completing it also requires tolerating the absent `column`,
which §4.3 requires of the JS implementation and not of the reference: a server
that forgot a column should hear about it at load time, not at the first query.

### 4.5 Default operators by type

| Type | Default operators |
|---|---|
| `string` | `=` `!=` `~` `!~` |
| `enum` | `=` `!=` `~` `!~` |
| `boolean` | `=` |
| `number` | `=` `!=` `<` `<=` `>` `>=` |
| `uuid` | `=` `!=` |
| `duration` | `<` `<=` `>` `>=` |
| `timestamp` | `<` `<=` `>` `>=` |

An explicit `operators` list replaces the default entirely and **MUST** be a
subset of it.

---

## 5. Syntactic grammar

```ebnf
query      = [ expr ] EOF ;
expr       = or_expr ;
or_expr    = and_expr { OR and_expr } ;
and_expr   = unary { AND unary } ;
unary      = [ NOT ] primary ;
primary    = LPAREN expr RPAREN | predicate ;
predicate  = IDENT OP value ;
value      = STRING | NUMBER | TRUE | FALSE ;
```

Precedence, tightest first: `NOT`, `AND`, `OR`. Both binary operators are
left-associative. Empty input is a valid query producing an empty result (§8.5).

Note what `value` excludes: a bare `IDENT` is **not** a value. `state = shared`
is `unexpected_token`, not a clever guess. This single rule is what removes the
injection surface that motivated the project.

`NOT` is an addition over the origin grammar. Implicit conjunction
(`a = "1" b = "2"` meaning AND) is **not** supported and is `unexpected_token`.

---

## 6. AST

The AST is the wire format between a browser and a server (§11), so its JSON
encoding is normative. Three node kinds:

```json
{ "kind": "binary", "op": "and" | "or", "left": <node>, "right": <node> }
{ "kind": "not", "expr": <node> }
{ "kind": "predicate", "field": "state", "op": "=",
  "value": { "type": "string" | "number" | "boolean", "value": <json> },
  "span": [0, 15] }
```

Rules:

- `field` is the **lowercased** field name as it appears in the schema, not as the
  user typed it.
- `op` is the canonical operator spelling from §3.1.
- `value.type` is the *literal's* type, not the field's: a `duration` field
  receives a `string` literal.
- `span` is present on `predicate` nodes and **MAY** be present on others. It is
  informational; a consumer **MUST NOT** rely on it for security decisions, and a
  decoder **MUST** accept nodes without spans.
- Object key order is irrelevant. Conformance compares decoded structures.

A decoder **MUST** reject any node it does not recognize, any predicate naming a
field absent from its own schema, and any nesting deeper than `maxDepth`, with the
same diagnostics as the parser would produce. **Decoding untrusted AST is subject
to exactly the same validation as parsing untrusted text.**

---

## 7. Resolution

For each `predicate` node:

1. Lowercase the field name and look it up. Absent → `unknown_field`, span of the
   identifier. The diagnostic **SHOULD** carry up to 4 nearest field names by
   Levenshtein distance ≤ 3, ordered by distance then alphabetically.
2. Check the operator against the field's permitted set. Absent →
   `unknown_operator_for_field`, span of the operator, listing permitted ones.
3. Coerce the literal to the field's type (§7.1). Failure →
   `invalid_value_for_field`, span of the literal.

### 7.1 Value coercion

| Field type | Accepts | Coerced to |
|---|---|---|
| `string` | STRING | string |
| `enum` | STRING | string; **MUST** be in `values` when non-dynamic and `values` non-empty, else `invalid_value_for_field` listing up to 8 permitted values |
| `boolean` | TRUE, FALSE | boolean |
| `number` | NUMBER | float64/double |
| `uuid` | STRING matching `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$` case-insensitively | lowercased string |
| `duration` | STRING matching §7.2 | **integer seconds** |
| `timestamp` | STRING in RFC 3339 | RFC 3339 string, normalized to UTC |

Enum membership is compared case-insensitively when the field is case-insensitive.

### 7.2 Duration literals

Grammar: `<count><unit>` pairs, optionally space-separated, at least one pair.
A count is a run of ASCII digits — non-negative and never fractional, because
coercion must yield whole seconds. `"1.5h"` and `"-1d"` are `invalid_duration`;
write `"90 minutes"`.
Units, case-insensitive: `s`/`sec`/`secs`/`second`/`seconds`, `m`/`min`/`mins`/`minute`/`minutes`,
`h`/`hr`/`hrs`/`hour`/`hours`, `d`/`day`/`days`, `w`/`week`/`weeks`.

`"2 days"`, `"36h"`, `"1w 2d"`, and `"90 minutes"` are all valid. A day is exactly
86400 seconds and a week exactly 7 days; there are no months or years, because
they are not fixed-length and a filter bar is the wrong place to litigate that.
Anything else is `invalid_duration`.

Coercion yields total seconds as an integer, which is what reaches SQL as a bound
parameter. **Duration strings are never emitted into SQL text.**

---

## 8. Emission

A dialect provides: a placeholder renderer, a set of type casts, a `LIKE` escape
convention, and the boolean literal spelling. Everything else is shared.

### 8.1 Structure

- `predicate` emits its comparison with no enclosing parentheses.
- `binary` emits `(` left ` ` OP ` ` right `)`, with `OP` spelled `AND` or `OR`.
- `not` emits `(NOT ` expr `)`.
- Arguments are appended in left-to-right AST traversal order, which is the order
  placeholders appear in the SQL.

Parenthesizing every binary node unconditionally is required. It makes output
determinable without precedence reasoning and makes the conformance corpus exact.

### 8.2 Comparison forms

Let `C` be the field's `column` and `P` the next placeholder.

| Field type | Operator | SQL | Argument |
|---|---|---|---|
| `string`, `enum` (case-sensitive) | `=` `!=` | `C = P` | value |
| `string`, `enum` (case-insensitive) | `=` `!=` | `LOWER(C) = P` | value lowercased |
| `string`, `enum` (case-sensitive) | `~` `!~` | `C LIKE P ESCAPE '\'` (or `NOT LIKE`) | `%` + escaped value + `%` |
| `string`, `enum` (case-insensitive) | `~` `!~` | `LOWER(C) LIKE P ESCAPE '\'` (or `NOT LIKE`) | `%` + escaped, lowercased value + `%` |
| `boolean` | `=` | `C = P` | boolean |
| `number` | all | `C < P` | number |
| `uuid` | `=` `!=` | `C = P<uuidCast>` | lowercased uuid string |
| `duration` | `<` `<=` `>` `>=` | `<durationForm>` | integer seconds |
| `timestamp` | `<` `<=` `>` `>=` | `C < P<timestampCast>` | RFC 3339 string |

Case folding uses `LOWER()` on both sides — the column in SQL, the argument in the
host language, using ASCII-only lowercasing to guarantee the two agree. Non-ASCII
case folding is deliberately not attempted: it differs between every database's
collation and every language's `toLowerCase`, and a filter bar cannot afford that
disagreement.

`LIKE` escaping replaces `\` → `\\`, `%` → `\%`, `_` → `\_` in the argument, in
that order, and the emitted SQL carries an explicit `ESCAPE '\'`. Without this,
`name ~ "%"` matches every row — a real bug in the implementation this project
generalizes.

`<durationForm>` measures age relative to now, so `edited > "2 days"` means "older
than two days":
- postgres: `EXTRACT(EPOCH FROM (NOW() - C)) > P`
- duckdb: `date_diff('second', C, current_timestamp) > P`
- sqlite: `(strftime('%s','now') - strftime('%s', C)) > P`
- mysql: `TIMESTAMPDIFF(SECOND, C, NOW()) > P`

### 8.3 Dialect table

| Dialect | Placeholder | uuid cast | timestamp cast | boolean |
|---|---|---|---|---|
| postgres | `$1`, `$2`, … | `::uuid` | `::timestamptz` | `true`/`false` |
| duckdb | `?` | `::UUID` | `::TIMESTAMPTZ` | `true`/`false` |
| sqlite | `?` | *(none)* | *(none)* | `1`/`0` |
| mysql | `?` | *(none)* | *(none)* | `1`/`0` |

### 8.4 Custom emitters

Some predicates are not a column comparison — "is this document running any
operation" may be an `EXISTS` over a JSONB column. A field **MAY** therefore carry
a host-supplied emitter instead of a `column`:

```go
func(b *sluice.Builder, op sluice.Operator, v sluice.Value) error
```

`Builder` exposes `WriteSQL(string)` for host-authored fragments and `Bind(any) string`
which appends an argument and returns its placeholder. It exposes no method that
writes a value into SQL text, so invariant 1 holds by construction even for custom
emitters. Custom emitters are native-code only and therefore cannot appear in a
JSON schema or cross a trust boundary.

### 8.5 Empty and degenerate results

Empty input compiles to `{ sql: "", args: [], fields: [] }`. The host decides what
an absent predicate means; the compiler **MUST NOT** invent `1=1`.

### 8.6 Sorting

`OrderBy(key, direction)` looks `key` up in `sorts` and emits
`ORDER BY <sql> ASC|DESC NULLS LAST`, where `<sql>` is host-supplied. An unknown
key is `unknown_sort_key`. MySQL, which lacks `NULLS LAST`, emits
`ORDER BY <sql> IS NULL, <sql> ASC|DESC`. Sort keys are never derived from input.

---

## 9. Diagnostics

A diagnostic is `{ code, message, span: [start, end], suggestions?: string[] }`.
**Codes are stable API** and are asserted by the conformance corpus; messages are
not, and **SHOULD** be phrased for display in an editor tooltip.

| Code | Raised when |
|---|---|
| `input_too_long` | input exceeds `maxLength` |
| `unterminated_string` | EOF inside a string literal |
| `invalid_escape` | unknown `\x` escape |
| `unexpected_token` | token cannot start or continue the current production |
| `unexpected_eof` | input ends mid-production |
| `unbalanced_paren` | unmatched `(` or `)` |
| `depth_exceeded` | nesting exceeds `maxDepth` |
| `too_many_predicates` | predicate count exceeds `maxPredicates` |
| `unknown_field` | identifier is not a schema field |
| `unknown_operator_for_field` | operator not permitted for that field |
| `invalid_value_for_field` | literal cannot coerce to the field's type |
| `invalid_duration` | duration literal fails §7.2 |
| `unknown_sort_key` | `OrderBy` key absent from `sorts` |
| `schema_invalid` | schema fails §4 validation at load time |

`Validate` **MUST** return *all* independent diagnostics rather than only the
first, so an editor can underline every problem in one pass. Recovery rule: after
a failed predicate, skip tokens until the next `AND`, `OR`, or `)` and resume.
`Compile` returns only the first diagnostic and no SQL.

---

## 10. Suggestions

`Suggest(input, cursor) → []Suggestion`, where `cursor` is a codepoint offset and
`Suggestion` is `{ text, kind, detail?, replaceSpan }`. `kind` is one of `field`,
`operator`, `value`, `keyword`, `expression`.

The algorithm is a state walk over the token stream, not a parse:

1. Compute the *prefix*: the maximal run of characters ending at `cursor` that
   contains no whitespace and no parenthesis, with a leading `"` stripped. Defining
   it lexically rather than from the token stream is deliberate — `web-1` lexes as
   two tokens, and the user typing it means one thing.
2. Determine the expected token class from the preceding tokens: start of input,
   after `AND`/`OR`/`NOT`/`(` → **field**; after a field → **operator**; after an
   operator → **value**; after a complete predicate or `)` → **keyword** (`AND`,
   `OR`) plus `)` when parentheses are open.
3. Emit candidates of that class, filtered to those containing the prefix
   case-insensitively. **Field** candidates are ordered exact match, then prefix
   match, then substring match, alphabetically within each group. **Operator** and
   **value** candidates preserve their declared order instead — a schema author
   who writes `["shared", "restricted"]` ordered them for a reason, and `=` should
   never sort below `!=`. `replaceSpan` covers the prefix, including the stripped
   opening quote when there was one.
4. Value candidates come from the field's `values` (static or dynamic) for `enum`,
   from `true`/`false` for `boolean`, and are empty for other types.
5. **Bare-value fallback.** When a field is expected but the prefix matches no
   field name, emit `expression` suggestions that wrap the prefix into a whole
   predicate against host-nominated fallback fields — `name = "web-1"` and
   `name ~ "web-1"` for prefix `web-1`. Fallback fields are configured as
   `options.fallbackFields`; when the prefix is a UUID, `uuid`-typed fields take
   precedence. This is what lets someone paste an identifier into an empty filter
   bar and get somewhere.

Suggestions **MUST** be available for input that does not parse. An editor asks
for completions precisely when the query is half-written.

---

## 11. Conformance adapter protocol

Every implementation **MUST** ship an executable that speaks this protocol. It is
how the language-agnostic corpus is run, and it is the definition of "done" for a
port.

**Transport:** JSON Lines on stdin/stdout, one request per line, one response per
line, in order. No banner output; diagnostics for humans go to stderr. Exit 0 when
stdin closes.

**Request:**

```json
{ "id": "case-1", "op": "lex" | "parse" | "compile" | "validate" | "suggest" | "schema",
  "schema": { … } | "documents",
  "dialect": "postgres",
  "dynamic": { "team": ["DESIGN-A"] },
  "input": "state = \"shared\"",
  "cursor": 8,
  "ast": { … } }
```

`schema` is either an inline schema object or the basename of a file in
`conformance/schemas/`. `ast` is present instead of `input` when the case exercises
AST decoding (§6).

**Response:**

```json
{ "id": "case-1",
  "tokens": [ { "kind": "IDENT", "value": "state", "span": [0, 5] } ],
  "ast": { … },
  "sql": "…", "args": [ … ], "fields": [ … ],
  "suggestions": [ { "text": "shared", "kind": "value", "replaceSpan": [8, 8] } ],
  "diagnostics": [ { "code": "unknown_field", "span": [0, 4] } ] }
```

Only the keys relevant to `op` need be present. A response **MUST NOT** omit
`diagnostics` when diagnostics were produced, and **MUST NOT** include `sql` when
they were. `token.kind` uses the spellings in §3.1; the trailing `EOF` token is
omitted from `tokens`.

Comparison rules used by the runner: `sql` compares as an exact string; `args`,
`fields`, `ast`, `tokens`, and `suggestions` compare as decoded structures in
order; `diagnostics` compare on `code` and `span` only, never on `message`. A
corpus case therefore cannot assert diagnostic wording, which is free to improve.

Register a new implementation by adding it to `conformance/adapters.json`.
See [`conformance/README.md`](conformance/README.md) for the runner.

---

## 12. Trust boundaries

Sluice is designed to let the browser compile queries, so the trust question is
unavoidable and answered here rather than left to each host.

**The security property that makes this tractable:** because values leave only as
bound parameters, the SQL *shape* is a pure function of the schema and the AST's
structure. It is drawn from a finite set enumerable from the schema alone. Shape
is verifiable; arbitrary text is not.

Three deployment modes, in descending order of how much you should like them:

**Mode A — local execution.** The browser compiles and executes against a
client-side database (DuckDB-WASM, PGlite, sql.js). No trust boundary is crossed,
so there is nothing to defend. This is the "no backend" case in its pure form and
it is the one to reach for first.

**Mode B — AST transport.** The browser sends the **AST**, not SQL. The server
decodes it against *its own* schema (§6) and compiles. A hostile client can
express only what the server's schema permits; the worst available outcome is an
expensive-but-legal filter, which `maxPredicates` and your statement timeout
already bound. **This is the recommended mode whenever a database sits behind a
server.** Sending the raw source string instead is equally safe and simpler to
log; send the AST when you want the client's parse errors to be authoritative.

**Mode C — SQL with proof.** The browser sends `{ source, sql, args }`. The server
recompiles `source` under its own schema and compares the result to the received
`sql` with a constant-time comparison, rejecting any mismatch. Be clear about what
this buys: the received SQL is **never trusted** — it is an assertion, not an
input — so Mode C is Mode B plus a version-skew detector. It is specified because
people will build it anyway, and they should build the version that cannot be
turned into an injection.

**There is no Mode D.** Executing client-supplied SQL after inspecting it —
allowlisting keywords, rejecting `;`, checking for `UNION` — is not a supported
pattern, and an implementation **MUST NOT** ship a helper that appears to bless
it. If a proposal's safety depends on parsing SQL you received over the network,
it is out of scope for this project.

Independent of mode, hosts remain responsible for row-level authorization,
statement timeouts, and read-only credentials. Sluice constrains *what predicate*
runs, not *what data the caller may see*.

---

## 13. Repository conventions

- **Reference implementation is Go**, at the module root. It is normative when
  this document and the corpus disagree with each other, but a bug in it is still
  a bug — fix the implementation and add the case, do not amend the spec to match.
- **Every behavior change lands with a corpus case in the same commit**, and the
  corpus is language-agnostic: never assert Go or JS specifics in it.
- **The JS core has zero runtime dependencies** and no build step for consumers;
  it ships as ESM. Editor bindings are separate packages so that `@sluice/core`
  never imports Monaco or CodeMirror.
- **No `unsafe`, no reflection-based SQL building, no `fmt.Sprintf` with a
  value argument in an emitter.** A grep for `Sprintf` in `dialect/` should show
  only schema-supplied fragments and placeholders.
- **Spec changes require a corresponding change to this file in the same PR.**
  `AGENTS.md` is the artifact; the code is its projection.

### Porting checklist

A new implementation is done when, in order:

1. Lexer passes `conformance/corpus/001-lex.json`.
2. Parser produces matching ASTs for `002-parse.json`.
3. Resolver produces matching diagnostic codes and spans for `003-diagnostics.json`.
4. Emitter produces byte-identical SQL and args for `004-compile-*.json` for every
   dialect it claims.
5. Suggester matches `005-suggest.json`.
6. The adapter (§11) is registered in `conformance/adapters.json` and green in CI.
7. §1's invariants are re-read and honestly checked against the diff. In
   particular: search the implementation for every site where a value could reach
   a SQL string, and confirm each one goes through the placeholder path.
