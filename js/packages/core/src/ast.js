import { nodeKinds } from './parse.js'

const operators = new Set(['=', '!=', '~', '!~', '<', '<=', '>', '>='])
const literalTypes = new Set(['string', 'number', 'boolean'])

/** True when s is one of the canonical operator spellings (AGENTS.md §3.1). */
export const isOperator = (s) => operators.has(s)

/**
 * Encodes a node into the normative wire format (AGENTS.md §6).
 *
 * Spans are emitted on predicate nodes only: they are informational, and the
 * internal spans the parser keeps for diagnostics are not part of the contract.
 */
export function encodeNode(node) {
  if (node === null || node === undefined) return null
  switch (node.kind) {
    case nodeKinds.binary:
      return {
        kind: node.kind,
        op: node.op,
        left: encodeNode(node.left),
        right: encodeNode(node.right),
      }
    case nodeKinds.not:
      return { kind: node.kind, expr: encodeNode(node.expr) }
    case nodeKinds.predicate:
      return {
        kind: node.kind,
        field: node.field,
        op: node.op,
        value: { type: node.value.type, value: node.value.value },
        span: node.span,
      }
    default:
      throw new TypeError(`unknown node kind ${node.kind}`)
  }
}

const allowed = {
  binary: new Set(['kind', 'op', 'left', 'right', 'span']),
  not: new Set(['kind', 'expr', 'span']),
  predicate: new Set(['kind', 'field', 'op', 'value', 'span']),
}

/**
 * Decodes a node, rejecting anything it does not recognize.
 *
 * Structural rejection happens here; schema-dependent rejection (unknown
 * fields, nesting depth, value coercion) happens in the compiler, which is the
 * only thing that knows the schema. Decoding an untrusted AST is subject to
 * exactly the same validation as parsing untrusted text (AGENTS.md §6).
 */
export function decodeNode(raw) {
  if (raw === null || raw === undefined) return null
  if (typeof raw !== 'object' || Array.isArray(raw)) {
    throw new TypeError('a node must be an object')
  }
  const permitted = allowed[raw.kind]
  if (!permitted) throw new TypeError(`unknown node kind ${raw.kind}`)
  for (const key of Object.keys(raw)) {
    if (!permitted.has(key)) throw new TypeError(`unknown key ${key} on a ${raw.kind} node`)
  }

  const node = { kind: raw.kind }
  if (Array.isArray(raw.span)) node.span = [raw.span[0], raw.span[1]]

  switch (raw.kind) {
    case nodeKinds.binary:
      if (raw.op !== 'and' && raw.op !== 'or') {
        throw new TypeError(`unknown binary operator ${raw.op}`)
      }
      if (raw.left == null || raw.right == null) {
        throw new TypeError('binary node needs both operands')
      }
      node.op = raw.op
      node.left = decodeNode(raw.left)
      node.right = decodeNode(raw.right)
      break
    case nodeKinds.not:
      if (raw.expr == null) throw new TypeError('not node needs an expression')
      node.expr = decodeNode(raw.expr)
      break
    default: {
      if (typeof raw.field !== 'string' || raw.field === '') {
        throw new TypeError('predicate node needs a field')
      }
      if (!isOperator(raw.op)) throw new TypeError(`unknown operator ${raw.op}`)
      const lit = raw.value
      if (!lit || typeof lit !== 'object' || !literalTypes.has(lit.type)) {
        throw new TypeError('predicate node needs a typed value')
      }
      const jsType = lit.type === 'boolean' ? 'boolean' : lit.type
      if (typeof lit.value !== jsType) {
        throw new TypeError(`value is not a ${lit.type}`)
      }
      node.field = raw.field
      node.op = raw.op
      node.value = { type: lit.type, value: lit.value }
    }
  }
  return node
}

/**
 * Expression nesting depth, counting the node itself as 1, so the maxDepth
 * limit applies to a decoded AST the same way the parser applies it to text.
 */
export function nodeDepth(node) {
  if (!node) return 0
  switch (node.kind) {
    case nodeKinds.binary:
      return Math.max(nodeDepth(node.left), nodeDepth(node.right)) + 1
    case nodeKinds.not:
      return nodeDepth(node.expr) + 1
    default:
      return 1
  }
}
