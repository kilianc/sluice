# Sluice

A configurable filter query language that compiles to SQL.

Declare your fields once. Get a parser, a SQL compiler that binds every value as a
parameter, and an autocompleting editor — in the browser, on the server, or both.

```
phase = "in-use" AND rack ~ "ash1" AND NOT online = true
```

## Install

```bash
go get github.com/kilianc/sluice
```

```bash
npm install @sluice/core
```

## Compile a query

```go
package main

import (
	"fmt"
	"log"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

const schemaJSON = `{
  "name": "machines",
  "fields": [
    { "name": "name",  "type": "string", "column": "inv.name" },
    { "name": "phase", "type": "enum",   "column": "inv.phase",
      "values": ["in-use", "not-in-use"] },
    { "name": "rack",  "type": "enum",   "column": "loc.name", "dynamic": true }
  ],
  "sorts": [ { "key": "name", "sql": "inv.name" } ]
}`

func main() {
	schema, err := sluice.LoadSchema([]byte(schemaJSON))
	if err != nil {
		log.Fatal(err)
	}
	c, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		log.Fatal(err)
	}

	res, err := c.Compile(`phase = "in-use" AND rack ~ "ash1"`)
	if err != nil {
		log.Fatal(err) // a diagnostic, with a position; there is no SQL to fall back on
	}

	fmt.Println(res.SQL)    // (LOWER(inv.phase) = $1 AND LOWER(loc.name) LIKE $2 ESCAPE '\')
	fmt.Println(res.Args)   // [in-use %ash1%]
	fmt.Println(res.Fields) // [phase rack] — so you can prune joins
}
```

Then put it in your own query, where the constraints you already apply stay
yours:

```go
query := "SELECT inv.id, inv.name FROM machine inv WHERE inv.tenant_id = $1"
// … append " AND (" + res.SQL + ")" with res.Args, and c.OrderBy("name", sluice.Desc)
```

The same schema drives the browser. The server serves it with `PublicSchema()`,
which strips the column SQL:

```js
import { createLanguage } from '@sluice/core'
import { duckdb } from '@sluice/core/dialects'

const lang = createLanguage(await (await fetch('/sluice/schema.json')).json())

lang.suggest('phase = ', 8) // → in-use, not-in-use
lang.validate('phse = "x"') // → unknown_field at 0..4, did you mean "phase"?
lang.parse('phase = "in-use"').ast   // send this to your server, or…
lang.compile('phase = "in-use"', duckdb) // …compile locally, when the schema you
                                         // serve publishes its columns — which is
                                         // the case worth wanting when the database
                                         // is in the browser too
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

## Documentation

| | |
|---|---|
| [language.md](docs/language.md) | the query language, in full |
| [schema.md](docs/schema.md) | declaring fields, dynamic enums, custom emitters |
| [dialects.md](docs/dialects.md) | what a dialect controls, and writing one |
| [editor.md](docs/editor.md) | completions and diagnostics in a filter bar |
| [security.md](docs/security.md) | the invariants, and the three ways to deploy client-side compilation |
| [porting.md](docs/porting.md) | implementing Sluice in another language |

[`AGENTS.md`](AGENTS.md) is the normative specification — written so that a
competent implementer, human or agent, can port Sluice without reading the
reference source. [`PLAN.md`](PLAN.md) is the roadmap and the decision log.

## Status

Pre-release, working toward v0.1.0. The Go reference and `@sluice/core` both pass
the whole corpus.

```bash
make test          # Go, then the JS package
make conformance   # the corpus against every implementation
```

Node is not required on your machine: the JS adapter and tests run in the pinned
image from [`tools/Dockerfile`](tools/Dockerfile) when no runtime is available.
[`CONTRIBUTING.md`](CONTRIBUTING.md) has the versioning policy and the rule that
keeps the implementations honest.

## License

MIT
