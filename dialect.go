package sluice

// Dialect is everything a database changes about emission: how placeholders are
// spelled, how values are cast, how age in seconds is computed, and how ORDER BY
// puts nulls last (AGENTS.md §8.3).
//
// A dialect contributes SQL text only from its own constants. It never sees an
// input string.
type Dialect interface {
	// Name is the identifier used in configuration and in the conformance
	// registry, e.g. "postgres".
	Name() string

	// Placeholder renders the n-th parameter reference, 1-based.
	Placeholder(n int) string

	// UUIDCast is appended to a placeholder compared against a uuid column,
	// e.g. "::uuid". May be empty.
	UUIDCast() string

	// TimestampCast is appended to a placeholder compared against a timestamp
	// column, e.g. "::timestamptz". May be empty.
	TimestampCast() string

	// BoolArg converts a boolean to the form the driver should bind, for
	// databases without a native boolean.
	BoolArg(b bool) any

	// LikeEscapeClause is appended after a LIKE placeholder, e.g. " ESCAPE '\'".
	LikeEscapeClause() string

	// AgeSeconds renders an expression giving the age of a timestamp column in
	// seconds, so that edited > "2 days" means "older than two days".
	AgeSeconds(column string) string

	// OrderBy renders a complete ORDER BY clause with nulls last.
	OrderBy(sql string, desc bool) string
}
