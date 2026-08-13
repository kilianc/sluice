/**
 * Diagnostic codes. These are stable API: the conformance corpus asserts them,
 * and messages are deliberately not asserted (AGENTS.md §9).
 */
export const codes = {
  inputTooLong: 'input_too_long',
  unterminatedString: 'unterminated_string',
  invalidEscape: 'invalid_escape',
  unexpectedToken: 'unexpected_token',
  unexpectedEOF: 'unexpected_eof',
  unbalancedParen: 'unbalanced_paren',
  depthExceeded: 'depth_exceeded',
  tooManyPredicates: 'too_many_predicates',
  unknownField: 'unknown_field',
  unknownOperatorForField: 'unknown_operator_for_field',
  invalidValueForField: 'invalid_value_for_field',
  invalidDuration: 'invalid_duration',
  unknownSortKey: 'unknown_sort_key',
  schemaInvalid: 'schema_invalid',
}

/**
 * A diagnostic is `{ code, message, span: [start, end], suggestions? }`, where
 * the span is a half-open range of 0-based codepoint offsets.
 */
export function diagnostic(code, span, message, suggestions) {
  const d = { code, message, span }
  if (suggestions && suggestions.length > 0) d.suggestions = suggestions
  return d
}

/**
 * Orders diagnostics by position, so "the first diagnostic" means the leftmost
 * problem regardless of which stage found it.
 */
export function sortDiagnostics(diagnostics) {
  return diagnostics.sort((a, b) =>
    a.span[0] !== b.span[0] ? a.span[0] - b.span[0] : a.span[1] - b.span[1],
  )
}

/** Thrown by compile and orderBy; carries the first diagnostic. */
export class SluiceError extends Error {
  constructor(diag) {
    super(`${diag.code}${diag.message ? `: ${diag.message}` : ''}`)
    this.name = 'SluiceError'
    this.diagnostic = diag
  }
}

/** Thrown when a schema fails AGENTS.md §4 validation. Carries every problem. */
export class SchemaError extends Error {
  constructor(diagnostics) {
    super(`invalid schema: ${diagnostics.map((d) => d.message).join('; ')}`)
    this.name = 'SchemaError'
    this.diagnostics = diagnostics
  }
}
