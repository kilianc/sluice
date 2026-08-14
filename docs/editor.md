# Driving an editor

The reason to declare fields once is that the same declaration can drive the
filter bar. `@sluice/core` gives you the two things an editor needs — completions
at a cursor, and diagnostics with spans — and knows nothing about any particular
editor.

If you use Monaco or CodeMirror, the binding is the whole integration —
[`@sluice/monaco`](../js/packages/monaco/):

```js
import * as monaco from 'monaco-editor'
import { createLanguage } from '@sluice/core'
import { register } from '@sluice/monaco'

const language = createLanguage(await (await fetch('/sluice/schema.json')).json())
register(monaco, { language })

monaco.editor.create(element, { value: 'state = "shared"', language: 'sluice' })
```

…and [`@sluice/codemirror`](../js/packages/codemirror/), which exports sources
rather than extensions because you already assemble CodeMirror yourself:

```js
import { completionSource, lintSource, streamParser } from '@sluice/codemirror'

extensions: [
  StreamLanguage.define(streamParser(language)),
  autocompletion({ override: [completionSource(language)] }),
  linter(lintSource(language)),
]
```

Completions, error underlines, highlighting and hovers are live from the first
keystroke. The [playground](../playground/) is that code, running against a
Postgres in the same page.

The rest of this page is what the binding does on your behalf — worth reading if
you are wiring up CodeMirror, another editor, or a plain `<input>`, and worth
skimming even if you are not, because two of these are easy to get wrong.

## Getting the schema to the browser

The server serves the reduced schema, with dynamic values resolved:

```go
http.HandleFunc("/sluice/schema.json", func(w http.ResponseWriter, r *http.Request) {
    teams, _ := listTeams(r.Context())
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(compiler.PublicSchema(map[string][]string{"team": teams}))
})
```

```js
import { createLanguage } from '@sluice/core'

const lang = createLanguage(await (await fetch('/sluice/schema.json')).json())
```

That document carries no column SQL, so it is safe to hand out — see
[security.md](security.md).

## Completions

```js
lang.suggest(input, cursor)
// [{ text: 'shared', kind: 'value', detail: undefined, replaceSpan: [8, 8] }]
```

`cursor` is a codepoint offset. `replaceSpan` is the range the completion should
replace — not always the token under the cursor, because the prefix is computed
lexically: `web-1` lexes as several tokens, and someone typing it means one
thing. Always replace `replaceSpan` rather than guessing a word boundary; when
the user has typed an opening quote, the span covers it.

`kind` tells you what you are offering, which is usually what an icon is for:

| kind | Offered when | Order |
|---|---|---|
| `field` | at the start, or after `AND` / `OR` / `NOT` / `(` | exact, then prefix, then substring match; alphabetical within each |
| `operator` | after a field | the schema's declared order |
| `value` | after an operator, for enums and booleans | the schema's declared order |
| `keyword` | after a complete predicate — `AND`, `OR`, and `)` if one is open | |
| `expression` | when a prefix matches no field at all | fallback fields, `=` before `~` |

Declared order is preserved for operators and values on purpose. A schema author
who wrote `["shared", "restricted"]` ordered them for a reason, and `=` should
never sort below `!=`.

The `expression` kind is the one that earns its keep: paste a title or a UUID
into an empty filter bar and you get `name = "web-1"` and `name ~ "web-1"` — whole
predicates, not a field list. Configure which fields it uses with
`options.fallbackFields`; a prefix that looks like a UUID puts `uuid`-typed
fields first regardless.

Completions are available on input that does not parse, which is the only useful
behaviour: an editor asks for them precisely when the query is half-written.

## Diagnostics

```js
const { ok, diagnostics } = lang.validate(input)
// [{ code: 'unknown_field', message: 'unknown field stat; did you mean state?', span: [0, 4] }]
```

`validate` returns every independent problem, so one pass underlines all of them.
Show `message`; branch on `code`, which is stable API. `suggestions` on an
`unknown_field` diagnostic holds the near misses, if you want a quick fix action.

## Offsets

**Spans and cursors are codepoint offsets. Editors are not.** Monaco counts
UTF-16 code units, and so does CodeMirror 6 — its positions are offsets into a
JavaScript string, so `EditorState.create({doc: 'a🌍b'}).doc.length` is 4 rather
than 3. An emoji in a filter value therefore misplaces every marker after it by
one per astral character, unless you convert:

```js
const toCodepoint = (text, utf16) => Array.from(text.slice(0, utf16)).length

const toUTF16 = (text, codepoint) =>
  Array.from(text).slice(0, codepoint).join('').length
```

Both bindings do this for you at every boundary and export the helpers.

## Two things the binding does that are easy to miss

**Preserve the order.** Monaco re-sorts completions by label unless each item
carries a `sortText`, which would put `!=` above `=` and alphabetize enum values
a schema author deliberately ordered. Any editor that sorts for you needs the
same treatment.

**Filter against the replaced text, not the label.** An `expression` suggestion
is labelled `name = "web-1"` while the user typed `web-1`, so an editor filtering
on the label alone drops exactly the suggestion that was most useful. Point the
filter at the text inside `replaceSpan`.

Beyond that: set the completion range from `replaceSpan` rather than from the
editor's idea of the current word, widen a zero-width diagnostic so it underlines
something, and convert offsets at every boundary.

## Which mode you are in

If the browser also compiles, read [security.md](security.md) first. The short
version: compiling client-side is fine when the database is in the browser, and
when it is not, send the AST or the source string and let the server compile it.
Sending SQL is never trusted, whatever the client does.
