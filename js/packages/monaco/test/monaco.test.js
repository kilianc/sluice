import { test } from 'node:test'
import assert from 'node:assert/strict'

import { createLanguage } from '../../core/src/index.js'
import { register, toCodepoint, toUTF16, monarchTokens } from '../src/index.js'
import { languageConfiguration } from '../src/monarch.js'
import { createFakeMonaco } from './fake-monaco.js'

const schema = {
  name: 'documents',
  options: { fallbackFields: ['name'] },
  fields: [
    { name: 'name', type: 'string', description: 'Document title' },
    { name: 'state', type: 'enum', values: ['shared', 'restricted'], description: 'Lifecycle state' },
    { name: 'active', type: 'boolean' },
  ],
}

function setup(text = '') {
  const fake = createFakeMonaco()
  const language = createLanguage(schema)
  const binding = register(fake.monaco, { language })
  const model = fake.createModel(text)
  return { ...fake, language, binding, model }
}

test('offsets convert both ways across an astral character', () => {
  const text = 'name = "🌍web"'
  // The globe is two UTF-16 units and one codepoint, so the two disagree by
  // one from that character onwards.
  assert.equal(toCodepoint(text, text.length), Array.from(text).length)
  assert.equal(toUTF16(text, 8), 8) // before the globe: identical
  assert.equal(toUTF16(text, 9), 10) // after it: two units for one codepoint
  assert.equal(toCodepoint(text, 10), 9)
  // A cursor between the surrogates still counts the character once.
  assert.equal(toCodepoint(text, 9), 9)
})

test('completions carry the range the language asked for', () => {
  const { complete, model } = setup('state = ')
  const { suggestions } = complete(model, 8)
  assert.deepEqual(
    suggestions.map((s) => s.label),
    ['shared', 'restricted'],
  )
  const [first] = suggestions
  assert.equal(first.range.startColumn, 9)
  assert.equal(first.range.endColumn, 9)
})

test('declared order survives monaco resorting', () => {
  const { complete, model } = setup('state ')
  const { suggestions } = complete(model, 6)
  assert.deepEqual(
    suggestions.map((s) => s.label),
    ['=', '!=', '~', '!~'],
  )
  // Monaco sorts by sortText, so "=" must sort above "!=" despite the labels.
  const sorted = [...suggestions].sort((a, b) => a.sortText.localeCompare(b.sortText))
  assert.deepEqual(
    sorted.map((s) => s.label),
    ['=', '!=', '~', '!~'],
  )
})

test('an expression suggestion is not filtered out by its own label', () => {
  const { complete, model } = setup('web-1')
  const { suggestions } = complete(model, 5)
  assert.deepEqual(
    suggestions.map((s) => s.label),
    ['name = "web-1"', 'name ~ "web-1"'],
  )
  // The label does not contain the typed word in the position monaco expects,
  // so filterText has to be the text being replaced or the item disappears.
  for (const s of suggestions) assert.equal(s.filterText, 'web-1')
})

test('a quoted prefix is replaced including its opening quote', () => {
  const { complete, model } = setup('state = "res')
  const { suggestions } = complete(model, 12)
  assert.deepEqual(suggestions.map((s) => s.label), ['restricted'])
  const [only] = suggestions
  assert.equal(model.getValueInRange(only.range), '"res')
})

test('diagnostics become markers with 1-based positions', () => {
  const { markersFor, model } = setup('stat = "x"')
  const [marker] = markersFor(model)
  assert.equal(marker.code, 'unknown_field')
  assert.equal(marker.startLineNumber, 1)
  assert.equal(marker.startColumn, 1)
  assert.equal(marker.endColumn, 5) // codepoints [0,4) → columns [1,5)
  assert.match(marker.message, /did you mean/)
})

test('markers move with the model', () => {
  const { markersFor, model } = setup('stat = "x"')
  assert.equal(markersFor(model).length, 1)
  model.setValue('state = "shared"')
  assert.equal(markersFor(model).length, 0)
})

test('an astral character does not shift the marker after it', () => {
  // The regression this whole offsets module exists for: the emoji is one
  // codepoint and two UTF-16 units, so a naive binding underlines one column
  // short of the actual field.
  const { markersFor, model } = setup('name = "🌍" AND stat = "x"')
  const [marker] = markersFor(model)
  assert.equal(marker.code, 'unknown_field')
  const text = model.getValue()
  assert.equal(text.slice(marker.startColumn - 1, marker.endColumn - 1), 'stat')
})

test('a zero-width diagnostic still underlines something', () => {
  const { markersFor, model } = setup('state = "shared" AND')
  const [marker] = markersFor(model)
  assert.equal(marker.code, 'unexpected_eof')
  assert.ok(marker.endColumn > marker.startColumn)
})

test('multi-line input keeps line and column straight', () => {
  const { markersFor, model } = setup('state = "shared"\nAND stat = "x"')
  const [marker] = markersFor(model)
  assert.equal(marker.startLineNumber, 2)
  assert.equal(marker.startColumn, 5)
})

test('hovering a field shows its type and description', () => {
  const { hover, model } = setup('state = "shared"')
  const contents = hover(model, 2).contents.map((c) => c.value)
  assert.match(contents[0], /\*\*state\*\*/)
  assert.match(contents[0], /enum/)
  assert.equal(contents[1], 'Lifecycle state')
  assert.match(contents[2], /shared, restricted/)
})

test('hovering something that is not a field says nothing', () => {
  const { hover, model } = setup('state = "shared"')
  assert.equal(hover(model, 7), null)
})

test('highlighting knows which identifiers are fields', () => {
  const { registered } = setup()
  const monarch = registered.monarch.get('sluice')
  assert.deepEqual(monarch.fields, ['name', 'state', 'active'])
  assert.ok(monarch.keywords.includes('and'))
})

test('every @reference in the tokenizer resolves', () => {
  // Monarch resolves "@name" against the config, and silently emits it as a
  // literal token type when there is no such key — so a typo in "brackets"
  // costs you bracket matching with no error anywhere. A rename once turned
  // "brackets" into "bteamets" (it contains a word that was being replaced) and
  // every test still passed.
  const tokens = monarchTokens(schema)
  const special = new Set(['@pop', '@push', '@popall', '@default', '@rematch'])
  const refs = JSON.stringify(tokens.tokenizer).match(/@[A-Za-z_]+/g) ?? []
  for (const ref of refs) {
    if (special.has(ref)) continue
    const key = ref.slice(1)
    assert.ok(
      key in tokens || key in tokens.tokenizer,
      `${ref} is referenced by a rule but defined nowhere`,
    )
  }
  assert.ok(Array.isArray(tokens.brackets), 'brackets must be spelled the way monaco reads it')
  assert.ok(Array.isArray(languageConfiguration.brackets))
  assert.ok(languageConfiguration.autoClosingPairs.some((p) => p.open === '"'))
})

test('dispose removes the providers and clears the markers', () => {
  const fake = setup('stat = "x"')
  assert.equal(fake.markersFor(fake.model).length, 1)
  assert.equal(fake.registered.completion.length, 1)

  fake.binding.dispose()

  assert.equal(fake.registered.completion.length, 0)
  assert.equal(fake.registered.hover.length, 0)
  assert.equal(fake.markersFor(fake.model).length, 0)
})

test('the binding refuses anything that is not a language', () => {
  const { monaco } = createFakeMonaco()
  assert.throws(() => register(monaco, { language: {} }), TypeError)
  assert.throws(() => register(monaco), TypeError)
})

test('a model in another language is left alone', () => {
  const fake = createFakeMonaco()
  register(fake.monaco, { language: createLanguage(schema) })
  const other = fake.createModel('stat = "x"', 'sql')
  assert.equal(fake.markersFor(other).length, 0)
})
