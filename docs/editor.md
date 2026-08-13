# Driving an editor

The reason to declare fields once is that the same declaration can drive the
filter bar. `@sluice/core` gives you the two things an editor needs — completions
at a cursor, and diagnostics with spans — and knows nothing about any particular
editor.

A binding for Monaco ships in v0.2 and one for CodeMirror in v0.3. Until then the
integration is about thirty lines, and this page is what they are.

## Getting the schema to the browser

The server serves the reduced schema, with dynamic values resolved:

```go
http.HandleFunc("/sluice/schema.json", func(w http.ResponseWriter, r *http.Request) {
    racks, _ := listRacks(r.Context())
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(compiler.PublicSchema(map[string][]string{"rack": racks}))
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
// [{ text: 'in-use', kind: 'value', detail: undefined, replaceSpan: [8, 8] }]
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
who wrote `["in-use", "not-in-use"]` ordered them for a reason, and `=` should
never sort below `!=`.

The `expression` kind is the one that earns its keep: paste a hostname or a UUID
into an empty filter bar and you get `name = "web-1"` and `name ~ "web-1"` — whole
predicates, not a field list. Configure which fields it uses with
`options.fallbackFields`; a prefix that looks like a UUID puts `uuid`-typed
fields first regardless.

Completions are available on input that does not parse, which is the only useful
behaviour: an editor asks for them precisely when the query is half-written.

## Diagnostics

```js
const { ok, diagnostics } = lang.validate(input)
// [{ code: 'unknown_field', message: 'unknown field phse; did you mean phase?', span: [0, 4] }]
```

`validate` returns every independent problem, so one pass underlines all of them.
Show `message`; branch on `code`, which is stable API. `suggestions` on an
`unknown_field` diagnostic holds the near misses, if you want a quick fix action.

## Offsets

**Spans and cursors are codepoint offsets. Editors are usually not.** Monaco and
CodeMirror 5 count UTF-16 code units, so an emoji or an astral character in a
filter value will misplace every marker after it unless you convert:

```js
const toCodepoint = (text, utf16) => Array.from(text.slice(0, utf16)).length

const toUTF16 = (text, codepoint) =>
  Array.from(text).slice(0, codepoint).join('').length
```

CodeMirror 6 counts codepoints already and needs neither.

## A whole Monaco binding

```js
monaco.languages.registerCompletionItemProvider('sluice', {
  triggerCharacters: [' ', '"', '(', '='],
  provideCompletionItems(model, position) {
    const text = model.getValue()
    const cursor = toCodepoint(text, model.getOffsetAt(position))
    return {
      suggestions: lang.suggest(text, cursor).map((s) => ({
        label: s.text,
        detail: s.detail,
        insertText: s.text,
        kind: monaco.languages.CompletionItemKind.Value,
        range: spanToRange(model, text, s.replaceSpan),
      })),
    }
  },
})

model.onDidChangeContent(() => {
  const text = model.getValue()
  monaco.editor.setModelMarkers(model, 'sluice', lang.validate(text).diagnostics.map((d) => ({
    message: d.message,
    severity: monaco.MarkerSeverity.Error,
    ...spanToRange(model, text, d.span),
  })))
})
```

where `spanToRange` maps a codepoint span through `toUTF16` and
`model.getPositionAt`. Validation is fast enough to run on every keystroke —
there is no network call and no parse of anything but the filter string.

## Which mode you are in

If the browser also compiles, read [security.md](security.md) first. The short
version: compiling client-side is fine when the database is in the browser, and
when it is not, send the AST or the source string and let the server compile it.
Sending SQL is never trusted, whatever the client does.
