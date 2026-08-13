import { asciiLower, fieldValues, foldsCase } from './schema.js'

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const rfc3339 =
  /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$/

/**
 * Every accepted duration unit and its length in seconds. A day is exactly
 * 86400 seconds and a week exactly 7 days; there are no months or years,
 * because they are not fixed-length and a filter bar is the wrong place to
 * litigate that (AGENTS.md §7.2).
 */
const durationUnits = {
  s: 1, sec: 1, secs: 1, second: 1, seconds: 1,
  m: 60, min: 60, mins: 60, minute: 60, minutes: 60,
  h: 3600, hr: 3600, hrs: 3600, hour: 3600, hours: 3600,
  d: 86400, day: 86400, days: 86400,
  w: 604800, week: 604800, weeks: 604800,
}

/**
 * Converts a duration literal to whole seconds (AGENTS.md §7.2). The grammar is
 * one or more <count><unit> pairs, optionally space-separated: "2 days", "36h",
 * "1w 2d", "90 minutes". A count is a run of ASCII digits — never fractional,
 * because coercion must yield whole seconds.
 *
 * Returns null when the literal is not a duration.
 */
export function parseDuration(s) {
  let total = 0
  let pairs = 0
  const src = Array.from(s)
  let i = 0

  while (i < src.length) {
    if (src[i] === ' ' || src[i] === '\t') {
      i++
      continue
    }
    const digitsStart = i
    while (i < src.length && src[i] >= '0' && src[i] <= '9') i++
    if (i === digitsStart) return null
    const count = Number(src.slice(digitsStart, i).join(''))
    if (!Number.isSafeInteger(count)) return null

    while (i < src.length && (src[i] === ' ' || src[i] === '\t')) i++
    const unitStart = i
    while (i < src.length && /[A-Za-z]/.test(src[i])) i++
    if (i === unitStart) return null

    const unit = durationUnits[asciiLower(src.slice(unitStart, i).join(''))]
    if (unit === undefined) return null
    total += count * unit
    if (!Number.isSafeInteger(total)) return null
    pairs++
  }
  return pairs === 0 ? null : total
}

/**
 * Coerces a literal to the field's type (AGENTS.md §7.1).
 *
 * Returns `{ value }` on success, or `{ error, message }` where error names the
 * diagnostic code the caller should raise. The coerced value is exactly what
 * gets bound as a parameter, and is the only form in which a value leaves the
 * compiler.
 */
export function coerce(field, literal, options, dynamic) {
  const fold = foldsCase(field, options)
  const wrongType = (want) => ({ error: 'invalid_value_for_field', message: `expects ${want}` })

  switch (field.type) {
    case 'string': {
      if (literal.type !== 'string') return wrongType('a quoted string')
      return { value: fold ? asciiLower(literal.value) : literal.value }
    }
    case 'enum': {
      if (literal.type !== 'string') return wrongType('a quoted string')
      const values = fieldValues(field, dynamic)
      // A non-dynamic enum constrains its values. A dynamic one whose values
      // were not supplied accepts any string and offers no completions; it does
      // not error (AGENTS.md §4.4).
      if (!field.dynamic && values.length > 0 && !includesValue(values, literal.value, fold)) {
        return {
          error: 'invalid_value_for_field',
          message: `expects one of ${values.slice(0, 8).join(', ')}${values.length > 8 ? ', …' : ''}`,
        }
      }
      return { value: fold ? asciiLower(literal.value) : literal.value }
    }
    case 'boolean':
      if (literal.type !== 'boolean') return wrongType('true or false')
      return { value: literal.value }
    case 'number':
      if (literal.type !== 'number') return wrongType('a number')
      return { value: literal.value }
    case 'uuid':
      if (literal.type !== 'string') return wrongType('a quoted uuid')
      if (!uuidPattern.test(literal.value)) return wrongType('a uuid')
      return { value: asciiLower(literal.value) }
    case 'duration': {
      if (literal.type !== 'string') return wrongType('a quoted duration such as "2 days"')
      const seconds = parseDuration(literal.value)
      if (seconds === null) {
        return {
          error: 'invalid_duration',
          message: 'is not a duration; use units s, m, h, d or w',
        }
      }
      return { value: seconds }
    }
    case 'timestamp': {
      if (literal.type !== 'string') return wrongType('a quoted RFC 3339 timestamp')
      const normalized = normalizeTimestamp(literal.value)
      if (normalized === null) return wrongType('an RFC 3339 timestamp')
      return { value: normalized }
    }
    default:
      return { error: 'invalid_value_for_field', message: 'has an unknown type' }
  }
}

function includesValue(values, value, fold) {
  return values.some((v) => v === value || (fold && asciiLower(v) === asciiLower(value)))
}

/** RFC 3339 in, RFC 3339 in UTC out, or null when it is not a timestamp. */
function normalizeTimestamp(s) {
  if (!rfc3339.test(s)) return null
  const ms = Date.parse(s)
  if (Number.isNaN(ms)) return null
  return new Date(ms).toISOString().replace(/\.\d{3}Z$/, 'Z')
}
