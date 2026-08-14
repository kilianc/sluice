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

// SQLite and MySQL have neither a boolean nor a uuid type, so booleans bind as
// 1 and 0 and nothing is cast.
const intBool = (b) => (b ? 1 : 0)

export const sqlite = {
  name: 'sqlite',
  placeholder: () => '?',
  uuidCast: '',
  timestampCast: '',
  boolArg: intBool,
  likeEscapeClause: " ESCAPE '\\'",
  ageSeconds: (column) => `(strftime('%s','now') - strftime('%s', ${column}))`,
  orderBy: nullsLast,
}

export const mysql = {
  name: 'mysql',
  placeholder: () => '?',
  uuidCast: '',
  timestampCast: '',
  boolArg: intBool,
  // A backslash is itself an escape character inside a MySQL string literal, so
  // ESCAPE '\' is an unterminated string there. The bound pattern is unaffected.
  likeEscapeClause: " ESCAPE '\\\\'",
  ageSeconds: (column) => `TIMESTAMPDIFF(SECOND, ${column}, NOW())`,
  // MySQL has no NULLS LAST; sorting on "IS NULL" first is the workaround.
  orderBy: (sql, desc) => `ORDER BY ${sql} IS NULL, ${sql} ${desc ? 'DESC' : 'ASC'}`,
}

export const dialects = { postgres, duckdb, sqlite, mysql }
