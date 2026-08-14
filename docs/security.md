# Security

Sluice exists because the filter bar it generalizes appended any token it did not
recognize straight into the `WHERE` clause. `state = "shared" OR EXISTS (SELECT 1
FROM document)` compiled. So did `1=1`. So did `bogus_column = "x"`. Behind SSO,
against a read replica, with a statement timeout, that was survivable; it is not
something to hand to strangers, and it is certainly not something a browser should
compile.

Every input in that paragraph is now a case in
[`conformance/corpus/006-security.json`](../conformance/corpus/006-security.json),
asserted to produce a diagnostic and no SQL, in every implementation.

## The invariants

1. **No interpolation.** A value from the input string is never concatenated into
   SQL text. It leaves the compiler only through the argument list — `LIKE`
   patterns, durations, UUIDs, numbers and booleans alike.
2. **No passthrough.** There is no branch that copies an unrecognized token into
   the output. Unknown input is a diagnostic, always.
3. **Identifiers come from the schema only.** Columns, aliases, casts and sort
   expressions are host-supplied. No part of an input string is ever treated as
   an identifier.
4. **Determinism.** The same schema, dialect and input produce byte-identical SQL
   and arguments across implementations, platforms and runs.
5. **Bounded cost.** Parsing is linear, and the length, depth and predicate limits
   are enforced before work proportional to the input.

Invariant 1 holds by construction rather than by discipline. The only way to emit
a value is `Builder.Bind`, which appends an argument and returns a placeholder;
there is no method that writes a value into SQL text. That is true for custom
emitters too, which is why the escape hatch is safe to offer.

Two consequences worth stating plainly. `name ~ "%"` looks for a literal percent
sign — `%`, `_` and `\` are escaped in the argument and the SQL carries an
explicit `ESCAPE '\'`, so a wildcard cannot be written by hand. And a bare word is
not a value: `state = shared` is a syntax error rather than a clever guess, which
is the single rule that removes the injection surface.

## Compiling in the browser

The design goal was that a browser could produce SQL, so that a database in the
browser — or a thin SQL endpoint — is enough, with no compilation service in
between. That makes the trust question unavoidable, so it is answered here rather
than left to each host.

**What makes it tractable:** because values leave only as bound parameters, the
SQL *shape* is a pure function of the schema and the AST's structure, drawn from a
finite set the schema enumerates. Shape is verifiable. Arbitrary text is not.

Three deployment modes, in descending order of how much you should like them.

### Mode A — local execution

The browser compiles and executes against a client-side database: DuckDB-WASM,
PGlite, sql.js. No trust boundary is crossed, so there is nothing to defend. This
is the no-backend case in its honest form and the one to reach for first. It is
also the only mode where the client legitimately needs `column` values, which
means publishing schema SQL you are willing to publish.

### Mode B — AST transport

The browser sends the **AST**; the server decodes it against *its own* schema and
compiles. Recommended whenever a database sits behind a server.

```js
fetch('/documents', { method: 'POST', body: JSON.stringify(lang.parse(input).ast) })
```

```go
var node ast.Node
json.NewDecoder(r.Body).Decode(&node)
res, err := compiler.CompileAST(&node)
```

Decoding an untrusted AST is subject to exactly the same validation as parsing
untrusted text: unknown node kinds, unknown operators, unknown fields, values of
the wrong type and excessive nesting are all rejected with the same diagnostics
the parser would produce. A hostile client can express only what the server's
schema permits, and the worst available outcome is an expensive-but-legal filter,
which `maxPredicates` and your statement timeout already bound.

Sending the source string instead is equally safe and simpler to log. Send the
AST when you want the client's parse errors to be authoritative.

### Mode C — SQL with proof

The browser sends `{ source, sql, args }`; the server recompiles `source` under
its own schema and compares the result to the received `sql`, rejecting any
mismatch. Be clear about what this buys: the received SQL is **never trusted** —
it is an assertion, not an input — so Mode C is Mode B plus a version-skew
detector. It is documented because people will build it anyway, and they should
build the version that cannot be turned into an injection.

### There is no Mode D

Executing client-supplied SQL after inspecting it — allowlisting keywords,
rejecting `;`, checking for `UNION` — is not a supported pattern, and no
implementation ships a helper that appears to bless it. If a proposal's safety
depends on parsing SQL you received over the network, it is out of scope for this
project.

## What is still yours

Sluice constrains *what predicate* runs. It does not know who is asking. Row-level
authorization, statement timeouts and read-only credentials remain the host's
responsibility, and a Sluice predicate should be `AND`-ed into whatever scoping
your application already applies:

```go
query := "SELECT ... FROM document doc WHERE doc.tenant_id = $1"
if res.SQL != "" {
    query += " AND (" + res.SQL + ")"
}
```

Note the parentheses: a compiled predicate is a single expression, but wrapping
it makes that independent of what the top-level operator happens to be.

The `column` values in your schema are trusted input. They are emitted verbatim,
so they must come from your source or your configuration — never from a database
column an end user can write, and never from a request.

## Reporting a vulnerability

Open a private security advisory on the GitHub repository rather than a public
issue. If you have found a way to get input text into emitted SQL, that is the
top of the pile: it means an invariant is broken, and the fix ships with a
corpus case in `006-security.json` so it cannot come back.
