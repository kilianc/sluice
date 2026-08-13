# Schemas

The schema is the whole configuration. One declaration produces the parser, the
SQL compiler and the editor's autocomplete, which is the part of this project
that is actually differentiated — filter-string-to-SQL libraries are common.

It is also the only place identifiers come from. Column SQL, table aliases, casts
and sort expressions are yours; no part of an input string is ever treated as an
identifier.

## Two forms

JSON is canonical:

```json
{
  "name": "machines",
  "options": { "caseInsensitive": true, "maxDepth": 16, "fallbackFields": ["name"] },
  "fields": [
    { "name": "phase", "type": "enum", "column": "inv.phase",
      "values": ["in-use", "not-in-use"], "description": "Lifecycle phase" }
  ],
  "sorts": [ { "key": "name", "sql": "inv.name" } ]
}
```

```go
schema, err := sluice.LoadSchema(jsonBytes)
```

The Go struct form is equivalent and behaves identically:

```go
schema := sluice.Schema{
    Name:    "machines",
    Options: sluice.Options{FallbackFields: []string{"name"}},
    Fields: []sluice.Field{
        {Name: "phase", Type: sluice.TypeEnum, Column: "inv.phase",
            Values: []string{"in-use", "not-in-use"}, Description: "Lifecycle phase"},
    },
    Sorts: []sluice.Sort{{Key: "name", SQL: "inv.name"}},
}
c, err := sluice.New(schema, postgres.Dialect)
```

Use the struct form when you have custom emitters, which cannot be expressed in
JSON.

## Fields

| Key | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | yes | The identifier users type. Matched case-insensitively; must match `[a-z_][a-z0-9_]*`. |
| `type` | enum | yes | `string`, `enum`, `boolean`, `number`, `uuid`, `duration`, `timestamp`. |
| `column` | string | yes¹ | Raw SQL the field resolves to. Trusted, and never seen by a browser. |
| `description` | string | no | Shown in autocomplete. |
| `values` | string[] | no | Permitted values for an `enum`. |
| `dynamic` | bool | no | An `enum` whose values are supplied per request. |
| `operators` | string[] | no | Narrows the defaults for the type. Must be a subset. |
| `caseInsensitive` | bool | no | Overrides `options.caseInsensitive` for this field. |

¹ unless the field carries a custom emitter (below).

`column` is a SQL expression, not just a column name — `inv.metadata ->> 'ip'` and
`LOWER(t.name)` are fine. It is emitted verbatim, so it must be yours.

`and`, `or`, `not`, `true` and `false` are reserved and cannot be field names.

## Options

| Key | Default | Meaning |
|---|---|---|
| `caseInsensitive` | `true` | `string`/`enum` comparisons fold case. |
| `maxLength` | `4096` | Input codepoints; checked before lexing. |
| `maxDepth` | `16` | Expression nesting. |
| `maxPredicates` | `64` | Predicates per query. |
| `fallbackFields` | *(none)* | Fields used by the bare-value completion, see [editor.md](editor.md). |

The limits bound the work one input can cause. They are enforced by the parser,
never by the caller, because the input arrives from a browser.

## Enums, static and dynamic

A static enum constrains its values: `phase = "bogus"` is `invalid_value_for_field`,
and the diagnostic lists what is permitted.

A dynamic enum — racks, tenants, anything that comes from the database rather
than the source — has its values supplied per request:

```go
req := c.WithDynamic(map[string][]string{"rack": racks})
res, err := req.Compile(input)
```

`WithDynamic` returns a lightweight view; the compiler itself is immutable, safe
for concurrent use, and never caches the values. A dynamic field whose values
were not supplied accepts any string and offers no completions — it does not
error, because the server is the authority on what exists and a stale client list
should not reject a real rack.

## The browser-facing schema

`PublicSchema` reduces the schema to what a client needs — names, types, values,
descriptions — and removes `column` and `sorts[].sql`:

```go
pub := c.PublicSchema(map[string][]string{"rack": racks})
json.NewEncoder(w).Encode(pub)   // GET /sluice/schema.json
```

```js
const lang = createLanguage(await (await fetch('/sluice/schema.json')).json())
```

Dynamic values are resolved into the document, so the client needs nothing else.
The field stays marked dynamic, which is what keeps the client from rejecting a
value the server would have accepted.

`@sluice/core` runs `validate`, `suggest` and `parse` on a schema with no columns
at all. `compile` needs them, which is the honest constraint of compiling in the
browser — see [security.md](security.md) for when that is the right thing to want.

## Sorts

`ORDER BY` keys are named, never composed from input:

```go
order, err := c.OrderBy("name", sluice.Desc)  // ORDER BY inv.name DESC NULLS LAST
```

An unknown key is `unknown_sort_key`. The `sql` may be any expression, including
a `CASE` — sorting by a computed health status is a normal thing to want and a
terrible thing to accept from a query string.

## Custom emitters

Some predicates are not a column comparison. "Is this machine running any
operation" may be an `EXISTS` over a JSONB column; "is it online" may be a
computation over a heartbeat timestamp. A field can therefore carry an emitter
instead of a `column`:

```go
{Name: "operation", Type: sluice.TypeString, Operators: []string{"=", "!="}, Emit: operationEmitter}

const inProgress = "op.payload ->> 'status' = 'in-progress'"
const each = "EXISTS (SELECT 1 FROM jsonb_each(m.operations) AS op(name, payload) WHERE "

func operationEmitter(b *sluice.Builder, op sluice.Operator, v sluice.Value) error {
    if v.String() == "any" {
        b.WriteSQL(each + inProgress + ")")
        return nil
    }
    b.WriteSQL(each + "LOWER(op.name) " + string(op) + " ")
    b.WriteSQL(b.Bind(v.String()))
    b.WriteSQL(" AND " + inProgress + ")")
    return nil
}
```

Narrowing `operators` is what lets `string(op)` be written straight into the
fragment: it can only be one of the two spellings the field declares. An emitter
that accepts `~` has to spell out the `LIKE` form itself — `sluice.LikePattern`
escapes the argument and `b.Dialect().LikeEscapeClause()` gives you the trailing
`ESCAPE`. The full four-predicate version is in
[`emit_test.go`](../emit_test.go), where it doubles as the acceptance test for
this interface.

`Builder` gives you exactly two things: `WriteSQL` for fragments you wrote, and
`Bind`, which appends an argument and returns its placeholder. There is no method
that writes a value into SQL text, so the no-interpolation invariant holds by
construction even here. `b.Dialect()` is available for casts and escape clauses,
and `sluice.LikePattern` escapes a value for a `LIKE` argument.

The value arrives already coerced and case-folded: `v.String()`, `v.Number()`,
`v.Bool()`, `v.Seconds()` for a duration, and `v.Arg()` for whatever the field's
type produces.

Custom emitters are native-code only. They cannot appear in a JSON schema and
therefore cannot cross a trust boundary.

## Validation

`LoadSchema` and `New` check the schema and report **every** problem at once, so
a misconfiguration is one round trip rather than several:

```
sluice: invalid schema: field "and" uses a reserved name; field "b" has unknown type "jsonb"
```

Each is a `schema_invalid` diagnostic. What gets rejected: reserved and malformed
names, duplicates, unknown types, a field with neither a column nor an emitter (or
with both), operators outside the type's set, `values` or `dynamic` on a
non-enum, a sort key with no expression, and a `fallbackFields` entry that names
nothing.
