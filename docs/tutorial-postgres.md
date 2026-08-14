# Postgres in twenty minutes

By the end of this you will have a filter bar over a Postgres database you
already have, with autocomplete that knows your fields, error underlines that
point at the character that is wrong, and SQL where every value someone typed
arrives as a bound parameter.

The database here is an issue tracker, because that is what filter bars are for.
It has the things a real schema has and a toy one does not: joins, an enum type,
a JSONB column, a computed value, and a count that lives in another table. If you
are following along on your own database, the steps map one-to-one — only the
field list changes.

Everything below is in [`examples/tickets/`](../examples/tickets/), and every
output on this page was produced by running it.

---

## Before you start

- Go 1.23 or newer.
- A Postgres. If you do not have one to hand:

```bash
docker run -d --name sluice-tut \
  -e POSTGRES_PASSWORD=sluice -e POSTGRES_DB=tickets \
  -p 55432:5432 postgres:16-alpine
```

- The example, and a database to point it at:

```bash
git clone https://github.com/kilianc/sluice
cd sluice/examples/tickets

export DATABASE_URL="postgres://postgres:sluice@localhost:55432/tickets"
psql "$DATABASE_URL" -f schema.sql
```

## Step 1 — the database

Four tables, and nothing in them knows that Sluice exists:

```sql
CREATE TYPE ticket_status AS ENUM ('open', 'in_progress', 'blocked', 'closed');

CREATE TABLE ticket (
  id          uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
  title       text          NOT NULL,
  status      ticket_status NOT NULL,
  priority    int           NOT NULL CHECK (priority BETWEEN 1 AND 5),
  assignee_id int           REFERENCES app_user (id),
  team_id     int           REFERENCES team (id),
  created_at  timestamptz   NOT NULL DEFAULT now(),
  due_at      timestamptz,
  meta        jsonb         NOT NULL DEFAULT '{}'
);
```

Plus `app_user`, `team` and `comment`. The full file is
[`schema.sql`](../examples/tickets/schema.sql), with ten tickets in it.

## Step 2 — describe it to Sluice

This is the only step that requires thought, and it is the whole configuration.
A field is a name someone types, a type, and the SQL it stands for:

```json
{
  "name": "tickets",
  "options": { "fallbackFields": ["title"] },
  "fields": [
    { "name": "title",  "type": "string", "column": "t.title" },
    { "name": "status", "type": "enum",   "column": "t.status::text",
      "values": ["open", "in_progress", "blocked", "closed"] },
    { "name": "priority", "type": "number", "column": "t.priority" },

    { "name": "assignee", "type": "string",   "column": "u.display_name" },
    { "name": "team",     "type": "enum",     "column": "tm.name", "dynamic": true },
    { "name": "age",      "type": "duration", "column": "t.created_at" },
    { "name": "overdue",  "type": "boolean",  "column": "(t.due_at < now())" },

    { "name": "comments", "type": "number",
      "column": "(SELECT count(*) FROM comment c WHERE c.ticket_id = t.id)" },

    { "name": "source", "type": "string", "column": "(t.meta ->> 'source')" }
  ],
  "sorts": [
    { "key": "created",  "sql": "t.created_at" },
    { "key": "priority", "sql": "t.priority" }
  ]
}
```

Read down the `column` values: a plain column, a cast, a joined column, a boolean
expression, a correlated subquery, a JSONB path. **A column is any SQL expression
you are willing to run**, which is what lets one word stand for something nobody
wants to type twice. `comments > 2` is a subquery; the person filtering does not
need to know that, and does not need to be trusted with it either.

Four things worth pointing out.

**`t.status::text`, not `t.status`.** Postgres enum types have no `lower()`, so
folding case on the raw column fails with `function lower(ticket_status) does not
exist`. The cast belongs in the schema, where you write it once.

**`age` is a `duration` over a timestamp column.** The column is
`t.created_at`, and `age > "30 days"` becomes
`EXTRACT(EPOCH FROM (NOW() - t.created_at)) > $1` with `2592000` bound. Nobody
types an interval, and no interval string reaches SQL.

**`team` is `dynamic`.** Its values are not in the source; they come from the
database on each request (step 6).

**`fallbackFields`** is what someone gets when they paste a word that is not a
field name: the bar offers `title = "…"` and `title ~ "…"` instead of a shrug.

## Step 3 — compile a filter before writing any code

The CLI is the fastest way to see whether your field list says what you meant:

```bash
go run github.com/kilianc/sluice/cmd/sluice compile \
  -schema schema.json 'status = "open" AND overdue = true'
```

```
(LOWER(t.status::text) = $1 AND (t.due_at < now()) = $2)
["open",true]
```

That is a `WHERE` fragment and its arguments — no table names of yours, no
values inlined. Try a few more:

```bash
… compile -schema schema.json 'assignee ~ "kim" AND comments > 2'
```
```
(LOWER(u.display_name) LIKE $1 ESCAPE '\' AND (SELECT count(*) FROM comment c WHERE c.ticket_id = t.id) > $2)
["%kim%",2]
```

```bash
… compile -schema schema.json 'source = "pagerduty" AND priority <= 2'
```
```
(LOWER((t.meta ->> 'source')) = $1 AND t.priority <= $2)
["pagerduty",2]
```

Flags go before the query — Go's flag package stops at the first positional
argument, and the CLI will tell you so rather than folding a stray `-dynamic`
into your filter.

## Step 4 — compile it in your application

```go
//go:embed schema.json
var schemaJSON []byte

schema, err := sluice.LoadSchema(schemaJSON)
if err != nil {
    log.Fatalf("schema: %v", err) // every problem at once, at startup
}
compiler, err := sluice.New(schema, postgres.Dialect)
```

`Compiler` is immutable and safe for concurrent use, so build it once at
startup. Then, per request:

```go
res, err := compiler.Compile(r.URL.Query().Get("filter"))
if err != nil {
    var e *sluice.Error
    if errors.As(err, &e) {
        // A diagnostic with a position. Hand it to the client as-is.
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": e.Diagnostic})
        return
    }
}

query := base                       // your SELECT and joins
if res.SQL != "" {
    query += "\nWHERE (" + res.SQL + ")"
}
order, err := compiler.OrderBy(sortKey(r), direction(r))
query += "\n" + order + "\nLIMIT 100"

rows, err := pool.Query(r.Context(), query, res.Args...)
```

Note the parentheses around `res.SQL`. A compiled predicate is one expression,
but wrapping it makes that independent of whatever your surrounding `WHERE`
does — and it is how you `AND` it into the scoping you already apply:

```go
query := "SELECT … FROM ticket t WHERE t.tenant_id = $1"
// … then AND the compiled predicate in, with its args appended after yours
```

Sluice constrains *which predicate* runs. It does not know who is asking; row
level authorization, statement timeouts and read-only credentials stay yours.

## Step 5 — publish the schema to the browser

The browser needs field names, types, values and descriptions. It does not need
your table aliases, and should not have them:

```go
http.HandleFunc("GET /sluice/schema.json", func(w http.ResponseWriter, r *http.Request) {
    teams, _ := listTeams(r.Context())
    writeJSON(w, http.StatusOK, compiler.PublicSchema(map[string][]string{"team": teams}))
})
```

`PublicSchema` strips every `column` and every `sorts[].sql`. Check it yourself:

```bash
curl -s localhost:8080/sluice/schema.json | grep -c '"column"'
```
```
0
```

## Step 6 — dynamic values

`team` was declared `dynamic`, so its values come from the database per request:

```go
res, err := compiler.
    WithDynamic(map[string][]string{"team": teams}).
    Compile(filter)
```

`WithDynamic` returns a lightweight view; the compiler itself never caches them.
The published schema carries them resolved, so the bar completes real team names:

```json
{ "name": "team", "type": "enum", "dynamic": true,
  "values": ["billing", "growth", "platform"] }
```

A dynamic field whose values were not supplied accepts any string and offers no
completions — the server is the authority on what exists, and a stale list in a
browser tab should not reject a team that does.

## Step 7 — the filter bar

Two calls. One to build the language from the schema you just published, one to
give it to the editor:

```js
import { createLanguage } from '@sluice/core'
import { register } from '@sluice/monaco'

const language = createLanguage(await (await fetch('/sluice/schema.json')).json())
register(monaco, { language })

monaco.editor.create(element, { value: 'status = "open"', language: 'sluice' })
```

That is completions, error underlines, highlighting and hovers. The bar knows
`status` is an enum with four values because the schema said so — the same
schema the server compiles with, which is why the autocomplete cannot suggest
something that will not compile.

There is nothing Monaco-specific about the language: `@sluice/codemirror` is the
same three ideas for CodeMirror 6, and [editor.md](editor.md) covers wiring
anything else.

## Step 8 — run it

```bash
go run .        # http://localhost:8080, or set PORT
```

Type `status = "open" AND overdue = true`. You should get two tickets, and the
page shows you what it compiled to:

```
sql    (LOWER(t.status::text) = $1 AND (t.due_at < now()) = $2)
args   ["open", true]
fields ["status", "overdue"]
```

Now try to break it.

```
stat = "open"
```
```json
{"code":"unknown_field","message":"unknown field stat; did you mean status, team?",
 "span":[0,4],"suggestions":["status","team"]}
```

The span is what the bar underlines: codepoints 0 to 4, the word `stat`. The
suggestions are near misses by edit distance, which is where a quick-fix action
comes from if you want one.

```
title = "x"; DROP TABLE ticket
```
```json
{"code":"unexpected_token","message":"unexpected character ';'","span":[11,12]}
```

400, no SQL, and the table is still there. Not because a semicolon was
blocklisted — because there is no branch anywhere that copies an unrecognized
token into the output. `1=1` is a syntax error for the same reason, and
`title ~ "%"` looks for a literal percent sign rather than matching every row.

## What to reach for next

**`res.Fields`** names the fields a query touched, in traversal order:

```go
if slices.Contains(res.Fields, "assignee") {
    query += " LEFT JOIN app_user u ON u.id = t.assignee_id"
}
```

The example always joins, because its page shows the assignee. When a join
exists *only* so that a field can be filtered on — a big aggregate, a table in
another schema — this is how you skip it on the queries that do not mention it.

**Custom emitters**, for a predicate that is not a comparison at all. "Is this
ticket waiting on someone" might be an `EXISTS` over a JSONB column; a field can
carry Go code instead of a `column`, and it still cannot interpolate a value.
See [schema.md](schema.md#custom-emitters).

**Another database.** The same schema compiles for DuckDB, SQLite and MySQL —
[dialects.md](dialects.md) covers what changes, which is less than you would
expect.

**Compiling in the browser.** The bar can produce the SQL itself, which is worth
doing when the database is also in the browser and is worth understanding before
you do it anywhere else: [security.md](security.md).

## Tearing down

```bash
docker rm -f sluice-tut
```
