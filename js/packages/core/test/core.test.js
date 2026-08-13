import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

import {
  createLanguage,
  decodeNode,
  duckdb,
  lex,
  likePattern,
  parseDuration,
  postgres,
  publicSchema,
} from '../src/index.js'

// The corpus covers everything the Go implementation and this one must agree
// on. These tests cover what is specific to running in JavaScript: UTF-16,
// schemas that carry no column SQL, and the shape of the public API.

const machinesSchema = JSON.parse(
  readFileSync(new URL('../../../../conformance/schemas/machines.json', import.meta.url), 'utf8'),
)

const machines = (dynamic = {}) => createLanguage(machinesSchema, { dynamic })

test('spans count codepoints, not UTF-16 units', () => {
  // "🎉" is one codepoint and two UTF-16 units. Indexing the string directly
  // would move every span after it, which is the bug this guards.
  const { tokens } = lex('name = "🎉" AND a')
  assert.deepEqual(
    tokens.map((t) => t.span),
    [
      [0, 4],
      [5, 6],
      [7, 10],
      [11, 14],
      [15, 16],
      [16, 16],
    ],
  )
})

test('a cursor lands on codepoints too', () => {
  const lang = machines()
  const suggestions = lang.suggest('name = "🎉" AND onl', 18)
  assert.deepEqual(
    suggestions.map((s) => [s.text, s.replaceSpan]),
    [['online', [15, 18]]],
  )
})

test('compiles the README example', () => {
  const lang = machines({ rack: ['ash1-r01'] })
  const res = lang.compile('phase = "in-use" AND rack ~ "ash1"', postgres)
  assert.equal(res.sql, `(LOWER(inv.phase) = $1 AND LOWER(loc.name) LIKE $2 ESCAPE '\\')`)
  assert.deepEqual(res.args, ['in-use', '%ash1%'])
  assert.deepEqual(res.fields, ['phase', 'rack'])
})

test('a dialect changes only what it is allowed to change', () => {
  const lang = machines()
  const input = 'id = "3F2504E0-4F89-11D3-9A0C-0305E82C3301" AND os_age > "2 days"'
  const pg = lang.compile(input, postgres)
  const duck = lang.compile(input, duckdb)
  assert.equal(pg.sql, '(inv.id = $1::uuid AND EXTRACT(EPOCH FROM (NOW() - img.created_at)) > $2)')
  assert.equal(
    duck.sql,
    "(inv.id = ?::UUID AND date_diff('second', img.created_at, current_timestamp) > ?)",
  )
  assert.deepEqual(pg.args, duck.args)
})

test('no input text reaches the SQL', () => {
  const lang = machines()
  const marker = 'Zq7Marker'
  for (const input of [
    `name = "${marker}"`,
    `name ~ "${marker}"`,
    `name !~ "%${marker}_"`,
    `name = "'; DROP TABLE machine --${marker}"`,
  ]) {
    const res = lang.compile(input, postgres)
    assert.ok(!res.sql.includes(marker), `${input} leaked into ${res.sql}`)
    assert.ok(!res.sql.includes('DROP'), `${input} leaked into ${res.sql}`)
  }
})

test('compile throws the first diagnostic and produces no SQL', () => {
  const lang = machines()
  assert.throws(
    () => lang.compile('phse = "x" AND online ~ "y"', postgres),
    (err) => {
      assert.equal(err.diagnostic.code, 'unknown_field')
      assert.deepEqual(err.diagnostic.span, [0, 4])
      return true
    },
  )
})

test('validate reports every independent problem', () => {
  const { ok, diagnostics } = machines().validate('phse = "x" AND online ~ "y" AND phase = "bogus"')
  assert.equal(ok, false)
  assert.deepEqual(
    diagnostics.map((d) => d.code),
    ['unknown_field', 'unknown_operator_for_field', 'invalid_value_for_field'],
  )
})

test('a browser-facing schema drives validate and suggest without any column SQL', () => {
  // AGENTS.md §4.3: the JS implementation must not require `column`.
  const pub = publicSchema(machines().schema, { rack: ['ash1-r01'] })
  assert.ok(!JSON.stringify(pub).includes('inv.phase'))

  const lang = createLanguage(pub)
  assert.equal(lang.validate('phase = "in-use"').ok, true)
  assert.deepEqual(
    lang.suggest('phase = ', 8).map((s) => s.text),
    ['in-use', 'not-in-use', 'maintenance'],
  )
})

test('compiling a column-less field says so rather than inventing SQL', () => {
  const lang = createLanguage(publicSchema(machines().schema))
  assert.throws(
    () => lang.compile('phase = "in-use"', postgres),
    (err) => err.diagnostic.code === 'schema_invalid',
  )
})

test('an untrusted AST is validated exactly like text', () => {
  const lang = machines()
  const cases = [
    [{ kind: 'raw', sql: '1=1' }, 'unexpected_token'],
    [{ kind: 'predicate', field: 'bogus', op: '=', value: { type: 'string', value: 'x' } }, 'unknown_field'],
    [{ kind: 'predicate', field: 'online', op: '~', value: { type: 'string', value: 'x' } }, 'unknown_operator_for_field'],
    [{ kind: 'predicate', field: 'phase', op: '=', value: { type: 'string', value: 'nope' } }, 'invalid_value_for_field'],
    [{ kind: 'predicate', field: 'name', op: "= 'x' OR 1=1 --", value: { type: 'string', value: 'x' } }, 'unexpected_token'],
    [{ kind: 'binary', op: 'union', left: null, right: null }, 'unexpected_token'],
  ]
  for (const [node, code] of cases) {
    assert.throws(
      () => lang.compileAST(node, postgres),
      (err) => err.diagnostic.code === code,
      JSON.stringify(node),
    )
  }
})

test('transporting the AST produces the same SQL as compiling the source', () => {
  const lang = machines()
  const input = 'phase = "in-use" AND NOT (name ~ "web" OR cores >= 8)'
  const fromText = lang.compile(input, postgres)
  const fromAST = lang.compileAST(JSON.parse(JSON.stringify(fromText.ast)), postgres)
  assert.equal(fromAST.sql, fromText.sql)
  assert.deepEqual(fromAST.args, fromText.args)
})

test('decodeNode rejects keys it does not recognize', () => {
  assert.throws(() =>
    decodeNode({
      kind: 'predicate',
      field: 'a',
      op: '=',
      value: { type: 'string', value: 'x' },
      sql: '1=1',
    }),
  )
  assert.throws(() => decodeNode({ kind: 'predicate', field: 'a', op: '=', value: { type: 'string', value: 8 } }))
  assert.throws(() => decodeNode({ kind: 'not' }))
})

test('dynamic values belong to a language, not to the schema', () => {
  const first = machines({ rack: ['ash1-r01'] })
  const second = machines({ rack: ['chi1-r09'] })
  assert.deepEqual(first.suggest('rack = "', 8).map((s) => s.text), ['ash1-r01'])
  assert.deepEqual(second.suggest('rack = "', 8).map((s) => s.text), ['chi1-r09'])
  // A dynamic field with no supplied values accepts any string and offers no
  // completions; it does not error.
  assert.deepEqual(machines().suggest('rack = "', 8), [])
  assert.equal(machines().validate('rack = "anything"').ok, true)
})

test('orderBy uses schema expressions only', () => {
  const lang = machines()
  assert.equal(lang.orderBy('name', 'asc', postgres), 'ORDER BY inv.name ASC NULLS LAST')
  assert.equal(lang.orderBy('phase', 'desc', postgres), 'ORDER BY inv.phase DESC NULLS LAST')
  assert.equal(lang.orderBy('', 'asc', postgres), '')
  assert.throws(
    () => lang.orderBy('inv.name; DROP TABLE machine', 'asc', postgres),
    (err) => err.diagnostic.code === 'unknown_sort_key',
  )
})

test('parseDuration', () => {
  assert.equal(parseDuration('2 days'), 172800)
  assert.equal(parseDuration('1w 2d'), 777600)
  assert.equal(parseDuration('90 minutes'), 5400)
  assert.equal(parseDuration('2HOURS'), 7200)
  for (const bad of ['', '2 fortnights', '2', 'days', '-1d', '1.5h', '1 month']) {
    assert.equal(parseDuration(bad), null, bad)
  }
})

test('likePattern escapes metacharacters before wrapping', () => {
  assert.equal(likePattern('cell'), '%cell%')
  assert.equal(likePattern('100%_x'), '%100\\%\\_x%')
  assert.equal(likePattern('a\\b'), '%a\\\\b%')
})

test('an invalid schema reports every problem at once', () => {
  assert.throws(
    () =>
      createLanguage({
        fields: [
          { name: 'or', type: 'string', column: 'inv.a' },
          { name: 'b', type: 'nope' },
        ],
      }),
    (err) => {
      assert.ok(err.diagnostics.length >= 2)
      assert.ok(err.diagnostics.every((d) => d.code === 'schema_invalid'))
      return true
    },
  )
})
