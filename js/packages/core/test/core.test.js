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

const documentsSchema = JSON.parse(
  readFileSync(new URL('../../../../conformance/schemas/documents.json', import.meta.url), 'utf8'),
)

const documents = (dynamic = {}) => createLanguage(documentsSchema, { dynamic })

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
  const lang = documents()
  const suggestions = lang.suggest('name = "🎉" AND act', 18)
  assert.deepEqual(
    suggestions.map((s) => [s.text, s.replaceSpan]),
    [['active', [15, 18]]],
  )
})

test('compiles the README example', () => {
  const lang = documents({ team: ['design-a'] })
  const res = lang.compile('state = "shared" AND team ~ "desi"', postgres)
  assert.equal(res.sql, `(LOWER(doc.state) = $1 AND LOWER(grp.name) LIKE $2 ESCAPE '\\')`)
  assert.deepEqual(res.args, ['shared', '%desi%'])
  assert.deepEqual(res.fields, ['state', 'team'])
})

test('a dialect changes only what it is allowed to change', () => {
  const lang = documents()
  const input = 'id = "3F2504E0-4F89-11D3-9A0C-0305E82C3301" AND edited > "2 days"'
  const pg = lang.compile(input, postgres)
  const duck = lang.compile(input, duckdb)
  assert.equal(pg.sql, '(doc.id = $1::uuid AND EXTRACT(EPOCH FROM (NOW() - rev.created_at)) > $2)')
  assert.equal(
    duck.sql,
    "(doc.id = ?::UUID AND date_diff('second', rev.created_at, current_timestamp) > ?)",
  )
  assert.deepEqual(pg.args, duck.args)
})

test('no input text reaches the SQL', () => {
  const lang = documents()
  const marker = 'Zq7Marker'
  for (const input of [
    `name = "${marker}"`,
    `name ~ "${marker}"`,
    `name !~ "%${marker}_"`,
    `name = "'; DROP TABLE document --${marker}"`,
  ]) {
    const res = lang.compile(input, postgres)
    assert.ok(!res.sql.includes(marker), `${input} leaked into ${res.sql}`)
    assert.ok(!res.sql.includes('DROP'), `${input} leaked into ${res.sql}`)
  }
})

test('compile throws the first diagnostic and produces no SQL', () => {
  const lang = documents()
  assert.throws(
    () => lang.compile('stat = "x" AND active ~ "y"', postgres),
    (err) => {
      assert.equal(err.diagnostic.code, 'unknown_field')
      assert.deepEqual(err.diagnostic.span, [0, 4])
      return true
    },
  )
})

test('validate reports every independent problem', () => {
  const { ok, diagnostics } = documents().validate('stat = "x" AND active ~ "y" AND state = "bogus"')
  assert.equal(ok, false)
  assert.deepEqual(
    diagnostics.map((d) => d.code),
    ['unknown_field', 'unknown_operator_for_field', 'invalid_value_for_field'],
  )
})

test('a browser-facing schema drives validate and suggest without any column SQL', () => {
  // AGENTS.md §4.3: the JS implementation must not require `column`.
  const pub = publicSchema(documents().schema, { team: ['design-a'] })
  assert.ok(!JSON.stringify(pub).includes('doc.state'))

  const lang = createLanguage(pub)
  assert.equal(lang.validate('state = "shared"').ok, true)
  assert.deepEqual(
    lang.suggest('state = ', 8).map((s) => s.text),
    ['shared', 'restricted', 'unpublished'],
  )
})

test('compiling a column-less field says so rather than inventing SQL', () => {
  const lang = createLanguage(publicSchema(documents().schema))
  assert.throws(
    () => lang.compile('state = "shared"', postgres),
    (err) => err.diagnostic.code === 'schema_invalid',
  )
})

test('an untrusted AST is validated exactly like text', () => {
  const lang = documents()
  const cases = [
    [{ kind: 'raw', sql: '1=1' }, 'unexpected_token'],
    [{ kind: 'predicate', field: 'bogus', op: '=', value: { type: 'string', value: 'x' } }, 'unknown_field'],
    [{ kind: 'predicate', field: 'active', op: '~', value: { type: 'string', value: 'x' } }, 'unknown_operator_for_field'],
    [{ kind: 'predicate', field: 'state', op: '=', value: { type: 'string', value: 'nope' } }, 'invalid_value_for_field'],
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
  const lang = documents()
  const input = 'state = "shared" AND NOT (name ~ "web" OR words >= 8)'
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
  const first = documents({ team: ['design-a'] })
  const second = documents({ team: ['deploy-z'] })
  assert.deepEqual(first.suggest('team = "', 8).map((s) => s.text), ['design-a'])
  assert.deepEqual(second.suggest('team = "', 8).map((s) => s.text), ['deploy-z'])
  // A dynamic field with no supplied values accepts any string and offers no
  // completions; it does not error.
  assert.deepEqual(documents().suggest('team = "', 8), [])
  assert.equal(documents().validate('team = "anything"').ok, true)
})

test('orderBy uses schema expressions only', () => {
  const lang = documents()
  assert.equal(lang.orderBy('name', 'asc', postgres), 'ORDER BY doc.name ASC NULLS LAST')
  assert.equal(lang.orderBy('state', 'desc', postgres), 'ORDER BY doc.state DESC NULLS LAST')
  assert.equal(lang.orderBy('', 'asc', postgres), '')
  assert.throws(
    () => lang.orderBy('doc.name; DROP TABLE document', 'asc', postgres),
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
          { name: 'or', type: 'string', column: 'doc.a' },
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
