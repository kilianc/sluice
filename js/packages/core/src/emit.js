import { codes, diagnostic } from './diagnostic.js'
import { nodeKinds } from './parse.js'
import { fieldOperators, fieldPermits, foldsCase, nearestFields } from './schema.js'
import { coerce } from './value.js'

/**
 * Builder accumulates emitted SQL and its bound arguments. It is what a custom
 * emitter writes through (AGENTS.md §8.4).
 *
 * There is deliberately no method that writes a value into SQL text. writeSQL
 * takes host-authored fragments; bind takes a value and hands back a
 * placeholder. Invariant 1 therefore holds by construction, including for
 * host-supplied emitters.
 */
export class Builder {
  #sql = []
  #args = []

  constructor(dialect) {
    this.dialect = dialect
  }

  /** Appends a host-authored SQL fragment verbatim. */
  writeSQL(fragment) {
    this.#sql.push(fragment)
  }

  /** Appends an argument and returns the placeholder that references it. */
  bind(value) {
    this.#args.push(value)
    return this.dialect.placeholder(this.#args.length)
  }

  get sql() {
    return this.#sql.join('')
  }

  get args() {
    return this.#args
  }
}

/**
 * Turns a value into a LIKE argument: metacharacters escaped, then wrapped in
 * wildcards. The emitted SQL carries an explicit ESCAPE clause. Without this,
 * name ~ "%" matches every row (AGENTS.md §8.2).
 */
export function likePattern(s) {
  return `%${s.replace(/\\/g, '\\\\').replace(/%/g, '\\%').replace(/_/g, '\\_')}%`
}

const spanFor = (node, part) => node[`${part}Span`] ?? node.span ?? [0, 0]

/**
 * Walks a tree and produces SQL, arguments and touched fields.
 *
 * Resolution runs here too, so an AST that arrived over the network is checked
 * exactly as a parsed one is.
 */
export function emit(schema, dialect, node, dynamic) {
  if (!node) {
    // Empty input compiles to empty. The host decides what an absent predicate
    // means; the compiler never invents 1=1 (AGENTS.md §8.5).
    return { sql: '', args: [], fields: [], diagnostics: [] }
  }

  const builder = new Builder(dialect)
  const diagnostics = []
  const fields = []
  const seen = new Set()

  const walk = (n) => {
    switch (n.kind) {
      case nodeKinds.binary:
        builder.writeSQL('(')
        walk(n.left)
        builder.writeSQL(n.op === 'or' ? ' OR ' : ' AND ')
        walk(n.right)
        builder.writeSQL(')')
        break
      case nodeKinds.not:
        builder.writeSQL('(NOT ')
        walk(n.expr)
        builder.writeSQL(')')
        break
      default: {
        const problem = emitPredicate(schema, dialect, builder, n, dynamic)
        if (problem) {
          diagnostics.push(problem)
          return
        }
        if (!seen.has(n.field)) {
          seen.add(n.field)
          fields.push(n.field)
        }
      }
    }
  }
  walk(node)

  if (diagnostics.length > 0) return { sql: '', args: [], fields: [], diagnostics }
  return { sql: builder.sql, args: builder.args, fields, diagnostics }
}

/** Emits one comparison, with no enclosing parentheses. */
function emitPredicate(schema, dialect, b, node, dynamic) {
  const field = schema.byName.get(node.field)
  if (!field) return unknownField(schema, node.field, spanFor(node, 'field'))

  if (!fieldPermits(field, node.op)) {
    return diagnostic(
      codes.unknownOperatorForField,
      spanFor(node, 'op'),
      `field ${field.name} does not support ${node.op}; it supports ${fieldOperators(field).join(' ')}`,
    )
  }

  const coerced = coerce(field, node.value, schema.options, dynamic)
  if (coerced.error) {
    return diagnostic(coerced.error, spanFor(node, 'value'), `field ${field.name} ${coerced.message}`)
  }
  const value = coerced.value

  if (field.emit) {
    field.emit(b, node.op, { type: field.type, value, literal: node.value })
    return null
  }
  if (!field.column) {
    // The browser-facing schema carries no column SQL, so a client-side compile
    // needs a schema whose columns the host is willing to publish (AGENTS.md §12
    // Mode A). Validate and suggest work without one; compiling does not.
    return diagnostic(
      codes.schemaInvalid,
      spanFor(node, 'field'),
      `field ${field.name} has no column in this schema, so it cannot be compiled here`,
    )
  }

  const column = field.column
  const fold = foldsCase(field, schema.options)

  switch (field.type) {
    case 'string':
    case 'enum': {
      const target = fold ? `LOWER(${column})` : column
      if (node.op === '~' || node.op === '!~') {
        const like = node.op === '!~' ? 'NOT LIKE' : 'LIKE'
        b.writeSQL(`${target} ${like} `)
        b.writeSQL(b.bind(likePattern(value)))
        b.writeSQL(dialect.likeEscapeClause)
      } else {
        b.writeSQL(`${target} ${node.op} `)
        b.writeSQL(b.bind(value))
      }
      break
    }
    case 'boolean':
      b.writeSQL(`${column} ${node.op} `)
      b.writeSQL(b.bind(dialect.boolArg(value)))
      break
    case 'number':
      b.writeSQL(`${column} ${node.op} `)
      b.writeSQL(b.bind(value))
      break
    case 'uuid':
      b.writeSQL(`${column} ${node.op} `)
      b.writeSQL(b.bind(value) + dialect.uuidCast)
      break
    case 'duration':
      b.writeSQL(`${dialect.ageSeconds(column)} ${node.op} `)
      b.writeSQL(b.bind(value))
      break
    case 'timestamp':
      b.writeSQL(`${column} ${node.op} `)
      b.writeSQL(b.bind(value) + dialect.timestampCast)
      break
  }
  return null
}

export function unknownField(schema, name, span) {
  const suggestions = nearestFields(schema, name)
  const message =
    `unknown field ${name}` +
    (suggestions.length > 0 ? `; did you mean ${suggestions.join(', ')}?` : '')
  return diagnostic(codes.unknownField, span, message, suggestions)
}
