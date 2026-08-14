# @sluice/codemirror

CodeMirror 6 sources for [Sluice](https://github.com/kilianc/sluice):
completions, diagnostics and highlighting, all derived from the same schema that
drives the compiler.

```bash
npm install @sluice/core @sluice/codemirror
```

```js
import { autocompletion } from '@codemirror/autocomplete'
import { linter } from '@codemirror/lint'
import { StreamLanguage } from '@codemirror/language'
import { EditorView, basicSetup } from 'codemirror'

import { createLanguage } from '@sluice/core'
import { completionSource, lintSource, streamParser } from '@sluice/codemirror'

const language = createLanguage(await (await fetch('/sluice/schema.json')).json())

new EditorView({
  parent: document.getElementById('filter'),
  doc: 'state = "shared"',
  extensions: [
    basicSetup,
    StreamLanguage.define(streamParser(language)),
    autocompletion({ override: [completionSource(language)] }),
    linter(lintSource(language)),
  ],
})
```

## Sources, not extensions

This package exports the three things only Sluice can provide and imports
nothing — not even CodeMirror. You already import `autocompletion`, `linter` and
`StreamLanguage`, and you already have opinions about how they are configured;
handing you sources rather than a preassembled extension keeps all of that
yours, and keeps this package out of your build's business.

| export | wire it into |
|---|---|
| `completionSource(language)` | `autocompletion({ override: [...] })` |
| `lintSource(language)` | `linter(...)` |
| `streamParser(language)` | `StreamLanguage.define(...)` |

## What each one does

**`completionSource`** returns `filter: false`, because the order Sluice returns
is meaningful — field relevance first, then the schema's declared order for
operators and enum values — and CodeMirror would otherwise re-rank by its own
fuzzy score and put `!=` above `=`. The replaced range comes from the language's
`replaceSpan`, which is not always the token under the cursor: `web-1` lexes as
several tokens and someone typing it means one thing. It returns `null` rather
than an empty result when there is nothing to say, so other sources still get
their turn.

**`lintSource`** maps every independent diagnostic, carrying the stable `code`
alongside the message. A zero-width diagnostic is widened so it underlines
something — backwards when it sits at the very end of the document, which is
where `unexpected_eof` always is.

**`streamParser`** knows your field names, so an identifier the schema does not
declare is styled `invalid` the moment it is typed, before validation has said
anything.

## Offsets

Sluice counts codepoints; CodeMirror positions are offsets into a JavaScript
string, which counts UTF-16 code units. Every crossing is converted, and the
helpers are exported if you need them:

```js
import { toCodepoint, toUTF16 } from '@sluice/codemirror'
```

Without this, everything looks correct until a filter value contains an emoji,
and then every diagnostic and completion after it is off by one per astral
character.

## License

MIT
