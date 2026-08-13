import { codes, diagnostic } from './diagnostic.js'

/**
 * Token kinds. The spellings are normative: the conformance protocol reports
 * them verbatim (AGENTS.md §3.1, §11).
 */
export const kinds = {
  IDENT: 'IDENT',
  STRING: 'STRING',
  NUMBER: 'NUMBER',
  TRUE: 'TRUE',
  FALSE: 'FALSE',
  AND: 'AND',
  OR: 'OR',
  NOT: 'NOT',
  OP: 'OP',
  LPAREN: 'LPAREN',
  RPAREN: 'RPAREN',
  EOF: 'EOF',
}

const keywords = {
  and: kinds.AND,
  or: kinds.OR,
  not: kinds.NOT,
  true: kinds.TRUE,
  false: kinds.FALSE,
}

const isSpace = (c) => c === ' ' || c === '\t' || c === '\r' || c === '\n'
const isDigit = (c) => c >= '0' && c <= '9'
const isIdentStart = (c) => c === '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
const isIdentPart = (c) => isIdentStart(c) || isDigit(c) || c === '.'

/** True when the token can stand in value position (AGENTS.md §5). */
export function isValueToken(token) {
  return (
    token.kind === kinds.STRING ||
    token.kind === kinds.NUMBER ||
    token.kind === kinds.TRUE ||
    token.kind === kinds.FALSE
  )
}

/**
 * Lex converts input into a token stream terminated by an EOF token, plus any
 * lexical diagnostics (AGENTS.md §3).
 *
 * Positions are codepoint offsets, which is why this walks an array of
 * codepoints rather than indexing the string: a JavaScript string is UTF-16, and
 * an emoji in a filter value must not shift every span after it.
 *
 * Lexing recovers rather than stopping: a malformed string literal still yields
 * a STRING token so the parser can carry on, and an unrecognized character is
 * reported and skipped. Nothing unrecognized is ever carried into the token
 * stream — invariant 2 starts here.
 */
export function lex(input) {
  const src = Array.from(input)
  const tokens = []
  const diagnostics = []
  let i = 0

  const emit = (kind, text, start, end, extra) =>
    tokens.push({ kind, text, span: [start, end], ...extra })

  while (i < src.length) {
    const c = src[i]

    if (isSpace(c)) {
      i++
    } else if (c === '(') {
      emit(kinds.LPAREN, '(', i, i + 1)
      i++
    } else if (c === ')') {
      emit(kinds.RPAREN, ')', i, i + 1)
      i++
    } else if (c === '"') {
      i = lexString(src, i, tokens, diagnostics)
    } else if (isIdentStart(c)) {
      const start = i
      i++
      while (i < src.length && isIdentPart(src[i])) i++
      const text = src.slice(start, i).join('')
      emit(keywords[text.toLowerCase()] ?? kinds.IDENT, text, start, i)
    } else if (isDigit(c) || (c === '-' && isDigit(src[i + 1] ?? ''))) {
      const start = i
      if (src[i] === '-') i++
      while (i < src.length && isDigit(src[i])) i++
      if (src[i] === '.' && isDigit(src[i + 1] ?? '')) {
        i++
        while (i < src.length && isDigit(src[i])) i++
      }
      const text = src.slice(start, i).join('')
      emit(kinds.NUMBER, text, start, i, { num: Number(text) })
    } else {
      const n = operatorLength(src, i)
      if (n > 0) {
        emit(kinds.OP, src.slice(i, i + n).join(''), i, i + n)
        i += n
      } else {
        diagnostics.push(
          diagnostic(codes.unexpectedToken, [i, i + 1], `unexpected character ${JSON.stringify(c)}`),
        )
        i++
      }
    }
  }

  emit(kinds.EOF, '', src.length, src.length)
  return { tokens, diagnostics }
}

/**
 * Operators are matched longest-first so that !=, !~ and <= are never split
 * (AGENTS.md §3.1). A bare ! is not an operator.
 */
function operatorLength(src, i) {
  const two = src[i] + (src[i + 1] ?? '')
  if (two === '!=' || two === '!~' || two === '<=' || two === '>=') return 2
  const one = src[i]
  if (one === '=' || one === '~' || one === '<' || one === '>') return 1
  return 0
}

function lexString(src, start, tokens, diagnostics) {
  let i = start + 1
  let value = ''

  const finish = (end) => {
    tokens.push({
      kind: kinds.STRING,
      text: src.slice(start, end).join(''),
      str: value,
      span: [start, end],
    })
    return end
  }

  for (;;) {
    if (i >= src.length) {
      diagnostics.push(
        diagnostic(codes.unterminatedString, [start, src.length], 'string literal is not closed'),
      )
      return finish(src.length)
    }
    const c = src[i]
    if (c === '"') return finish(i + 1)
    if (c !== '\\') {
      value += c
      i++
      continue
    }
    if (i + 1 >= src.length) {
      diagnostics.push(
        diagnostic(codes.unterminatedString, [start, src.length], 'string literal is not closed'),
      )
      return finish(src.length)
    }
    const e = src[i + 1]
    switch (e) {
      case '"':
      case '\\':
        value += e
        break
      case 'n':
        value += '\n'
        break
      case 't':
        value += '\t'
        break
      case 'r':
        value += '\r'
        break
      default:
        diagnostics.push(
          diagnostic(codes.invalidEscape, [i, i + 2], `unknown escape \\${e}`),
        )
        value += e // recovery: keep the character, keep parsing
    }
    i += 2
  }
}

/**
 * The token's value as the conformance protocol reports it: a number for
 * NUMBER, a boolean for TRUE/FALSE, the unescaped contents for STRING, and the
 * source text otherwise.
 */
export function tokenValue(token) {
  switch (token.kind) {
    case kinds.NUMBER:
      return token.num
    case kinds.TRUE:
      return true
    case kinds.FALSE:
      return false
    case kinds.STRING:
      return token.str
    default:
      return token.text
  }
}
