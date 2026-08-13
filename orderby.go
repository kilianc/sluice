package sluice

import "github.com/kilianc/sluice/ast"

// Direction is a sort direction.
type Direction int

const (
	Asc Direction = iota
	Desc
)

func (d Direction) String() string {
	if d == Desc {
		return "DESC"
	}
	return "ASC"
}

// OrderBy renders an ORDER BY clause for a schema-declared sort key
// (AGENTS.md §8.6). The key selects a host-supplied expression; nothing about
// the clause is derived from input.
func (c *Compiler) OrderBy(key string, dir Direction) (string, error) {
	if key == "" {
		return "", nil
	}
	s, ok := c.sorts[key]
	if !ok {
		return "", &Error{Diagnostic: Diagnostic{
			Code:    ast.CodeUnknownSortKey,
			Message: "unknown sort key " + key,
		}}
	}
	return c.dialect.OrderBy(s.SQL, dir == Desc), nil
}
