# @sluice/monaco

Monaco binding for [Sluice](https://github.com/kilianc/sluice): completions,
diagnostics and syntax highlighting, all derived from the same schema that
drives the compiler.

```bash
npm install @sluice/core @sluice/monaco
```

```js
import * as monaco from 'monaco-editor'
import { createLanguage } from '@sluice/core'
import { register } from '@sluice/monaco'

const language = createLanguage(await (await fetch('/sluice/schema.json')).json())
const binding = register(monaco, { language })

monaco.editor.create(document.getElementById('filter'), {
  value: 'state = "shared"',
  language: 'sluice',
})
```

That is the whole integration. Completions, error underlines and highlighting
are live from the first keystroke.

## What it does

- **Completions** at the cursor, in the order the language returns them — field
  relevance, and declared order for operators and enum values. Monaco re-sorts
  by label unless told otherwise, so the binding sets `sortText` to preserve it.
- **Diagnostics** as model markers, with the code on the marker so you can branch
  on it. A zero-width diagnostic is widened by one column so it underlines
  something.
- **Highlighting** from a Monarch tokenizer built from your fields, which means
  an identifier the schema does not declare is styled as invalid the moment it
  is typed — before validation has said anything.
- **Hover** showing a field's type, description and permitted values.

## Options

```js
register(monaco, {
  language,             // required: a Language from @sluice/core
  languageId: 'sluice', // the Monaco language id to register
  markers: true,        // set diagnostics as model markers
  debounce: 0,          // ms to wait after a keystroke before validating
})
```

Returns `{ languageId, validate(model), dispose() }`. `dispose` removes the
providers and clears the markers it set.

Validation is fast enough to run on every keystroke — no network, no parse of
anything but the filter string — so `debounce` is there for very large documents
rather than as a default you should reach for.

## Offsets

Sluice counts codepoints; Monaco counts UTF-16 code units. The binding converts
at every boundary, and exports the helpers if you need them yourself:

```js
import { toCodepoint, toUTF16, spanToRange } from '@sluice/monaco'
```

Get this wrong and everything looks perfect until someone puts an emoji in a
filter value, at which point every marker after it is off by one per astral
character.

## Design

The package imports nothing. It takes a `Language` object rather than building
one from a schema, so the editor binding cannot disagree with the compiler about
what the language is — there is only one implementation of the language, and it
lives in `@sluice/core`.

`monaco-editor` is a peer dependency and is never imported: you pass the
namespace in. That keeps the binding agnostic about how Monaco was loaded, which
matters because Monaco is commonly loaded through its AMD bundle rather than as
an ES module.

## License

MIT
