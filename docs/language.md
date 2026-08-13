# The language

Sluice queries are predicates: comparisons joined by `AND`, `OR` and `NOT`, with
parentheses where you want them.

```
phase = "in-use" AND NOT online = true
(rack ~ "ash1" OR rack ~ "chi1") AND cores >= 32
os_age > "2 days"
```

The field names are yours — they come from the schema the application declares,
not from Sluice — so the language a user sees is exactly as large as the fields
you publish. This page is the whole grammar; it is short on purpose, and a
tooltip can hold most of it.

## Values

| Kind | Written | Notes |
|---|---|---|
| string | `"web-1"` | double quotes only |
| number | `32`, `-1.5` | no quotes |
| boolean | `true`, `false` | case-insensitive |

Escapes inside a string are `\"`, `\\`, `\n`, `\t` and `\r`. Anything else after
a backslash is an error.

Two rules surprise people, and both are deliberate:

**Single quotes are not string delimiters.** `name = 'web-1'` is an error. A SQL
quoting habit should not silently produce a valid-looking Sluice query.

**A bare word is not a value.** `phase = in-use` is an error; write
`phase = "in-use"`. This single rule is what removes the injection surface — an
unrecognized token has nowhere to go except a diagnostic.

## Operators

Which operators a field accepts depends on its type. The schema may narrow this
list further.

| Field type | Operators | Meaning |
|---|---|---|
| `string`, `enum` | `=` `!=` `~` `!~` | `~` is "contains", `!~` "does not contain" |
| `boolean` | `=` | |
| `number` | `=` `!=` `<` `<=` `>` `>=` | |
| `uuid` | `=` `!=` | |
| `duration` | `<` `<=` `>` `>=` | measured as age, see below |
| `timestamp` | `<` `<=` `>` `>=` | RFC 3339 |

Operators do not need spaces around them: `phase="in-use"` and `phase = "in-use"`
lex identically.

`~` matches a substring anywhere in the value. Wildcards are not part of the
language: `name ~ "%"` looks for a literal percent sign, because `%` and `_` are
escaped before the pattern reaches SQL. There is no way to write a `LIKE`
pattern by hand, which is the point.

## Precedence

Tightest first: `NOT`, then `AND`, then `OR`. Both binary operators are
left-associative, so

```
phase = "in-use" OR online = true AND cores > 8
```

means `phase = "in-use" OR (online = true AND cores > 8)`. Parenthesize when you
mean otherwise. Writing two predicates side by side is not an implicit `AND`; it
is an error.

An empty query is valid and matches everything — or rather, it compiles to
nothing at all and lets the application decide what an absent filter means.

## Durations

A `duration` field compares against an age, so `os_age > "2 days"` means "older
than two days".

```
"2 days"    "36h"    "1w 2d"    "90 minutes"
```

The grammar is one or more `<count><unit>` pairs, optionally space-separated.
Units, case-insensitive:

| Unit | Spellings |
|---|---|
| second | `s` `sec` `secs` `second` `seconds` |
| minute | `m` `min` `mins` `minute` `minutes` |
| hour | `h` `hr` `hrs` `hour` `hours` |
| day | `d` `day` `days` |
| week | `w` `week` `weeks` |

A day is exactly 86400 seconds and a week exactly 7 days. There are no months or
years, because they are not fixed-length and a filter bar is the wrong place to
litigate that. Counts are whole numbers: `"1.5h"` is an error, `"90 minutes"` is
not.

## Case

String and enum comparisons fold case by default, so `name = "WEB-1"` and
`name = "web-1"` find the same rows. Folding is ASCII-only: it is the one rule
under which the host language and every database agree, and a filter bar cannot
afford a disagreement there. A schema can turn folding off per field or
globally.

## Timestamps and UUIDs

Timestamps are RFC 3339 and normalized to UTC:
`seen > "2026-08-13T09:30:00+02:00"`. UUIDs are the canonical 8-4-4-4-12 form,
matched case-insensitively.

## Limits

A query may be at most 4096 codepoints, nest at most 16 levels deep and contain
at most 64 predicates. A schema can change these. They exist because queries
arrive from browsers.

## What the language does not do

Sluice compiles a **predicate** and an **ORDER BY**. Joins, projections,
aggregation and subqueries stay in your application's SQL, which drops a Sluice
predicate into its `WHERE`. That constraint is the product: it is what keeps the
language teachable in a tooltip.

Deliberately not in v0.1, though each is defensible: value lists
(`phase = ("a", "b")`), relative timestamps (`> "now-7d"`), free-text search
across several fields at once, and `ORDER BY` expressions composed in the
language rather than named in the schema.

## Errors

Every error carries a code and a source position, so an editor can underline the
exact span. Codes are stable; the messages are not, and are meant to be shown to
a person.

| Code | What happened |
|---|---|
| `unexpected_token` | something that cannot appear here — a bare word in value position, a stray `;`, a single quote |
| `unexpected_eof` | the query stops mid-predicate |
| `unbalanced_paren` | a `(` with no `)`, or the reverse |
| `unterminated_string` | a `"` with no closing `"` |
| `invalid_escape` | an unknown `\x` inside a string |
| `unknown_field` | not a field in this schema; the diagnostic suggests near misses |
| `unknown_operator_for_field` | that field does not accept that operator |
| `invalid_value_for_field` | the value is not of the field's type, or outside its permitted values |
| `invalid_duration` | not a duration per the table above |
| `input_too_long`, `depth_exceeded`, `too_many_predicates` | a limit |

`Validate` returns all of them at once. `Compile` returns the first and no SQL —
a query that produced a diagnostic never reaches your database.
