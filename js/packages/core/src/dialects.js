/**
 * A dialect is everything a database changes about emission: how placeholders
 * are spelled, how values are cast, how age in seconds is computed, and how
 * ORDER BY puts nulls last (AGENTS.md §8.3).
 *
 * A dialect contributes SQL text only from its own constants. It never sees an
 * input string.
 */

const nullsLast = (sql, desc) => `ORDER BY ${sql} ${desc ? 'DESC' : 'ASC'} NULLS LAST`

export const postgres = {
  name: 'postgres',
  placeholder: (n) => `$${n}`,
  uuidCast: '::uuid',
  timestampCast: '::timestamptz',
  boolArg: (b) => b,
  likeEscapeClause: " ESCAPE '\\'",
  ageSeconds: (column) => `EXTRACT(EPOCH FROM (NOW() - ${column}))`,
  orderBy: nullsLast,
}

export const duckdb = {
  name: 'duckdb',
  placeholder: () => '?',
  uuidCast: '::UUID',
  timestampCast: '::TIMESTAMPTZ',
  boolArg: (b) => b,
  likeEscapeClause: " ESCAPE '\\'",
  ageSeconds: (column) => `date_diff('second', ${column}, current_timestamp)`,
  orderBy: nullsLast,
}

export const dialects = { postgres, duckdb }
