// Package duckdb implements the DuckDB dialect, including DuckDB-WASM in a
// browser (AGENTS.md §12 Mode A).
package duckdb

// Dialect is the DuckDB dialect: positional placeholders, uppercase casts,
// native booleans, NULLS LAST.
var Dialect dialect

type dialect struct{}

func (dialect) Name() string { return "duckdb" }

func (dialect) Placeholder(int) string { return "?" }

func (dialect) UUIDCast() string { return "::UUID" }

func (dialect) TimestampCast() string { return "::TIMESTAMPTZ" }

func (dialect) BoolArg(b bool) any { return b }

func (dialect) LikeEscapeClause() string { return ` ESCAPE '\'` }

func (dialect) AgeSeconds(column string) string {
	return "date_diff('second', " + column + ", current_timestamp)"
}

func (dialect) OrderBy(sql string, desc bool) string {
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	return "ORDER BY " + sql + dir + " NULLS LAST"
}
