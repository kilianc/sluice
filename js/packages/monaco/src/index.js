// @sluice/monaco — a binding, not a second implementation.
//
// Everything about the language lives in @sluice/core. This package converts
// between its shapes and Monaco's: codepoint spans to ranges, suggestions to
// completion items, diagnostics to markers. It imports nothing, and takes the
// Language object rather than building one, so the editor binding can never
// disagree with the compiler about what the language is.

import { monarchTokens, languageConfiguration } from './monarch.js'
import { toCodepoint, toUTF16, spanToRange } from './offsets.js'

export { toCodepoint, toUTF16, spanToRange, monarchTokens }

const KIND = {
  field: 'Field',
  operator: 'Operator',
  value: 'Value',
  keyword: 'Keyword',
  expression: 'Snippet',
}

/**
 * Wire a Sluice language into Monaco.
 *
 * @param {object} monaco the monaco-editor namespace
 * @param {object} options
 * @param {object} options.language a Language from @sluice/core
 * @param {string} [options.languageId] defaults to "sluice"
 * @param {boolean} [options.markers] set diagnostics as model markers, default true
 * @param {number} [options.debounce] ms to wait before validating, default 0
 * @returns {{dispose(): void, languageId: string, validate(model): void}}
 */
export function register(monaco, options) {
  const { language, languageId = 'sluice', markers = true, debounce = 0 } = options ?? {}
  if (!language || typeof language.suggest !== 'function') {
    throw new TypeError('@sluice/monaco: options.language must be a @sluice/core Language')
  }

  const disposables = []
  const timers = new WeakMap()

  if (!monaco.languages.getLanguages().some((l) => l.id === languageId)) {
    monaco.languages.register({ id: languageId })
  }
  monaco.languages.setLanguageConfiguration(languageId, languageConfiguration)
  monaco.languages.setMonarchTokensProvider(languageId, monarchTokens(language.schema ?? {}))

  disposables.push(
    monaco.languages.registerCompletionItemProvider(languageId, {
      // "=" and '"' are what a user types immediately before wanting a list.
      triggerCharacters: [' ', '"', '(', '=', '~', '<', '>', '!'],
      provideCompletionItems(model, position) {
        const text = model.getValue()
        const cursor = toCodepoint(text, model.getOffsetAt(position))
        const suggestions = language.suggest(text, cursor).map((s, i) => {
          const range = spanToRange(monaco, model, text, s.replaceSpan)
          return {
            label: s.text,
            detail: s.detail,
            kind: monaco.languages.CompletionItemKind[KIND[s.kind] ?? 'Text'],
            insertText: s.text,
            range,
            // Monaco re-sorts by label unless told otherwise, and the order
            // Sluice returns is normative: declared order for values and
            // operators, relevance for fields.
            sortText: String(i).padStart(4, '0'),
            // An `expression` suggestion has a label ("name = \"web-1\"") that
            // does not contain what the user typed, and Monaco would filter it
            // out on that basis. Filtering against the text being replaced
            // keeps every suggestion the language offered.
            filterText: model.getValueInRange(range) || s.text,
          }
        })
        return { suggestions }
      },
    }),
  )

  disposables.push(
    monaco.languages.registerHoverProvider(languageId, {
      provideHover(model, position) {
        const text = model.getValue()
        const cursor = toCodepoint(text, model.getOffsetAt(position))
        const field = fieldAt(language, text, cursor)
        if (!field) return null
        const lines = [`**${field.name}** \`${field.type}\``]
        if (field.description) lines.push(field.description)
        if (field.values?.length) {
          lines.push('Values: ' + field.values.slice(0, 8).join(', ') +
            (field.values.length > 8 ? ', …' : ''))
        }
        return { contents: lines.map((value) => ({ value })) }
      },
    }),
  )

  function validate(model) {
    if (!markers || model.isDisposed?.()) return
    const text = model.getValue()
    const diagnostics = language.validate(text).diagnostics.map((d) => {
      const range = spanToRange(monaco, model, text, d.span)
      return {
        message: d.message,
        severity: monaco.MarkerSeverity.Error,
        code: d.code,
        startLineNumber: range.startLineNumber,
        startColumn: range.startColumn,
        endLineNumber: range.endLineNumber,
        // A zero-width span — unexpected_eof sits at the end of the input —
        // would underline nothing at all, so widen it by one column.
        endColumn: range.endColumn + (range.startColumn === range.endColumn ? 1 : 0),
      }
    })
    monaco.editor.setModelMarkers(model, languageId, diagnostics)
  }

  function attach(model) {
    if (model.getLanguageId() !== languageId) return
    validate(model)
    disposables.push(
      model.onDidChangeContent(() => {
        if (debounce <= 0) return validate(model)
        clearTimeout(timers.get(model))
        timers.set(model, setTimeout(() => validate(model), debounce))
      }),
    )
  }

  monaco.editor.getModels().forEach(attach)
  disposables.push(monaco.editor.onDidCreateModel(attach))

  return {
    languageId,
    validate,
    dispose() {
      for (const d of disposables) d.dispose?.()
      disposables.length = 0
      monaco.editor.getModels().forEach((m) => {
        if (m.getLanguageId() === languageId) monaco.editor.setModelMarkers(m, languageId, [])
      })
    },
  }
}

/**
 * The schema field named at a cursor position, or undefined. Used for hovers,
 * so it deliberately answers on half-written input.
 */
function fieldAt(language, text, cursor) {
  const chars = Array.from(text)
  let start = cursor
  while (start > 0 && /[A-Za-z0-9_.]/.test(chars[start - 1])) start--
  let end = cursor
  while (end < chars.length && /[A-Za-z0-9_.]/.test(chars[end])) end++
  const word = chars.slice(start, end).join('').toLowerCase()
  if (!word) return undefined
  return (language.schema?.fields ?? []).find((f) => f.name.toLowerCase() === word)
}
