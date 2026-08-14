// Package sqlite implements the SQLite dialect, including sql.js in a browser.
package sqlite

// Dialect is the SQLite dialect: positional placeholders, no casts, integer
// booleans, NULLS LAST.
var Dialect dialect

type dialect struct{}

func (dialect) Name() string { return "sqlite" }

func (dialect) Placeholder(int) string { return "?" }

// SQLite has no uuid or timestamp type, so a value is compared as the text it
// is stored as. Nothing is cast.
func (dialect) UUIDCast() string { return "" }

func (dialect) TimestampCast() string { return "" }

// SQLite has no boolean type either: 1 and 0 are what a driver should bind.
func (dialect) BoolArg(b bool) any {
	if b {
		return int64(1)
	}
	return int64(0)
}

func (dialect) LikeEscapeClause() string { return ` ESCAPE '\'` }

func (dialect) AgeSeconds(column string) string {
	return "(strftime('%s','now') - strftime('%s', " + column + "))"
}

func (dialect) OrderBy(sql string, desc bool) string {
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	return "ORDER BY " + sql + dir + " NULLS LAST"
}
