// The Mode A reference: compile in the browser, execute in the browser.
//
// Sluice comes from the working tree rather than a CDN, so the playground always
// demonstrates the code in this repository. PGlite and Monaco are pinned.

import { PGlite } from 'https://cdn.jsdelivr.net/npm/@electric-sql/pglite@0.2.17/dist/index.js'
import { createLanguage, postgres } from '../js/packages/core/src/index.js'
import { register } from '../js/packages/monaco/src/index.js'

// One declaration. It drives the compiler, the completions, the diagnostics and
// the highlighting — the whole point of the project.
const schema = {
  name: 'documents',
  options: { fallbackFields: ['name'] },
  fields: [
    { name: 'id', type: 'uuid', column: 'doc.id', description: 'Document id' },
    { name: 'name', type: 'string', column: 'doc.name', description: 'Document title' },
    {
      name: 'state',
      type: 'enum',
      column: 'doc.state',
      values: ['shared', 'restricted', 'unpublished'],
      description: 'Lifecycle state',
    },
    { name: 'active', type: 'boolean', column: 'doc.active', description: 'Opened in the last 15 minutes' },
    { name: 'words', type: 'number', column: 'doc.words', description: 'CPU core count' },
    { name: 'edited', type: 'duration', column: 'doc.revised_at', description: 'Time since the last revision' },
    { name: 'team', type: 'enum', column: 'doc.team', dynamic: true, description: 'Team that owns it' },
  ],
  sorts: [{ key: 'name', sql: 'doc.name' }],
}

const seed = `
  CREATE TABLE document (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    state text NOT NULL,
    active boolean NOT NULL,
    words int NOT NULL,
    revised_at timestamptz NOT NULL,
    team text NOT NULL
  );
  INSERT INTO document (id, name, state, active, words, revised_at, team) VALUES
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3301', 'web-1',   'shared',      true,  32, now() - interval '3 days',   'design-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3302', 'web-2',   'shared',      true,  32, now() - interval '9 days',   'design-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3303', 'web-3',   'unpublished', false, 16, now() - interval '40 days',  'design-b'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3304', 'db-1',    'shared',      true,  64, now() - interval '2 hours',  'deploy-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3305', 'db-2',    'restricted',  false, 64, now() - interval '95 days',  'deploy-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3306', 'cache-1', 'shared',      true,   8, now() - interval '12 hours', 'deploy-b'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3307', 'batch-1', 'restricted',  false, 96, now() - interval '1 year',   'deploy-c');
`

const examples = [
  'state = "shared"',
  'name ~ "web" AND words >= 32',
  'edited > "30 days"',
  'NOT active = true',
  '(state = "shared" OR state = "unpublished") AND team ~ "desi"',
  // Two that are worth clicking for what they *do not* do: `~ "%"` looks for a
  // literal percent sign and finds nothing, where the implementation this
  // generalizes matched every row; `1=1` is a syntax error rather than SQL.
  'name ~ "%"',
  '1=1',
]

const $ = (id) => document.getElementById(id)

const db = new PGlite()
await db.exec(seed)

// Dynamic values come from the database, per request, exactly as they would on
// a server. Here "the request" is a page load.
const teams = await db.query('SELECT DISTINCT team FROM document ORDER BY team')
const language = createLanguage(schema, { dynamic: { team: teams.rows.map((r) => r.team) } })

const monaco = await window.monacoReady
register(monaco, { language })

const editor = monaco.editor.create($('editor'), {
  value: examples[0],
  language: 'sluice',
  theme: matchMedia('(prefers-color-scheme: dark)').matches ? 'vs-dark' : 'vs',
  automaticLayout: true,
  fontSize: 14,
  lineNumbers: 'off',
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  overviewRulerLanes: 0,
  folding: false,
  renderLineHighlight: 'none',
  wordWrap: 'on',
  // Suggestions are the point of the demo, so open them eagerly.
  quickSuggestions: { other: true, strings: true },
  suggestOnTriggerCharacters: true,
})

for (const text of examples) {
  const button = document.createElement('button')
  button.textContent = text
  button.onclick = () => {
    editor.setValue(text)
    editor.focus()
  }
  $('examples').append(button)
}

async function run() {
  const input = editor.getValue()

  let compiled
  try {
    compiled = language.compile(input, postgres)
  } catch (err) {
    // Compile throws the first diagnostic and produces no SQL. The editor is
    // already underlining it; show it here too, because this pane is where
    // someone is looking.
    const d = err.diagnostic
    $('sql').className = 'bad'
    $('sql').textContent = d ? `${d.span[0]}:${d.span[1]} ${d.code}: ${d.message}` : String(err)
    $('args').textContent = '[]'
    $('rows').innerHTML = ''
    $('count').textContent = ''
    return
  }

  $('sql').className = ''
  const where = compiled.sql ? `\nWHERE ${compiled.sql}` : ''
  const order = language.orderBy('name', 'asc', postgres)
  const sql = `SELECT doc.name, doc.state, doc.active, doc.words, doc.team\nFROM document doc${where}\n${order}`
  $('sql').textContent = sql
  $('args').textContent = JSON.stringify(compiled.args)

  const result = await db.query(sql, compiled.args)
  renderRows(result)
}

function renderRows({ fields, rows }) {
  const table = $('rows')
  table.innerHTML = ''
  const head = table.insertRow()
  for (const f of fields) {
    const th = document.createElement('th')
    th.textContent = f.name
    head.append(th)
  }
  for (const row of rows) {
    const tr = table.insertRow()
    for (const f of fields) {
      const td = tr.insertCell()
      td.textContent = String(row[f.name])
    }
  }
  $('count').textContent = `— ${rows.length} of 7`
}

editor.onDidChangeModelContent(() => {
  clearTimeout(run.timer)
  run.timer = setTimeout(run, 150)
})

await run()
