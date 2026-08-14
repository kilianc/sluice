// Package mysql implements the MySQL dialect.
package mysql

// Dialect is the MySQL dialect: positional placeholders, no casts, integer
// booleans, and the two places MySQL differs from everyone else — a doubled
// backslash in the ESCAPE clause, and no NULLS LAST.
var Dialect dialect

type dialect struct{}

func (dialect) Name() string { return "mysql" }

func (dialect) Placeholder(int) string { return "?" }

func (dialect) UUIDCast() string { return "" }

func (dialect) TimestampCast() string { return "" }

// MySQL has no boolean type: BOOLEAN is an alias for TINYINT(1).
func (dialect) BoolArg(b bool) any {
	if b {
		return int64(1)
	}
	return int64(0)
}

// A backslash is itself an escape character inside a MySQL string literal, so
// ESCAPE '\' is an unterminated string there. The clause has to carry a doubled
// backslash to mean the single character every other dialect writes plainly.
// The bound pattern is unaffected: it is a parameter, not literal text.
func (dialect) LikeEscapeClause() string { return ` ESCAPE '\\'` }

func (dialect) AgeSeconds(column string) string {
	return "TIMESTAMPDIFF(SECOND, " + column + ", NOW())"
}

// MySQL has no NULLS LAST. Sorting on `<expr> IS NULL` first is the standard
// workaround: false sorts before true, so nulls land at the end either way.
func (dialect) OrderBy(sql string, desc bool) string {
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	return "ORDER BY " + sql + " IS NULL, " + sql + dir
}
