// The demo on the landing page: compile in the browser, execute in the browser.
//
// Sluice comes from this repository rather than a CDN, so the site always
// demonstrates the code it ships beside. Monaco and PGlite are pinned.

import { PGlite } from 'https://cdn.jsdelivr.net/npm/@electric-sql/pglite@0.2.17/dist/index.js'
import { createLanguage, dialects } from './js/packages/core/src/index.js'
import { register } from './js/packages/monaco/src/index.js'

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
    { name: 'active', type: 'boolean', column: 'doc.active', description: 'Open in the last 15 minutes' },
    { name: 'words', type: 'number', column: 'doc.words', description: 'Word count' },
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
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3301', 'onboarding',   'shared',      true,  1200, now() - interval '3 days',   'design-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3302', 'roadmap',      'shared',      true,  3400, now() - interval '9 days',   'design-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3303', 'postmortem',   'unpublished', false,  820, now() - interval '40 days',  'design-b'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3304', 'runbook',      'shared',      true,  6100, now() - interval '2 hours',  'deploy-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3305', 'old-notes',    'restricted',  false,  240, now() - interval '95 days',  'deploy-a'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3306', 'style-guide',  'shared',      true,  2750, now() - interval '12 hours', 'deploy-b'),
    ('3f2504e0-4f89-11d3-9a0c-0305e82c3307', 'archive-2019', 'restricted',  false, 9600, now() - interval '1 year',   'deploy-c');
`

const examples = [
  'state = "shared"',
  'name ~ "run" AND words >= 1000',
  'edited > "30 days"',
  'NOT active = true',
  '(state = "shared" OR state = "unpublished") AND team ~ "design"',
  'name ~ "%"',
  '1=1',
]

// PGlite is Postgres, so only the postgres output is executable here. The other
// three are compiled and shown, which is the point of the switcher.
const RUNNABLE = 'postgres'
const ORDER = ['postgres', 'duckdb', 'sqlite', 'mysql']

const $ = (id) => document.getElementById(id)
let dialect = RUNNABLE

const db = new PGlite()
await db.exec(seed)

const TOTAL = (await db.query('SELECT count(*)::int AS n FROM document')).rows[0].n
const teams = await db.query('SELECT DISTINCT team FROM document ORDER BY team')
const language = createLanguage(schema, { dynamic: { team: teams.rows.map((r) => r.team) } })

const monaco = await window.monacoReady
register(monaco, { language })

const css = getComputedStyle(document.body)
const hex = (name) => css.getPropertyValue(name).trim().replace('#', '')
const color = (name) => css.getPropertyValue(name).trim()

// A theme with a transparent background, so the editor disappears into the
// search bar and only the text is Monaco's.
monaco.editor.defineTheme('sluice', {
  base: 'vs',
  inherit: true,
  rules: [
    { token: 'type.identifier', foreground: hex('--syntax-field') },
    { token: 'string', foreground: hex('--syntax-string') },
    { token: 'string.quote', foreground: hex('--syntax-string') },
    { token: 'string.escape', foreground: hex('--syntax-string') },
    { token: 'keyword', foreground: hex('--fg-faint') },
    { token: 'operator', foreground: hex('--fg-muted') },
    { token: 'number', foreground: hex('--fg') },
    { token: 'invalid', foreground: hex('--bad') },
  ],
  colors: {
    'editor.background': '#00000000',
    // Monaco outlines its own container in the theme's focus colour, which
    // draws a blue box inside the search bar. The bar shows focus itself.
    focusBorder: '#00000000',
    'editor.lineHighlightBorder': '#00000000',
    'editorCursor.foreground': color('--fg'),
    'editorSuggestWidget.background': color('--bg'),
    'editorSuggestWidget.border': color('--line'),
    // The selected row needs its whole set of colours, not just a background:
    // vs leaves the rest white, for a selection meant to be a solid blue.
    'editorSuggestWidget.selectedBackground': color('--line'),
    'editorSuggestWidget.selectedForeground': color('--fg'),
    'editorSuggestWidget.selectedIconForeground': color('--fg-muted'),
    'editorSuggestWidget.foreground': color('--fg'),
    'editorSuggestWidget.highlightForeground': color('--accent'),
    'editorSuggestWidget.focusHighlightForeground': color('--accent'),
  },
})

const editor = monaco.editor.create($('editor'), {
  value: examples[0],
  language: 'sluice',
  theme: 'sluice',
  automaticLayout: true,
  fontSize: 14,
  lineHeight: 24,
  fontFamily: css.getPropertyValue('--mono'),
  // Everything that makes an editor look like an editor, off.
  lineNumbers: 'off',
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  scrollbar: { vertical: 'hidden', horizontal: 'hidden', handleMouseWheel: false },
  overviewRulerLanes: 0,
  overviewRulerBorder: false,
  hideCursorInOverviewRuler: true,
  folding: false,
  glyphMargin: false,
  lineDecorationsWidth: 0,
  lineNumbersMinChars: 0,
  renderLineHighlight: 'none',
  occurrencesHighlight: 'off',
  contextmenu: false,
  wordWrap: 'off',
  padding: { top: 0, bottom: 0 },
  quickSuggestions: { other: true, strings: true },
  suggestOnTriggerCharacters: true,
  fixedOverflowWidgets: true,
})

// A search bar is one line. Monaco has no single-line mode, so paste and Enter
// are flattened rather than fought with — the suggest widget still takes Enter
// for itself when it is open.
editor.onDidChangeModelContent(() => {
  const value = editor.getValue()
  if (value.includes('\n')) editor.setValue(value.replace(/\s*\n\s*/g, ' '))
})

const bar = $('searchbar')
editor.onDidFocusEditorText(() => bar.classList.add('focused'))
editor.onDidBlurEditorText(() => bar.classList.remove('focused'))
bar.addEventListener('mousedown', (e) => {
  // Clicking the padding of the bar should focus the field, as an input would.
  if (e.target === bar) editor.focus()
})

$('clear').onclick = () => {
  editor.setValue('')
  editor.focus()
}

for (const name of ORDER) {
  const button = document.createElement('button')
  button.textContent = name
  button.setAttribute('role', 'tab')
  button.setAttribute('aria-selected', String(name === dialect))
  button.onclick = () => {
    dialect = name
    for (const b of $('dialects').children) {
      b.setAttribute('aria-selected', String(b.textContent === name))
    }
    run()
  }
  $('dialects').append(button)
}

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

  $('clear').hidden = input === ''

  let shown
  try {
    shown = language.compile(input, dialects[dialect])
  } catch (err) {
    // Compile throws the first diagnostic and produces no SQL. The bar is
    // already underlining it; say it in words too, where someone is looking.
    const d = err.diagnostic
    $('sql').className = 'error'
    $('sql').textContent = d ? `${d.code} at ${d.span[0]}..${d.span[1]}\n${d.message}` : String(err)
    $('args').textContent = '[]'
    $('note').textContent = 'No SQL is produced for input that does not compile.'
    setCount(d ? d.code.replace(/_/g, ' ') : 'error', true)
    return
  }

  $('sql').className = ''
  const where = shown.sql ? `\nWHERE ${shown.sql}` : ''
  const order = language.orderBy('name', 'asc', dialects[dialect])
  const sql = `SELECT doc.name, doc.state, doc.active, doc.words, doc.team\nFROM document doc${where}\n${order}`
  $('sql').textContent = sql
  $('args').textContent = JSON.stringify(shown.args, null, 1).replace(/\n\s*/g, ' ')
  $('note').textContent =
    dialect === RUNNABLE
      ? shown.args.length
        ? 'Every value above is a bound parameter, never text in the SQL.'
        : ''
      : `Compiled for ${dialect}. The rows are executed by the Postgres in this page.`

  // The results always reflect the query; only the pane above follows the
  // dialect tabs, since the database in the page is Postgres.
  const runnable =
    dialect === RUNNABLE ? { sql, args: shown.args } : executableFor(input, shown.args)
  const result = await db.query(runnable.sql, runnable.args)
  renderRows(result)
  setCount(`${result.rows.length} of ${TOTAL}`, false)
}

function executableFor(input, args) {
  const pg = language.compile(input, dialects[RUNNABLE])
  const where = pg.sql ? `\nWHERE ${pg.sql}` : ''
  const order = language.orderBy('name', 'asc', dialects[RUNNABLE])
  return {
    sql: `SELECT doc.name, doc.state, doc.active, doc.words, doc.team\nFROM document doc${where}\n${order}`,
    args: pg.args,
  }
}

function setCount(text, bad) {
  const el = $('count')
  el.textContent = text
  el.classList.toggle('bad', Boolean(bad))
}

function renderRows({ fields, rows }) {
  const table = $('rows')
  table.innerHTML = ''
  const head = table.createTHead().insertRow()
  for (const f of fields) {
    const th = document.createElement('th')
    th.textContent = f.name
    head.append(th)
  }
  const body = table.createTBody()
  for (const row of rows) {
    const tr = body.insertRow()
    for (const f of fields) {
      tr.insertCell().textContent = String(row[f.name])
    }
  }
  if (rows.length === 0) {
    const cell = body.insertRow().insertCell()
    cell.colSpan = fields.length
    cell.textContent = 'no rows'
    cell.style.color = 'var(--fg-faint)'
  }
}

editor.onDidChangeModelContent(() => {
  clearTimeout(run.timer)
  run.timer = setTimeout(run, 120)
})

await run()
