# Sluice

A configurable filter query language that compiles to SQL.

Declare your fields once. Get a parser, a SQL compiler that binds every value as a
parameter, and an autocompleting editor — in the browser, on the server, or both.

```
state = "shared" AND team ~ "desi" AND NOT active = true
```

## Install

```bash
go get github.com/kilianc/sluice
```

```bash
npm install @sluice/core          # the language
npm install @sluice/monaco        # optional: the Monaco binding
npm install @sluice/codemirror    # …or the CodeMirror 6 one
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
  "name": "documents",
  "fields": [
    { "name": "name",  "type": "string", "column": "doc.name" },
    { "name": "state", "type": "enum",   "column": "doc.state",
      "values": ["shared", "restricted"] },
    { "name": "team",  "type": "enum",   "column": "grp.name", "dynamic": true }
  ],
  "sorts": [ { "key": "name", "sql": "doc.name" } ]
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

	res, err := c.Compile(`state = "shared" AND team ~ "desi"`)
	if err != nil {
		log.Fatal(err) // a diagnostic, with a position; there is no SQL to fall back on
	}

	fmt.Println(res.SQL)    // (LOWER(doc.state) = $1 AND LOWER(grp.name) LIKE $2 ESCAPE '\')
	fmt.Println(res.Args)   // [shared %desi%]
	fmt.Println(res.Fields) // [state team] — so you can prune joins
}
```

Then put it in your own query, where the constraints you already apply stay
yours:

```go
query := "SELECT doc.id, doc.name FROM document doc WHERE doc.tenant_id = $1"
// … append " AND (" + res.SQL + ")" with res.Args, and c.OrderBy("name", sluice.Desc)
```

The same schema drives the browser. The server serves it with `PublicSchema()`,
which strips the column SQL:

```js
import { createLanguage } from '@sluice/core'
import { duckdb } from '@sluice/core/dialects'

const lang = createLanguage(await (await fetch('/sluice/schema.json')).json())

lang.suggest('state = ', 8) // → shared, restricted
lang.validate('stat = "x"') // → unknown_field at 0..4, did you mean "state"?
lang.parse('state = "shared"').ast   // send this to your server, or…
lang.compile('state = "shared"', duckdb) // …compile locally, when the schema you
                                         // serve publishes its columns — which is
                                         // the case worth wanting when the database
                                         // is in the browser too
```

Wiring that to an editor is one call — completions, error underlines,
highlighting and hovers, all from the same schema:

```js
import { register } from '@sluice/monaco'

register(monaco, { language: lang })
```

There is a [playground](playground/) that does exactly this and executes the
result against a Postgres compiled into the page, so nothing leaves the browser.
Run it with `make playground`.

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
| [playground/](playground/) | compile and run a query with no server at all |
| [security.md](docs/security.md) | the invariants, and the three ways to deploy client-side compilation |
| [porting.md](docs/porting.md) | implementing Sluice in another language |

[`AGENTS.md`](AGENTS.md) is the normative specification — written so that a
competent implementer, human or agent, can port Sluice without reading the
reference source. [`PLAN.md`](PLAN.md) is the roadmap and the decision log.

## Status

Pre-release, working toward v0.2.0. Two implementations — the Go reference and
`@sluice/core` — pass the whole corpus, in four dialects.

```bash
make test          # Go, then every JS package
make conformance   # the corpus against every implementation
make playground    # the demo, at http://localhost:8901/playground/
```

Node is not required on your host: the JS adapter and tests run in the pinned
image from [`tools/Dockerfile`](tools/Dockerfile) when no runtime is available.
[`CONTRIBUTING.md`](CONTRIBUTING.md) has the versioning policy and the rule that
keeps the implementations honest.

## License

MIT
