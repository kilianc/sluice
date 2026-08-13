import { kinds, lex } from './lex.js'
import { asciiLower, fieldOperators, fieldPermits, fieldValues } from './schema.js'

const WANT_FIELD = 0
const WANT_OPERATOR = 1
const WANT_VALUE = 2
const WANT_KEYWORD = 3

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

const isBoundary = (c) =>
  c === ' ' || c === '\t' || c === '\r' || c === '\n' || c === '(' || c === ')'

const matches = (candidate, prefix) =>
  prefix === '' || asciiLower(candidate).includes(asciiLower(prefix))

/**
 * Completions for a cursor position, where cursor is a codepoint offset
 * (AGENTS.md §10).
 *
 * This is a state walk over the token stream, not a parse: an editor asks for
 * completions precisely when the query is half-written, so it must work on
 * input that does not parse.
 */
export function suggest(schema, dynamic, input, cursor) {
  const src = Array.from(input)
  const at = Math.max(0, Math.min(cursor, src.length))

  // The prefix is defined lexically rather than from the token stream: "web-1"
  // lexes as several tokens, and the user typing it means one thing.
  let start = at
  while (start > 0 && !isBoundary(src[start - 1])) start--
  const span = [start, at]
  let prefix = src.slice(start, at).join('')
  if (prefix.startsWith('"')) prefix = prefix.slice(1)

  const { state, field, openParens } = walkState(schema, input, start)

  switch (state) {
    case WANT_FIELD: {
      const fields = fieldSuggestions(schema, prefix, span)
      if (fields.length > 0 || prefix === '') return fields
      return fallbackSuggestions(schema, prefix, span)
    }
    case WANT_OPERATOR:
      if (!field) return []
      return fieldOperators(field)
        .filter((op) => matches(op, prefix))
        .map((op) => ({ text: op, kind: 'operator', replaceSpan: span }))
    case WANT_VALUE:
      return valueSuggestions(field, dynamic, prefix, span)
    default: {
      const out = ['AND', 'OR']
        .filter((kw) => matches(kw, prefix))
        .map((kw) => ({ text: kw, kind: 'keyword', replaceSpan: span }))
      if (openParens > 0 && matches(')', prefix)) {
        out.push({ text: ')', kind: 'keyword', replaceSpan: span })
      }
      return out
    }
  }
}

/**
 * Determines which token class is expected at an offset, from the tokens
 * entirely before it.
 */
function walkState(schema, input, upTo) {
  const { tokens } = lex(input)
  let state = WANT_FIELD
  let field = null
  let openParens = 0

  for (const token of tokens) {
    if (token.kind === kinds.EOF || token.span[1] > upTo) break
    switch (token.kind) {
      case kinds.LPAREN:
        openParens++
        state = WANT_FIELD
        break
      case kinds.RPAREN:
        if (openParens > 0) openParens--
        state = WANT_KEYWORD
        break
      case kinds.AND:
      case kinds.OR:
      case kinds.NOT:
        state = WANT_FIELD
        break
      case kinds.IDENT:
        if (state === WANT_FIELD) {
          field = schema.byName.get(asciiLower(token.text)) ?? null
          state = WANT_OPERATOR
        } else {
          state = WANT_KEYWORD
        }
        break
      case kinds.OP:
        state = WANT_VALUE
        break
      default:
        state = WANT_KEYWORD
    }
  }
  return { state, field, openParens }
}

/**
 * Field candidates, ordered exact match, then prefix match, then substring
 * match, alphabetically within each group.
 */
function fieldSuggestions(schema, prefix, span) {
  const needle = asciiLower(prefix)
  const ranked = []
  for (const field of schema.fields) {
    const name = field.name
    let group
    if (name === needle) group = 0
    else if (name.startsWith(needle)) group = 1
    else if (name.includes(needle)) group = 2
    else continue
    ranked.push({ field, group })
  }
  ranked.sort((a, b) =>
    a.group !== b.group ? a.group - b.group : a.field.name < b.field.name ? -1 : 1,
  )
  return ranked.map(({ field }) => {
    const s = { text: field.name, kind: 'field', replaceSpan: span }
    if (field.description) s.detail = field.description
    return s
  })
}

/**
 * Enum values and booleans, in declared order — a schema author who wrote
 * ["in-use", "not-in-use"] ordered them for a reason. Other types are free text
 * and offer nothing.
 */
function valueSuggestions(field, dynamic, prefix, span) {
  if (!field) return []
  let values = []
  if (field.type === 'enum') {
    values = fieldValues(field, dynamic)
  } else if (field.type === 'boolean') {
    values = ['true', 'false']
  }
  return values
    .filter((v) => matches(v, prefix))
    .map((v) => ({ text: v, kind: 'value', replaceSpan: span }))
}

/**
 * Wraps a prefix that matches no field name into whole predicates against
 * host-nominated fields, so that pasting an identifier into an empty filter bar
 * gets somewhere (AGENTS.md §10.5).
 */
function fallbackSuggestions(schema, prefix, span) {
  if (prefix === '') return []

  const candidates = []
  const seen = new Set()
  const add = (field) => {
    if (!field || seen.has(field.name)) return
    seen.add(field.name)
    candidates.push(field)
  }
  // A pasted uuid means an id lookup, whatever the configured fallbacks say.
  if (uuidPattern.test(prefix)) {
    for (const field of schema.fields) if (field.type === 'uuid') add(field)
  }
  for (const name of schema.options.fallbackFields) add(schema.byName.get(asciiLower(name)))

  const out = []
  for (const field of candidates) {
    for (const op of ['=', '~']) {
      if (!fieldPermits(field, op)) continue
      const s = {
        text: `${field.name} ${op} "${quoteInner(prefix)}"`,
        kind: 'expression',
        replaceSpan: span,
      }
      if (field.description) s.detail = field.description
      out.push(s)
    }
  }
  return out
}

/** Escapes a value for display inside a suggested string literal. */
const quoteInner = (s) => s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
