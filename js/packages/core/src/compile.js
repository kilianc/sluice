import { decodeNode, encodeNode, nodeDepth } from './ast.js'
import { codes, diagnostic, sortDiagnostics, SluiceError } from './diagnostic.js'
import { emit, unknownField } from './emit.js'
import { parseString } from './parse.js'
import { fieldOperators, fieldPermits, prepareSchema, publicSchema } from './schema.js'
import { suggest } from './suggest.js'
import { coerce } from './value.js'

/**
 * Creates a language bound to a schema.
 *
 * The same declaration drives parsing, compilation and the editor. Dynamic enum
 * values are supplied per language instance rather than cached on the schema
 * (AGENTS.md §4.4).
 */
export function createLanguage(schemaInput, { dynamic = {} } = {}) {
  const schema = prepareSchema(schemaInput)
  const resolvedDynamic = {}
  for (const [key, values] of Object.entries(dynamic)) {
    resolvedDynamic[key.toLowerCase()] = values
  }

  const limits = {
    maxDepth: schema.options.maxDepth,
    maxPredicates: schema.options.maxPredicates,
  }

  /** Lexes and parses, without resolving against the fields. */
  function parse(input) {
    const tooLong = lengthDiagnostic(input, schema.options.maxLength)
    if (tooLong) return { ast: null, diagnostics: [tooLong] }
    const res = parseString(input, limits)
    return { ast: encodeNode(res.node), diagnostics: res.diagnostics, node: res.node }
  }

  /** Every independent diagnostic, so an editor can underline all of them. */
  function diagnose(input) {
    const tooLong = lengthDiagnostic(input, schema.options.maxLength)
    if (tooLong) return { node: null, diagnostics: [tooLong] }

    const res = parseString(input, limits)
    const diagnostics = [...res.diagnostics, ...resolve(schema, res.node, resolvedDynamic)]
    // An identifier in field position whose predicate never parsed is still
    // checked, so `EXISTS (SELECT 1 FROM t)` reports unknown_field on EXISTS.
    for (const ref of res.orphans) {
      if (!schema.byName.has(ref.name)) diagnostics.push(unknownField(schema, ref.name, ref.span))
    }
    return { node: res.node, diagnostics: sortDiagnostics(diagnostics) }
  }

  return {
    schema,

    parse,

    validate(input) {
      const { diagnostics } = diagnose(input)
      return { ok: diagnostics.length === 0, diagnostics }
    },

    /**
     * Compiles an input string. Throws a SluiceError carrying the first
     * diagnostic and, in that case, produces no SQL.
     */
    compile(input, dialect) {
      const { node, diagnostics } = diagnose(input)
      if (diagnostics.length > 0) throw new SluiceError(diagnostics[0])
      const out = emit(schema, dialect, node, resolvedDynamic)
      if (out.diagnostics.length > 0) throw new SluiceError(out.diagnostics[0])
      return { sql: out.sql, args: out.args, fields: out.fields, ast: encodeNode(node) }
    },

    /**
     * Compiles an AST that did not come from this process — the untrusted-AST
     * entry point of AGENTS.md §12 Mode B. Decoding an untrusted AST is subject
     * to exactly the same validation as parsing untrusted text: the node names
     * fields, never columns, and every value goes through the same coercion.
     */
    compileAST(raw, dialect) {
      let node
      try {
        node = decodeNode(raw)
      } catch (err) {
        throw new SluiceError(diagnostic(codes.unexpectedToken, [0, 0], err.message))
      }
      if (nodeDepth(node) > schema.options.maxDepth) {
        throw new SluiceError(
          diagnostic(
            codes.depthExceeded,
            node.span ?? [0, 0],
            'expression nests deeper than the schema permits',
          ),
        )
      }
      const out = emit(schema, dialect, node, resolvedDynamic)
      if (out.diagnostics.length > 0) throw new SluiceError(out.diagnostics[0])
      return { sql: out.sql, args: out.args, fields: out.fields, ast: encodeNode(node) }
    },

    suggest(input, cursor) {
      return suggest(schema, resolvedDynamic, input, cursor)
    },

    /**
     * An ORDER BY clause for a schema-declared sort key (AGENTS.md §8.6). The
     * key selects a host-supplied expression; nothing about the clause is
     * derived from input.
     */
    orderBy(key, direction, dialect) {
      if (!key) return ''
      const sort = schema.sortsByKey.get(key)
      if (!sort) {
        throw new SluiceError(diagnostic(codes.unknownSortKey, [0, 0], `unknown sort key ${key}`))
      }
      if (!sort.sql) {
        throw new SluiceError(
          diagnostic(codes.schemaInvalid, [0, 0], `sort key ${key} has no expression in this schema`),
        )
      }
      return dialect.orderBy(sort.sql, direction === 'desc')
    },

    publicSchema(dynamicValues = resolvedDynamic) {
      return publicSchema(schema, dynamicValues)
    },
  }
}

function lengthDiagnostic(input, maxLength) {
  const length = Array.from(input).length
  if (length <= maxLength) return null
  return diagnostic(codes.inputTooLong, [0, length], 'input is longer than the schema permits')
}

/**
 * Binds every predicate to a schema field, checking the operator and coercing
 * the literal (AGENTS.md §7).
 */
function resolve(schema, node, dynamic) {
  if (!node) return []
  if (node.kind === 'binary') {
    return [...resolve(schema, node.left, dynamic), ...resolve(schema, node.right, dynamic)]
  }
  if (node.kind === 'not') return resolve(schema, node.expr, dynamic)

  const field = schema.byName.get(node.field)
  if (!field) return [unknownField(schema, node.field, node.fieldSpan ?? node.span)]

  if (!fieldPermits(field, node.op)) {
    return [
      diagnostic(
        codes.unknownOperatorForField,
        node.opSpan ?? node.span,
        `field ${field.name} does not support ${node.op}; it supports ${fieldOperators(field).join(' ')}`,
        fieldOperators(field),
      ),
    ]
  }

  const coerced = coerce(field, node.value, schema.options, dynamic)
  if (coerced.error) {
    return [
      diagnostic(
        coerced.error,
        node.valueSpan ?? node.span,
        `field ${field.name} ${coerced.message}`,
      ),
    ]
  }
  return []
}
