package ast

import "encoding/json"

// TokenKind names a lexical class. The spellings are normative: the conformance
// protocol reports them verbatim (AGENTS.md §3.1, §11).
type TokenKind string

const (
	IDENT  TokenKind = "IDENT"
	STRING TokenKind = "STRING"
	NUMBER TokenKind = "NUMBER"
	TRUE   TokenKind = "TRUE"
	FALSE  TokenKind = "FALSE"
	AND    TokenKind = "AND"
	OR     TokenKind = "OR"
	NOT    TokenKind = "NOT"
	OP     TokenKind = "OP"
	LPAREN TokenKind = "LPAREN"
	RPAREN TokenKind = "RPAREN"
	EOF    TokenKind = "EOF"
)

// Token is one lexeme. Text is the source spelling as typed — keywords preserve
// the user's casing — while Str and Num carry the decoded value of STRING and
// NUMBER tokens.
type Token struct {
	Kind TokenKind
	Text string
	Str  string
	Num  float64
	Span Span
}

// Value is the token's value as the conformance protocol reports it: a JSON
// number for NUMBER, a JSON boolean for TRUE/FALSE, the unescaped contents for
// STRING, and the source text otherwise.
func (t Token) Value() any {
	switch t.Kind {
	case NUMBER:
		return t.Num
	case TRUE:
		return true
	case FALSE:
		return false
	case STRING:
		return t.Str
	default:
		return t.Text
	}
}

// MarshalJSON renders the wire form { kind, value, span }.
func (t Token) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind  TokenKind `json:"kind"`
		Value any       `json:"value"`
		Span  Span      `json:"span"`
	}{t.Kind, t.Value(), t.Span})
}

// IsValue reports whether the token can stand in value position (AGENTS.md §5).
// Notably a bare IDENT cannot; that single rule is what removes the injection
// surface the project exists to close.
func (t Token) IsValue() bool {
	switch t.Kind {
	case STRING, NUMBER, TRUE, FALSE:
		return true
	}
	return false
}
