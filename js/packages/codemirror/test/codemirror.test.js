import { test } from 'node:test'
import assert from 'node:assert/strict'

import { createLanguage } from '../../core/src/index.js'
import { completionSource, lintSource, streamParser, toCodepoint, toUTF16 } from '../src/index.js'

const schema = {
  name: 'documents',
  options: { fallbackFields: ['name'] },
  fields: [
    { name: 'name', type: 'string', description: 'Document title' },
    { name: 'state', type: 'enum', values: ['shared', 'restricted'], description: 'Lifecycle state' },
    { name: 'active', type: 'boolean' },
  ],
}

const language = createLanguage(schema)

// CodeMirror hands a source `context.state.doc` and `context.pos`; a linter gets
// a view. Only `doc.toString()` is ever touched, so a fake is honest here.
const doc = (text) => ({ toString: () => text, length: text.length })
const context = (text, pos) => ({ state: { doc: doc(text) }, pos })
const view = (text) => ({ state: { doc: doc(text) } })

test('offsets convert both ways across an astral character', () => {
  const text = 'name = "🌍web"'
  assert.equal(toCodepoint(text, text.length), Array.from(text).length)
  assert.equal(toUTF16(text, 8), 8)
  assert.equal(toUTF16(text, 9), 10)
  assert.equal(toCodepoint(text, 10), 9)
})

test('completions come back in the language order, unfiltered', () => {
  const source = completionSource(language)
  const result = source(context('state ', 6))
  assert.deepEqual(
    result.options.map((o) => o.label),
    ['=', '!=', '~', '!~'],
  )
  // CodeMirror re-ranks by its own fuzzy score unless told not to, which would
  // put "!=" above "=".
  assert.equal(result.filter, false)
})

test('the replaced range is the span the language asked for', () => {
  const source = completionSource(language)
  const result = source(context('state = "res', 12))
  assert.deepEqual(result.options.map((o) => o.label), ['restricted'])
  assert.equal(result.from, 8) // the opening quote is replaced too
  assert.equal(result.to, 12)
})

test('a bare value becomes whole predicates', () => {
  const result = completionSource(language)(context('web-1', 5))
  assert.deepEqual(
    result.options.map((o) => o.label),
    ['name = "web-1"', 'name ~ "web-1"'],
  )
  assert.equal(result.from, 0)
  assert.equal(result.to, 5)
})

test('nothing to suggest is null, not an empty list', () => {
  // CodeMirror expects null when a source has nothing to say; an empty result
  // would suppress other sources.
  assert.equal(completionSource(language)(context('name = ', 7)), null)
})

test('an astral character does not shift the completion range', () => {
  const text = 'name = "🌍" AND st'
  const result = completionSource(language)(context(text, text.length))
  assert.deepEqual(result.options.map((o) => o.label), ['state'])
  assert.equal(text.slice(result.from, result.to), 'st')
})

test('diagnostics carry positions, severity and the stable code', () => {
  const [d] = lintSource(language)(view('stat = "x"'))
  assert.equal(d.code, 'unknown_field')
  assert.equal(d.severity, 'error')
  assert.equal(d.source, 'sluice')
  assert.deepEqual([d.from, d.to], [0, 4])
})

test('a zero-width diagnostic still underlines something', () => {
  const [d] = lintSource(language)(view('state = "shared" AND'))
  assert.equal(d.code, 'unexpected_eof')
  assert.ok(d.to > d.from)
})

test('an astral character does not shift a diagnostic after it', () => {
  const text = 'name = "🌍" AND stat = "x"'
  const [d] = lintSource(language)(view(text))
  assert.equal(text.slice(d.from, d.to), 'stat')
})

test('valid input lints clean', () => {
  assert.deepEqual(lintSource(language)(view('state = "shared"')), [])
})

test('the tokenizer separates declared fields from everything else', () => {
  const parser = streamParser(language)
  const tokens = tokenize(parser, 'state = "shared" AND stat')
  assert.deepEqual(tokens, [
    ['state', 'variableName'],
    ['=', 'operator'],
    ['"shared"', 'string'],
    ['AND', 'keyword'],
    ['stat', 'invalid'], // not a field, and styled as such while still typing
  ])
})

test('an unterminated string is marked rather than swallowing the line', () => {
  const tokens = tokenize(streamParser(language), 'name = "abc')
  assert.deepEqual(tokens[tokens.length - 1], ['"abc', 'invalid'])
})

test('the sources refuse anything that is not a language', () => {
  for (const make of [completionSource, lintSource, streamParser]) {
    assert.throws(() => make({}), TypeError)
  }
})

// A minimal StringStream, which is all the parser touches.
function tokenize(parser, text) {
  const out = []
  let pos = 0
  const stream = {
    get pos() {
      return pos
    },
    eatSpace() {
      const start = pos
      while (pos < text.length && /\s/.test(text[pos])) pos++
      return pos > start
    },
    match(re) {
      const m = re.exec(text.slice(pos))
      if (m) pos += m[0].length
      return m
    },
    next() {
      return text[pos++]
    },
  }
  while (pos < text.length) {
    const start = pos
    const style = parser.token(stream)
    if (pos === start) throw new Error('tokenizer made no progress')
    if (style) out.push([text.slice(start, pos), style])
  }
  return out
}
