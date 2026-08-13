# @sluice/core

A configurable filter query language that compiles to SQL. Zero runtime
dependencies, ESM, no build step, and no editor imports — bindings for Monaco and
CodeMirror are separate packages, so a headless client compiling for DuckDB-WASM
pays nothing for them.

```js
import { createLanguage } from '@sluice/core'
import { postgres, duckdb } from '@sluice/core/dialects'

const lang = createLanguage(publicSchema, { dynamic: { rack: racks } })

lang.validate('phse = "x"')       // { ok: false, diagnostics: [{ code, message, span }] }
lang.suggest('phase = ', 8)       // [{ text, kind, detail, replaceSpan }]
lang.parse('phase = "in-use"')    // { ast, diagnostics }
lang.compile('phase = "in-use"', duckdb)  // { sql, args, fields, ast }
lang.compileAST(node, duckdb)     // the untrusted-AST entry point
lang.orderBy('name', 'desc', duckdb)
```

The schema this takes is the browser-facing one: field names, types, values and
descriptions, with no column SQL. `validate`, `suggest` and `parse` work on it as
it is. `compile` needs columns, which is the honest constraint of compiling in
the browser — see AGENTS.md §12 for the three deployment modes and which one you
should want.

Values leave the compiler only as bound parameters, so `sql` is a pure function
of your schema and the query's structure. See the [repository
README](https://github.com/kilianc/sluice/blob/main/README.md) for the language, and
[AGENTS.md](https://github.com/kilianc/sluice/blob/main/AGENTS.md) for the specification both implementations are
validated against.

## Tests

```
node --test test/*.test.js
```

The shared behaviour lives in the [conformance corpus](https://github.com/kilianc/sluice/blob/main/conformance/),
which drives this package and the Go reference through the same cases.
