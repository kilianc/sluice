package sluice

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/kilianc/sluice/ast"
)

// Value is a literal after coercion to its field's type. It is what a custom
// emitter receives, and Arg is exactly what gets bound as a parameter.
type Value struct {
	Type    FieldType
	Literal ast.Literal
	arg     any
}

// Arg returns the coerced value as it is bound. This is the only form in which
// a value leaves the compiler (invariant 1).
func (v Value) Arg() any { return v.arg }

// String returns the coerced string for string, enum, uuid and timestamp
// fields, and "" for the others.
func (v Value) String() string {
	s, _ := v.arg.(string)
	return s
}

// Number returns the coerced number for number fields.
func (v Value) Number() float64 {
	n, _ := v.arg.(float64)
	return n
}

// Bool returns the coerced boolean for boolean fields.
func (v Value) Bool() bool {
	b, _ := v.arg.(bool)
	return b
}

// Seconds returns a duration field's value in whole seconds. Duration strings
// never reach SQL (AGENTS.md §7.2).
func (v Value) Seconds() int64 {
	n, _ := v.arg.(int64)
	return n
}

var (
	errWrongLiteral    = errors.New("invalid value for field")
	errInvalidDuration = errors.New("invalid duration")
)

// coerce converts a literal to the field's type (AGENTS.md §7.1). The returned
// error is a message fragment; the caller attaches the code and the span.
func (c *Compiler) coerce(f *Field, lit ast.Literal, dynamic map[string][]string) (Value, string, error) {
	v := Value{Type: f.Type, Literal: lit}
	fold := f.foldsCase(c.schema.Options)

	switch f.Type {
	case TypeString:
		if lit.Type != ast.LitString {
			return v, "expects a quoted string", errWrongLiteral
		}
		s := lit.Str
		if fold {
			s = asciiLower(s)
		}
		v.arg = s

	case TypeEnum:
		if lit.Type != ast.LitString {
			return v, "expects a quoted string", errWrongLiteral
		}
		values := c.valuesOf(f, dynamic)
		// A non-dynamic enum constrains its values. A dynamic one whose values
		// were not supplied accepts any string and offers no completions; it
		// does not error (AGENTS.md §4.4).
		if !f.Dynamic && len(values) > 0 && !enumContains(values, lit.Str, fold) {
			return v, "expects one of " + listValues(values), errWrongLiteral
		}
		s := lit.Str
		if fold {
			s = asciiLower(s)
		}
		v.arg = s

	case TypeBoolean:
		if lit.Type != ast.LitBoolean {
			return v, "expects true or false", errWrongLiteral
		}
		v.arg = lit.Bool

	case TypeNumber:
		if lit.Type != ast.LitNumber {
			return v, "expects a number", errWrongLiteral
		}
		v.arg = lit.Num

	case TypeUUID:
		if lit.Type != ast.LitString {
			return v, "expects a quoted uuid", errWrongLiteral
		}
		if !isUUID(lit.Str) {
			return v, "expects a uuid", errWrongLiteral
		}
		v.arg = asciiLower(lit.Str)

	case TypeDuration:
		if lit.Type != ast.LitString {
			return v, "expects a quoted duration such as \"2 days\"", errWrongLiteral
		}
		secs, err := ParseDuration(lit.Str)
		if err != nil {
			return v, "is not a duration; use units s, m, h, d or w", errInvalidDuration
		}
		v.arg = secs

	case TypeTimestamp:
		if lit.Type != ast.LitString {
			return v, "expects a quoted RFC 3339 timestamp", errWrongLiteral
		}
		t, err := time.Parse(time.RFC3339, lit.Str)
		if err != nil {
			return v, "is not an RFC 3339 timestamp", errWrongLiteral
		}
		v.arg = t.UTC().Format(time.RFC3339)

	default:
		return v, "has an unknown type", errWrongLiteral
	}
	return v, "", nil
}

func enumContains(values []string, s string, fold bool) bool {
	for _, v := range values {
		if v == s || (fold && asciiLower(v) == asciiLower(s)) {
			return true
		}
	}
	return false
}

// listValues renders up to 8 permitted values for a diagnostic message.
func listValues(values []string) string {
	shown := values
	suffix := ""
	if len(shown) > 8 {
		shown, suffix = shown[:8], ", …"
	}
	return strings.Join(shown, ", ") + suffix
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

// durationUnits maps every accepted unit spelling to its length in seconds.
// A day is exactly 86400 seconds and a week exactly 7 days; there are no months
// or years, because they are not fixed-length and a filter bar is the wrong
// place to litigate that (AGENTS.md §7.2).
var durationUnits = map[string]int64{
	"s": 1, "sec": 1, "secs": 1, "second": 1, "seconds": 1,
	"m": 60, "min": 60, "mins": 60, "minute": 60, "minutes": 60,
	"h": 3600, "hr": 3600, "hrs": 3600, "hour": 3600, "hours": 3600,
	"d": 86400, "day": 86400, "days": 86400,
	"w": 604800, "week": 604800, "weeks": 604800,
}

// ParseDuration converts a duration literal to whole seconds (AGENTS.md §7.2).
// The grammar is one or more <number><unit> pairs, optionally space-separated:
// "2 days", "36h", "1w 2d", "90 minutes".
func ParseDuration(s string) (int64, error) {
	var total int64
	pairs := 0
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		if runes[i] == ' ' || runes[i] == '\t' {
			i++
			continue
		}
		start := i
		for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
		if i == start {
			return 0, errInvalidDuration
		}
		n, err := strconv.ParseInt(string(runes[start:i]), 10, 64)
		if err != nil {
			return 0, errInvalidDuration
		}
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		unitStart := i
		for i < len(runes) && isASCIILetter(runes[i]) {
			i++
		}
		if i == unitStart {
			return 0, errInvalidDuration
		}
		mult, ok := durationUnits[asciiLower(string(runes[unitStart:i]))]
		if !ok {
			return 0, errInvalidDuration
		}
		if n != 0 && mult > (1<<62)/n {
			return 0, errInvalidDuration
		}
		total += n * mult
		pairs++
	}
	if pairs == 0 {
		return 0, errInvalidDuration
	}
	return total, nil
}

func isASCIILetter(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
