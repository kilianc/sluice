import { codes, diagnostic, SchemaError } from './diagnostic.js'

/** Defaults for schema options (AGENTS.md §4.2). */
export const defaults = {
  caseInsensitive: true,
  maxLength: 4096,
  maxDepth: 16,
  maxPredicates: 64,
}

/**
 * The permitted operator set per type, in the order an editor should offer them
 * (AGENTS.md §4.5). Order is part of the contract: "=" should never sort below
 * "!=".
 */
export const defaultOperators = {
  string: ['=', '!=', '~', '!~'],
  enum: ['=', '!=', '~', '!~'],
  boolean: ['='],
  number: ['=', '!=', '<', '<=', '>', '>='],
  uuid: ['=', '!='],
  duration: ['<', '<=', '>', '>='],
  timestamp: ['<', '<=', '>', '>='],
}

const reserved = new Set(['and', 'or', 'not', 'true', 'false'])

/**
 * ASCII-only lowercasing. Non-ASCII case folding differs between every database
 * collation and every language's toLowerCase, and a filter bar cannot afford
 * that disagreement (AGENTS.md §8.2).
 */
export function asciiLower(s) {
  let out = ''
  for (const c of s) {
    out += c >= 'A' && c <= 'Z' ? String.fromCharCode(c.charCodeAt(0) + 32) : c
  }
  return out
}

const validName = (name) => /^[a-z_][a-z0-9_]*$/.test(name)

/**
 * Validates a schema against AGENTS.md §4 and returns it with its lookups
 * built. Every problem is reported at once, so a host fixes its configuration
 * in one pass rather than one error at a time.
 *
 * `column` is deliberately not required: the browser-facing schema has none,
 * and validate and suggest work perfectly well without it (AGENTS.md §4.3).
 * Compiling a field that has neither a column nor an emitter is what fails.
 */
export function prepareSchema(input) {
  const schema = {
    name: input.name ?? '',
    options: { ...input.options },
    fields: (input.fields ?? []).map((f) => ({ ...f })),
    sorts: (input.sorts ?? []).map((s) => ({ ...s })),
  }
  const problems = []
  const bad = (message) => problems.push(diagnostic(codes.schemaInvalid, [0, 0], message))

  const byName = new Map()
  for (const field of schema.fields) {
    const name = asciiLower(field.name ?? '')
    if (name === '') {
      bad('field name is empty')
      continue
    }
    if (!validName(name)) bad(`field "${field.name}" must match [a-z_][a-z0-9_]*`)
    else if (reserved.has(name)) bad(`field "${field.name}" uses a reserved name`)
    if (byName.has(name)) bad(`field "${name}" is declared twice`)

    if (!defaultOperators[field.type]) {
      bad(`field "${name}" has unknown type "${field.type}"`)
      byName.set(name, field)
      continue
    }
    if (field.column && field.emit) {
      bad(`field "${name}" declares both a column and a custom emitter`)
    }
    if (field.type !== 'enum') {
      if (field.values?.length > 0) bad(`field "${name}" is ${field.type}, so it cannot declare values`)
      if (field.dynamic) bad(`field "${name}" is ${field.type}, so it cannot be dynamic`)
    }
    for (const op of field.operators ?? []) {
      if (!defaultOperators[field.type].includes(op)) {
        bad(
          `field "${name}" permits "${op}", which is not one of ` +
            `${defaultOperators[field.type].join(' ')} for ${field.type}`,
        )
      }
    }
    field.name = name
    byName.set(name, field)
  }

  const sortKeys = new Set()
  for (const sort of schema.sorts) {
    if (!sort.key) {
      bad('sort key is empty')
      continue
    }
    if (sortKeys.has(sort.key)) bad(`sort key "${sort.key}" is declared twice`)
    sortKeys.add(sort.key)
  }

  for (const name of schema.options.fallbackFields ?? []) {
    if (!byName.has(asciiLower(name))) bad(`fallback field "${name}" is not a declared field`)
  }

  if (problems.length > 0) throw new SchemaError(problems)

  schema.options = {
    caseInsensitive: schema.options.caseInsensitive ?? defaults.caseInsensitive,
    maxLength: schema.options.maxLength || defaults.maxLength,
    maxDepth: schema.options.maxDepth || defaults.maxDepth,
    maxPredicates: schema.options.maxPredicates || defaults.maxPredicates,
    fallbackFields: schema.options.fallbackFields ?? [],
  }
  schema.byName = byName
  schema.sortsByKey = new Map(schema.sorts.map((s) => [s.key, s]))
  return schema
}

/** The field's permitted operators in declared order. */
export function fieldOperators(field) {
  return field.operators?.length > 0 ? field.operators : defaultOperators[field.type]
}

export const fieldPermits = (field, op) => fieldOperators(field).includes(op)

/**
 * The enum values in force for a field. A dynamic field takes them from the
 * request; failing that, from the values carried on the field itself, which is
 * how a schema resolved by publicSchema reaches a browser (AGENTS.md §4.4).
 */
export function fieldValues(field, dynamic) {
  if (field.dynamic && dynamic && field.name in dynamic) return dynamic[field.name]
  return field.values ?? []
}

export function foldsCase(field, options) {
  return field.caseInsensitive ?? options.caseInsensitive
}

/**
 * The schema as it is served to a browser: field names, types, values and
 * descriptions, with column SQL and sort expressions removed and dynamic values
 * resolved (AGENTS.md §4.3).
 */
export function publicSchema(schema, dynamic = {}) {
  return {
    name: schema.name,
    options: {
      caseInsensitive: schema.options.caseInsensitive,
      maxLength: schema.options.maxLength,
      maxDepth: schema.options.maxDepth,
      maxPredicates: schema.options.maxPredicates,
      fallbackFields: schema.options.fallbackFields,
    },
    fields: schema.fields.map((f) => {
      const pub = { name: f.name, type: f.type }
      if (f.description) pub.description = f.description
      if (f.operators?.length > 0) pub.operators = f.operators
      if (f.caseInsensitive !== undefined) pub.caseInsensitive = f.caseInsensitive
      if (f.dynamic) {
        pub.dynamic = true
        pub.values = [...(dynamic[f.name] ?? [])]
      } else if (f.values?.length > 0) {
        pub.values = [...f.values]
      }
      return pub
    }),
    sorts: schema.sorts.map((s) => ({ key: s.key })),
  }
}

/**
 * Up to 4 field names within Levenshtein distance 3 of name, ordered by
 * distance then alphabetically (AGENTS.md §7).
 */
export function nearestFields(schema, name) {
  return schema.fields
    .map((f) => ({ name: f.name, distance: levenshtein(name, f.name) }))
    .filter((c) => c.distance <= 3)
    .sort((a, b) => (a.distance !== b.distance ? a.distance - b.distance : a.name < b.name ? -1 : 1))
    .slice(0, 4)
    .map((c) => c.name)
}

function levenshtein(a, b) {
  const ar = Array.from(a)
  const br = Array.from(b)
  let prev = br.map((_, j) => j + 1)
  prev.unshift(0)
  for (let i = 1; i <= ar.length; i++) {
    const cur = [i]
    for (let j = 1; j <= br.length; j++) {
      const cost = ar[i - 1] === br[j - 1] ? 0 : 1
      cur[j] = Math.min(cur[j - 1] + 1, prev[j] + 1, prev[j - 1] + cost)
    }
    prev = cur
  }
  return prev[br.length]
}
