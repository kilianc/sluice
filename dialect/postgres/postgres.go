// Package postgres implements the PostgreSQL dialect.
package postgres

import "strconv"

// Dialect is the PostgreSQL dialect: numbered placeholders, :: casts, native
// booleans, NULLS LAST.
var Dialect dialect

type dialect struct{}

func (dialect) Name() string { return "postgres" }

func (dialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }

func (dialect) UUIDCast() string { return "::uuid" }

func (dialect) TimestampCast() string { return "::timestamptz" }

func (dialect) BoolArg(b bool) any { return b }

func (dialect) LikeEscapeClause() string { return ` ESCAPE '\'` }

func (dialect) AgeSeconds(column string) string {
	return "EXTRACT(EPOCH FROM (NOW() - " + column + "))"
}

func (dialect) OrderBy(sql string, desc bool) string {
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	return "ORDER BY " + sql + dir + " NULLS LAST"
}
