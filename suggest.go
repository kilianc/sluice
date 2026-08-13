package sluice

import (
	"sort"
	"strings"

	"github.com/kilianc/sluice/ast"
)

// Suggestion kinds (AGENTS.md §10).
const (
	SuggestField      = "field"
	SuggestOperator   = "operator"
	SuggestValue      = "value"
	SuggestKeyword    = "keyword"
	SuggestExpression = "expression"
)

// Suggestion is one completion candidate. ReplaceSpan covers the text the
// editor should replace, including an opening quote when the user typed one.
type Suggestion struct {
	Text        string `json:"text"`
	Kind        string `json:"kind"`
	Detail      string `json:"detail,omitempty"`
	ReplaceSpan Span   `json:"replaceSpan"`
}

type suggestState int

const (
	wantField suggestState = iota
	wantOperator
	wantValue
	wantKeyword
)

// Suggest returns completions for a cursor position, where cursor is a
// codepoint offset (AGENTS.md §10).
//
// It is a state walk over the token stream, not a parse: an editor asks for
// completions precisely when the query is half-written, so this must work on
// input that does not parse.
func (c *Compiler) Suggest(input string, cursor int) []Suggestion {
	runes := []rune(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}

	// The prefix is defined lexically rather than from the token stream:
	// "web-1" lexes as several tokens, and the user typing it means one thing.
	start := cursor
	for start > 0 && !isSuggestBoundary(runes[start-1]) {
		start--
	}
	span := Span{Start: start, End: cursor}
	prefix := string(runes[start:cursor])
	if strings.HasPrefix(prefix, `"`) {
		prefix = prefix[1:]
	}

	state, field, openParens := c.walkState(input, start)

	switch state {
	case wantField:
		if out := c.fieldSuggestions(prefix, span); len(out) > 0 || prefix == "" {
			return out
		}
		return c.fallbackSuggestions(prefix, span)
	case wantOperator:
		return c.operatorSuggestions(field, prefix, span)
	case wantValue:
		return c.valueSuggestions(field, prefix, span)
	default:
		return c.keywordSuggestions(prefix, span, openParens)
	}
}

func isSuggestBoundary(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '(' || r == ')'
}

// walkState determines which token class is expected at the given offset, from
// the tokens entirely before it.
func (c *Compiler) walkState(input string, upTo int) (suggestState, *Field, int) {
	toks, _ := ast.Lex(input)
	state := wantField
	var field *Field
	openParens := 0

	for _, t := range toks {
		if t.Kind == ast.EOF || t.Span.End > upTo {
			break
		}
		switch t.Kind {
		case ast.LPAREN:
			openParens++
			state = wantField
		case ast.RPAREN:
			if openParens > 0 {
				openParens--
			}
			state = wantKeyword
		case ast.AND, ast.OR, ast.NOT:
			state = wantField
		case ast.IDENT:
			if state == wantField {
				field = c.fields[asciiLower(t.Text)]
				state = wantOperator
			} else {
				state = wantKeyword
			}
		case ast.OP:
			state = wantValue
		case ast.STRING, ast.NUMBER, ast.TRUE, ast.FALSE:
			state = wantKeyword
		}
	}
	return state, field, openParens
}

// fieldSuggestions orders exact match, then prefix match, then substring match,
// alphabetically within each group.
func (c *Compiler) fieldSuggestions(prefix string, span Span) []Suggestion {
	type ranked struct {
		field Field
		group int
	}
	needle := asciiLower(prefix)
	var matches []ranked
	for _, f := range c.schema.Fields {
		name := asciiLower(f.Name)
		switch {
		case name == needle:
			matches = append(matches, ranked{f, 0})
		case strings.HasPrefix(name, needle):
			matches = append(matches, ranked{f, 1})
		case strings.Contains(name, needle):
			matches = append(matches, ranked{f, 2})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].group != matches[j].group {
			return matches[i].group < matches[j].group
		}
		return asciiLower(matches[i].field.Name) < asciiLower(matches[j].field.Name)
	})

	out := make([]Suggestion, 0, len(matches))
	for _, m := range matches {
		out = append(out, Suggestion{
			Text:        asciiLower(m.field.Name),
			Kind:        SuggestField,
			Detail:      m.field.Description,
			ReplaceSpan: span,
		})
	}
	return out
}

// operatorSuggestions preserves declared order: a schema author who wrote
// ["=", "!="] ordered them for a reason.
func (c *Compiler) operatorSuggestions(f *Field, prefix string, span Span) []Suggestion {
	if f == nil {
		return []Suggestion{}
	}
	out := make([]Suggestion, 0, 4)
	for _, op := range f.operators() {
		if matchesPrefix(op, prefix) {
			out = append(out, Suggestion{Text: op, Kind: SuggestOperator, ReplaceSpan: span})
		}
	}
	return out
}

// valueSuggestions offers enum values and booleans. Other types are free text
// and offer nothing.
func (c *Compiler) valueSuggestions(f *Field, prefix string, span Span) []Suggestion {
	out := []Suggestion{}
	if f == nil {
		return out
	}
	var values []string
	switch f.Type {
	case TypeEnum:
		values = c.valuesOf(f, c.dynamic)
	case TypeBoolean:
		values = []string{"true", "false"}
	}
	for _, v := range values {
		if matchesPrefix(v, prefix) {
			out = append(out, Suggestion{Text: v, Kind: SuggestValue, ReplaceSpan: span})
		}
	}
	return out
}

func (c *Compiler) keywordSuggestions(prefix string, span Span, openParens int) []Suggestion {
	out := []Suggestion{}
	for _, kw := range []string{"AND", "OR"} {
		if matchesPrefix(kw, prefix) {
			out = append(out, Suggestion{Text: kw, Kind: SuggestKeyword, ReplaceSpan: span})
		}
	}
	if openParens > 0 && matchesPrefix(")", prefix) {
		out = append(out, Suggestion{Text: ")", Kind: SuggestKeyword, ReplaceSpan: span})
	}
	return out
}

// fallbackSuggestions wraps a prefix that matches no field name into whole
// predicates against host-nominated fields, so that pasting an identifier into
// an empty filter bar gets somewhere (AGENTS.md §10.5).
func (c *Compiler) fallbackSuggestions(prefix string, span Span) []Suggestion {
	out := []Suggestion{}
	if prefix == "" {
		return out
	}

	var candidates []*Field
	seen := map[string]bool{}
	add := func(f *Field) {
		if f == nil || seen[asciiLower(f.Name)] {
			return
		}
		seen[asciiLower(f.Name)] = true
		candidates = append(candidates, f)
	}
	// A pasted uuid means an id lookup, whatever the configured fallbacks say.
	if isUUID(prefix) {
		for i := range c.schema.Fields {
			if c.schema.Fields[i].Type == TypeUUID {
				add(&c.schema.Fields[i])
			}
		}
	}
	for _, name := range c.schema.Options.FallbackFields {
		add(c.fields[asciiLower(name)])
	}

	for _, f := range candidates {
		for _, op := range []string{"=", "~"} {
			if !f.permits(op) {
				continue
			}
			out = append(out, Suggestion{
				Text:        asciiLower(f.Name) + " " + op + ` "` + quoteInner(prefix) + `"`,
				Kind:        SuggestExpression,
				Detail:      f.Description,
				ReplaceSpan: span,
			})
		}
	}
	return out
}

func matchesPrefix(candidate, prefix string) bool {
	if prefix == "" {
		return true
	}
	return strings.Contains(asciiLower(candidate), asciiLower(prefix))
}

// quoteInner escapes a value for display inside a suggested string literal.
func quoteInner(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
