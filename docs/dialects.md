# Dialects

A dialect is everything a database changes about emission, and nothing else. The
structure of the output — which predicates appear, how they are parenthesized,
what order the arguments are in, whether case is folded — is dialect-independent,
so the same query produces the same shape everywhere and only the spelling moves.

| Dialect | Placeholder | uuid cast | timestamp cast | Status |
|---|---|---|---|---|
| `postgres` | `$1`, `$2`, … | `::uuid` | `::timestamptz` | shipped |
| `duckdb` | `?` | `::UUID` | `::TIMESTAMPTZ` | shipped |
| `sqlite` | `?` | *(none)* | *(none)* | planned |
| `mysql` | `?` | *(none)* | *(none)* | planned |

```go
import "github.com/kilianc/sluice/dialect/postgres"

c, err := sluice.New(schema, postgres.Dialect)
```

```js
import { postgres, duckdb } from '@sluice/core/dialects'

lang.compile(input, duckdb)
```

The same query, twice:

```
id = "3F2504E0-4F89-11D3-9A0C-0305E82C3301" AND os_age > "2 days"
```

```sql
-- postgres
(inv.id = $1::uuid AND EXTRACT(EPOCH FROM (NOW() - img.created_at)) > $2)
-- duckdb
(inv.id = ?::UUID AND date_diff('second', img.created_at, current_timestamp) > ?)
```

Both bind `["3f2504e0-4f89-11d3-9a0c-0305e82c3301", 172800]`. Note the duration:
it reaches SQL as an integer number of seconds, never as an interval string.

## Using the result

`Compile` hands back a `WHERE` fragment and its arguments, in placeholder order.
Drop it into your own SQL:

```go
res, err := c.Compile(input)
if err != nil {
    return err // a diagnostic; there is no SQL to fall back on
}

query := "SELECT inv.id, inv.name FROM machine inv"
if res.SQL != "" {
    query += " WHERE " + res.SQL
}
order, _ := c.OrderBy(sortKey, sluice.Desc)
rows, err := db.Query(ctx, query+" "+order, res.Args...)
```

Empty input compiles to an empty string, not to `1=1`. What an absent predicate
means is your decision, and inventing a tautology takes it away from you.

`res.Fields` names the fields the query touched, in traversal order and
deduplicated, which is enough to prune joins:

```go
if slices.Contains(res.Fields, "rack") {
    query += " JOIN loc ON loc.id = inv.loc_id"
}
```

## Sorting

`ORDER BY <expr> ASC|DESC NULLS LAST`, from a schema-named key. MySQL has no
`NULLS LAST`, so its dialect emits `ORDER BY <expr> IS NULL, <expr> ASC|DESC`
instead — the one place a dialect changes more than spelling.

## Writing one

Implement `sluice.Dialect`:

```go
type Dialect interface {
    Name() string
    Placeholder(n int) string      // 1-based
    UUIDCast() string              // "::uuid", or ""
    TimestampCast() string
    BoolArg(b bool) any            // for databases with no native boolean
    LikeEscapeClause() string      // " ESCAPE '\\'"
    AgeSeconds(column string) string
    OrderBy(sql string, desc bool) string
}
```

`AgeSeconds` renders the age of a timestamp column in seconds, which is what
makes `os_age > "2 days"` mean "older than two days":

| Dialect | Expression |
|---|---|
| postgres | `EXTRACT(EPOCH FROM (NOW() - C))` |
| duckdb | `date_diff('second', C, current_timestamp)` |
| sqlite | `(strftime('%s','now') - strftime('%s', C))` |
| mysql | `TIMESTAMPDIFF(SECOND, C, NOW())` |

One rule governs the whole interface: **a dialect contributes SQL text only from
its own constants.** It never receives an input string and has no way to reach
one. If you find yourself wanting a value inside a dialect method, the thing you
want is `Bind` in a [custom emitter](schema.md#custom-emitters).

A new dialect needs a corpus file — copy `conformance/corpus/004-compile-duckdb.json`,
which exists precisely to show what a dialect is allowed to change — and an entry
in the adapter registry's `dialects` list. See [porting.md](porting.md).
