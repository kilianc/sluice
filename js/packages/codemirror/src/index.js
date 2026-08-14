// @sluice/codemirror — sources, not extensions.
//
// CodeMirror is assembled by the host: you already import `autocompletion`,
// `linter` and `StreamLanguage` and decide how they are configured. So this
// package exports the three things only Sluice can provide — a completion
// source, a lint source and a stream tokenizer — and imports nothing at all.
// You wire them up, which means you keep control of every CodeMirror option and
// this package never has to have an opinion about your build.
//
//   import { autocompletion } from '@codemirror/autocomplete'
//   import { linter } from '@codemirror/lint'
//   import { StreamLanguage } from '@codemirror/language'
//   import { completionSource, lintSource, streamParser } from '@sluice/codemirror'
//
//   const extensions = [
//     StreamLanguage.define(streamParser(language)),
//     autocompletion({ override: [completionSource(language)] }),
//     linter(lintSource(language)),
//   ]

import { toCodepoint, toUTF16 } from './offsets.js'

export { toCodepoint, toUTF16 }

const TYPE = {
  field: 'variable',
  operator: 'operator',
  value: 'text',
  keyword: 'keyword',
  expression: 'text',
}

/**
 * A CodeMirror completion source.
 * @param {object} language a Language from @sluice/core
 */
export function completionSource(language) {
  requireLanguage(language)
  return function sluiceCompletions(context) {
    const text = context.state.doc.toString()
    const cursor = toCodepoint(text, context.pos)
    const suggestions = language.suggest(text, cursor)
    if (suggestions.length === 0) return null

    // Every suggestion shares the replaceSpan the language computed, which is
    // not always the token under the cursor: `web-1` lexes as several tokens
    // and someone typing it means one thing.
    const [start, end] = suggestions[0].replaceSpan
    return {
      from: toUTF16(text, start),
      to: toUTF16(text, end),
      // The order is the language's — field relevance, then declared order for
      // operators and values. CodeMirror would otherwise re-rank by its own
      // fuzzy score and put `!=` above `=`.
      filter: false,
      options: suggestions.map((s) => ({
        label: s.text,
        detail: s.detail,
        type: TYPE[s.kind] ?? 'text',
      })),
    }
  }
}

/**
 * A CodeMirror lint source: every independent diagnostic, positioned.
 * @param {object} language a Language from @sluice/core
 */
export function lintSource(language) {
  requireLanguage(language)
  return function sluiceLint(view) {
    const text = view.state.doc.toString()
    return language.validate(text).diagnostics.map((d) => {
      const from = toUTF16(text, d.span[0])
      const to = toUTF16(text, d.span[1])
      if (to > from) {
        return diagnostic(from, to, d)
      }
      // A zero-width diagnostic would underline nothing at all. unexpected_eof
      // sits at the very end of the document, where there is no next character
      // to widen onto, so widen backwards over the last one instead.
      return from < text.length
        ? diagnostic(from, from + 1, d)
        : diagnostic(Math.max(0, from - 1), from, d)
    })
  }
}

/**
 * A StreamParser spec for StreamLanguage.define, built from the schema. An
 * identifier the schema does not declare is styled as invalid the moment it is
 * typed, before validation has said anything.
 * @param {object} language a Language from @sluice/core
 */
export function streamParser(language) {
  requireLanguage(language)
  const fields = new Set((language.schema?.fields ?? []).map((f) => f.name.toLowerCase()))
  const keywords = new Set(['and', 'or', 'not', 'true', 'false'])

  return {
    name: 'sluice',
    token(stream) {
      if (stream.eatSpace()) return null

      if (stream.match(/^"(?:[^"\\]|\\.)*"/)) return 'string'
      if (stream.match(/^"(?:[^"\\]|\\.)*$/)) return 'invalid' // unterminated
      if (stream.match(/^-?\d+(?:\.\d+)?/)) return 'number'
      if (stream.match(/^(?:!=|!~|<=|>=|[=~<>])/)) return 'operator'
      if (stream.match(/^[()]/)) return 'paren'

      const ident = stream.match(/^[A-Za-z_][A-Za-z0-9_.]*/)
      if (ident) {
        const word = ident[0].toLowerCase()
        if (keywords.has(word)) return 'keyword'
        return fields.has(word) ? 'variableName' : 'invalid'
      }

      stream.next()
      return 'invalid'
    },
    languageData: {
      // The language has no comments, and its only brackets are parentheses.
      closeBrackets: { brackets: ['(', '"'] },
      autocomplete: undefined,
    },
  }
}

function diagnostic(from, to, d) {
  return { from, to, severity: 'error', source: 'sluice', message: d.message, code: d.code }
}

function requireLanguage(language) {
  if (!language || typeof language.suggest !== 'function') {
    throw new TypeError('@sluice/codemirror: expected a Language from @sluice/core')
  }
}
