package ast

import (
	"encoding/json"
	"errors"
	"strconv"
)

// Diagnostic codes. These are stable API: the conformance corpus asserts them,
// and messages are deliberately not asserted (AGENTS.md §9).
const (
	CodeInputTooLong            = "input_too_long"
	CodeUnterminatedString      = "unterminated_string"
	CodeInvalidEscape           = "invalid_escape"
	CodeUnexpectedToken         = "unexpected_token"
	CodeUnexpectedEOF           = "unexpected_eof"
	CodeUnbalancedParen         = "unbalanced_paren"
	CodeDepthExceeded           = "depth_exceeded"
	CodeTooManyPredicates       = "too_many_predicates"
	CodeUnknownField            = "unknown_field"
	CodeUnknownOperatorForField = "unknown_operator_for_field"
	CodeInvalidValueForField    = "invalid_value_for_field"
	CodeInvalidDuration         = "invalid_duration"
	CodeUnknownSortKey          = "unknown_sort_key"
	CodeSchemaInvalid           = "schema_invalid"
)

// Span is a half-open [Start, End) range of 0-based Unicode codepoint offsets
// into the input (AGENTS.md §3). It marshals as a two-element JSON array.
type Span struct {
	Start int
	End   int
}

// MarshalJSON renders the span as [start, end].
func (s Span) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, 16)
	b = append(b, '[')
	b = strconv.AppendInt(b, int64(s.Start), 10)
	b = append(b, ',')
	b = strconv.AppendInt(b, int64(s.End), 10)
	b = append(b, ']')
	return b, nil
}

// UnmarshalJSON accepts the [start, end] form.
func (s *Span) UnmarshalJSON(data []byte) error {
	var pair []int
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return errors.New("span must be a two-element array")
	}
	s.Start, s.End = pair[0], pair[1]
	return nil
}

// Diagnostic is a single problem found in the input, positioned in it.
// Messages are for display and are free to change; Code and Span are not.
type Diagnostic struct {
	Code        string   `json:"code"`
	Message     string   `json:"message,omitempty"`
	Span        Span     `json:"span"`
	Suggestions []string `json:"suggestions,omitempty"`
}
