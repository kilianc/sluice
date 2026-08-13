# Sluice

A configurable filter query language that compiles to SQL.

Declare your fields once. Get a parser, a SQL compiler that binds every value as a
parameter, and an autocompleting editor — in the browser, on the server, or both.

```
phase = "in-use" AND rack ~ "ash1" AND NOT online = true
```

```go
c, _ := sluice.New(schema, postgres.Dialect)
res, _ := c.Compile(`phase = "in-use" AND rack ~ "ash1"`)

res.SQL    // (LOWER(inv.phase) = $1 AND UPPER(loc.name) LIKE $2 ESCAPE '\')
res.Args   // []any{"in-use", "%ASH1%"}
res.Fields // []string{"phase", "rack"} — so you can prune joins
```

The same schema drives the browser:

```js
import { createLanguage } from '@sluice/core'

const lang = createLanguage(schema)
lang.suggest('phase = ', 8) // → in-use, not-in-use, maintenance
lang.validate('phse = "x"') // → unknown_field at 0..4, did you mean "phase"?
lang.compile('phase = "in-use"', postgres) // → { sql, args, ast }
```

## Why

Every internal tool eventually grows a filter bar, and every filter bar eventually
grows a bad little query language stitched together with string concatenation.
Sluice is that language, done once: a real parser, a real AST, parameter binding
by construction, and typed autocomplete derived from the same field declarations
the compiler uses.

It is deliberately not a SQL replacement. It compiles a **predicate** and an
**ORDER BY**. Joins, projections, and aggregation stay your application's business.
That constraint is what keeps it small enough to be worth depending on.

## Guarantees

1. **No user-supplied text ever reaches SQL.** Values leave only as bound
   parameters. The SQL *shape* is a pure function of your schema and the query's
   structure, drawn from a finite set your schema defines.
2. **No passthrough.** A token the schema does not recognize is an error with a
   source position, never something copied into the output.
3. **One grammar, every runtime.** Implementations are validated against a
   language-agnostic [conformance suite](conformance/), so ports cannot drift.

See [`AGENTS.md`](AGENTS.md) for the normative specification — it is written so
that a competent implementer (human or agent) can port Sluice to a new language
without reading the reference source.

## Status

Pre-release. See [`PLAN.md`](PLAN.md) for the roadmap and design decisions.

## License

MIT
