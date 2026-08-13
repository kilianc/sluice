import { codes, diagnostic, sortDiagnostics } from './diagnostic.js'
import { isValueToken, kinds, lex } from './lex.js'

/** Node kinds. The spellings are the wire format (AGENTS.md §6). */
export const nodeKinds = {
  binary: 'binary',
  not: 'not',
  predicate: 'predicate',
}

export const binary = (op, left, right) => ({ kind: nodeKinds.binary, op, left, right })
export const not = (expr) => ({ kind: nodeKinds.not, expr })

/**
 * Parses a token stream into an AST (AGENTS.md §5).
 *
 * Parsing recovers: after a failed predicate it skips to the next AND, OR or
 * closing parenthesis and resumes, so validate can report every independent
 * problem in one pass (AGENTS.md §9). Recovery never invents a node — an input
 * that produced a diagnostic never compiles.
 */
export function parse(tokens, limits) {
  const p = {
    tokens,
    pos: 0,
    limits,
    depth: 0,
    predicates: 0,
    diagnostics: [],
    orphans: [],
    fatal: false,
  }
  const node = parseQuery(p)
  sortDiagnostics(p.diagnostics)
  return { node, orphans: p.orphans, diagnostics: p.diagnostics }
}

/** Lexes and parses in one step. */
export function parseString(input, limits) {
  const { tokens, diagnostics } = lex(input)
  const res = parse(tokens, limits)
  res.diagnostics = sortDiagnostics([...diagnostics, ...res.diagnostics])
  return res
}

const peek = (p) => p.tokens[p.pos]

function next(p) {
  const t = p.tokens[p.pos]
  if (t.kind !== kinds.EOF) p.pos++
  return t
}

function report(p, code, span, message) {
  p.diagnostics.push(diagnostic(code, span, message))
}

function parseQuery(p) {
  if (peek(p).kind === kinds.EOF) return null
  const node = parseExpr(p)
  while (!p.fatal && peek(p).kind !== kinds.EOF) {
    const t = peek(p)
    if (t.kind === kinds.RPAREN) {
      report(p, codes.unbalancedParen, t.span, "no matching '('")
      next(p)
    } else if (t.kind === kinds.AND || t.kind === kinds.OR) {
      // Recovered mid-query: keep parsing for diagnostics, but the partial tree
      // is not usable, so its value is discarded.
      next(p)
      parseExpr(p)
    } else {
      report(p, codes.unexpectedToken, t.span, unexpected(t, 'AND, OR or end of input'))
      next(p)
      recover(p)
    }
  }
  return node
}

const parseExpr = (p) => parseOr(p)

function parseOr(p) {
  let left = parseAnd(p)
  while (!p.fatal && peek(p).kind === kinds.OR) {
    next(p)
    left = join('or', left, parseAnd(p))
  }
  return left
}

function parseAnd(p) {
  let left = parseUnary(p)
  while (!p.fatal && peek(p).kind === kinds.AND) {
    next(p)
    left = join('and', left, parseUnary(p))
  }
  return left
}

/** Builds a binary node, tolerating a failed operand rather than inventing one. */
function join(op, left, right) {
  if (left === null) return right
  if (right === null) return left
  return binary(op, left, right)
}

function parseUnary(p) {
  if (peek(p).kind === kinds.NOT) {
    next(p)
    const expr = parseUnary(p)
    return expr === null ? null : not(expr)
  }
  return parsePrimary(p)
}

function parsePrimary(p) {
  if (peek(p).kind !== kinds.LPAREN) return parsePredicate(p)

  const open = next(p)
  if (p.depth + 1 > p.limits.maxDepth) {
    report(p, codes.depthExceeded, open.span, `nesting deeper than ${p.limits.maxDepth} levels`)
    p.fatal = true
    return null
  }
  p.depth++
  const inner = parseExpr(p)
  if (peek(p).kind === kinds.RPAREN) {
    next(p)
  } else if (!p.fatal) {
    report(p, codes.unbalancedParen, open.span, "no matching ')'")
  }
  p.depth--
  return inner
}

function parsePredicate(p) {
  const first = peek(p)
  if (first.kind !== kinds.IDENT) {
    fail(p, first, 'a field name')
    return null
  }
  next(p)

  const opToken = peek(p)
  if (opToken.kind !== kinds.OP) {
    orphan(p, first)
    fail(p, opToken, 'an operator')
    return null
  }
  next(p)

  const valueToken = peek(p)
  if (!isValueToken(valueToken)) {
    orphan(p, first)
    fail(p, valueToken, 'a quoted string, number or boolean')
    return null
  }
  next(p)

  const node = {
    kind: nodeKinds.predicate,
    field: first.text.toLowerCase(),
    op: opToken.text,
    value: literalOf(valueToken),
    span: [first.span[0], valueToken.span[1]],
    fieldSpan: first.span,
    opSpan: opToken.span,
    valueSpan: valueToken.span,
  }

  p.predicates++
  if (p.predicates > p.limits.maxPredicates) {
    report(p, codes.tooManyPredicates, node.span, `more than ${p.limits.maxPredicates} predicates`)
    p.fatal = true
    return null
  }
  return node
}

function literalOf(token) {
  switch (token.kind) {
    case kinds.NUMBER:
      return { type: 'number', value: token.num }
    case kinds.TRUE:
      return { type: 'boolean', value: true }
    case kinds.FALSE:
      return { type: 'boolean', value: false }
    default:
      return { type: 'string', value: token.str }
  }
}

/** Reports a token that cannot continue the production, then recovers. */
function fail(p, token, want) {
  if (token.kind === kinds.EOF) {
    report(p, codes.unexpectedEOF, token.span, `expected ${want}, found end of input`)
    p.fatal = true
    return
  }
  report(p, codes.unexpectedToken, token.span, unexpected(token, want))
  recover(p)
}

/**
 * Skips to the next AND, OR or ')' — the points at which the grammar can resume
 * — leaving that token for the caller (AGENTS.md §9).
 */
function recover(p) {
  for (;;) {
    const kind = peek(p).kind
    if (kind === kinds.AND || kind === kinds.OR || kind === kinds.RPAREN || kind === kinds.EOF) {
      return
    }
    next(p)
  }
}

/**
 * Records an identifier that appeared in field position but whose predicate did
 * not parse. Resolution still checks it, so that `EXISTS (SELECT 1 FROM t)`
 * reports unknown_field on EXISTS rather than only a syntax error further right.
 */
function orphan(p, token) {
  p.orphans.push({ name: token.text.toLowerCase(), span: token.span })
}

function unexpected(token, want) {
  const got = token.kind === kinds.STRING ? JSON.stringify(token.str) : token.text
  return `expected ${want}, found ${got}`
}
