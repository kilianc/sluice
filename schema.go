// Package sluice compiles a small, configurable filter language into a SQL
// predicate with every value bound as a parameter.
//
// The schema is the whole configuration: one field declaration drives the
// parser, the compiler and the editor. Column SQL, table aliases and sort
// expressions come from the schema and nowhere else — no part of an input
// string is ever treated as an identifier.
package sluice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kilianc/sluice/ast"
)

// FieldType is the declared type of a field. It selects the literal a predicate
// accepts, the operators it permits, and the SQL shape it emits.
type FieldType string

const (
	TypeString    FieldType = "string"
	TypeEnum      FieldType = "enum"
	TypeBoolean   FieldType = "boolean"
	TypeNumber    FieldType = "number"
	TypeUUID      FieldType = "uuid"
	TypeDuration  FieldType = "duration"
	TypeTimestamp FieldType = "timestamp"
)

// Operator is a canonical operator spelling (AGENTS.md §3.1).
type Operator string

// EmitFunc is a host-supplied emitter for a field that is not a column
// comparison — "is this machine running any operation" may be an EXISTS over a
// JSONB column (AGENTS.md §8.4).
//
// Builder offers no way to write a value into SQL text, so invariant 1 holds by
// construction even here. Custom emitters are native-code only: they cannot
// appear in a JSON schema and therefore cannot cross a trust boundary.
type EmitFunc func(b *Builder, op Operator, v Value) error

// Field is one queryable name.
type Field struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Column      string    `json:"column,omitempty"`
	Description string    `json:"description,omitempty"`
	Values      []string  `json:"values,omitempty"`
	Dynamic     bool      `json:"dynamic,omitempty"`
	Operators   []string  `json:"operators,omitempty"`

	// CaseInsensitive overrides Options.CaseInsensitive for this field.
	CaseInsensitive *bool `json:"caseInsensitive,omitempty"`

	// Emit replaces Column with host-authored SQL. Native only.
	Emit EmitFunc `json:"-"`
}

// Sort is a permitted ORDER BY key. Sort expressions are host-supplied and are
// never derived from input (AGENTS.md §8.6).
type Sort struct {
	Key string `json:"key"`
	SQL string `json:"sql,omitempty"`
}

// Options carry schema-wide defaults and the limits that bound the work any one
// input can cause (AGENTS.md §4.2).
type Options struct {
	CaseInsensitive *bool    `json:"caseInsensitive,omitempty"`
	MaxLength       int      `json:"maxLength,omitempty"`
	MaxDepth        int      `json:"maxDepth,omitempty"`
	MaxPredicates   int      `json:"maxPredicates,omitempty"`
	FallbackFields  []string `json:"fallbackFields,omitempty"`
}

// Schema is the host application's field declaration.
type Schema struct {
	Name    string  `json:"name,omitempty"`
	Options Options `json:"options"`
	Fields  []Field `json:"fields"`
	Sorts   []Sort  `json:"sorts,omitempty"`
}

// Defaults for Options (AGENTS.md §4.2).
const (
	DefaultMaxLength     = 4096
	DefaultMaxDepth      = 16
	DefaultMaxPredicates = 64
)

func (o Options) maxLength() int {
	if o.MaxLength > 0 {
		return o.MaxLength
	}
	return DefaultMaxLength
}

func (o Options) maxDepth() int {
	if o.MaxDepth > 0 {
		return o.MaxDepth
	}
	return DefaultMaxDepth
}

func (o Options) maxPredicates() int {
	if o.MaxPredicates > 0 {
		return o.MaxPredicates
	}
	return DefaultMaxPredicates
}

func (o Options) caseInsensitive() bool {
	if o.CaseInsensitive != nil {
		return *o.CaseInsensitive
	}
	return true
}

// LoadSchema decodes the canonical JSON form (AGENTS.md §4.3) and validates it.
func LoadSchema(data []byte) (Schema, error) {
	var s Schema
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Schema{}, &SchemaError{Diagnostics: []Diagnostic{{
			Code:    ast.CodeSchemaInvalid,
			Message: "schema is not valid JSON: " + err.Error(),
		}}}
	}
	if err := s.Validate(); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// SchemaError reports every problem with a schema at once, so a host fixes its
// configuration in one pass rather than one error at a time.
type SchemaError struct {
	Diagnostics []Diagnostic
}

func (e *SchemaError) Error() string {
	msgs := make([]string, 0, len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		msgs = append(msgs, d.Message)
	}
	return "sluice: invalid schema: " + strings.Join(msgs, "; ")
}

var reservedNames = map[string]bool{
	"and": true, "or": true, "not": true, "true": true, "false": true,
}

// Validate checks the schema against AGENTS.md §4. Every problem is reported as
// a schema_invalid diagnostic.
func (s Schema) Validate() error {
	var diags []Diagnostic
	bad := func(format string, args ...any) {
		diags = append(diags, Diagnostic{
			Code:    ast.CodeSchemaInvalid,
			Message: fmt.Sprintf(format, args...),
		})
	}

	seen := map[string]bool{}
	for _, f := range s.Fields {
		name := asciiLower(f.Name)
		switch {
		case name == "":
			bad("field name is empty")
			continue
		case !validFieldName(name):
			bad("field %q must match [a-z_][a-z0-9_]*", f.Name)
		case reservedNames[name]:
			bad("field %q uses a reserved name", f.Name)
		}
		if seen[name] {
			bad("field %q is declared twice", name)
		}
		seen[name] = true

		if _, ok := defaultOperators[f.Type]; !ok {
			bad("field %q has unknown type %q", name, f.Type)
			continue
		}
		if f.Column == "" && f.Emit == nil {
			bad("field %q needs a column or a custom emitter", name)
		}
		if f.Column != "" && f.Emit != nil {
			bad("field %q declares both a column and a custom emitter", name)
		}
		if f.Type != TypeEnum {
			if len(f.Values) > 0 {
				bad("field %q is %s, so it cannot declare values", name, f.Type)
			}
			if f.Dynamic {
				bad("field %q is %s, so it cannot be dynamic", name, f.Type)
			}
		}
		if f.Dynamic && len(f.Values) > 0 {
			bad("field %q is dynamic, so its values are supplied per request", name)
		}
		allowed := defaultOperators[f.Type]
		for _, op := range f.Operators {
			if !contains(allowed, op) {
				bad("field %q permits %q, which is not one of %s for %s",
					name, op, strings.Join(allowed, " "), f.Type)
			}
		}
	}

	sortKeys := map[string]bool{}
	for _, srt := range s.Sorts {
		if srt.Key == "" {
			bad("sort key is empty")
			continue
		}
		if sortKeys[srt.Key] {
			bad("sort key %q is declared twice", srt.Key)
		}
		sortKeys[srt.Key] = true
		if srt.SQL == "" {
			bad("sort key %q needs an sql expression", srt.Key)
		}
	}

	for _, name := range s.Options.FallbackFields {
		if !seen[asciiLower(name)] {
			bad("fallback field %q is not a declared field", name)
		}
	}

	if len(diags) > 0 {
		return &SchemaError{Diagnostics: diags}
	}
	return nil
}

func validFieldName(name string) bool {
	for i, c := range name {
		switch {
		case c == '_':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return name != ""
}

// defaultOperators is the permitted operator set per type, in the order an
// editor should offer them (AGENTS.md §4.5). Order is part of the contract:
// "=" should never sort below "!=".
var defaultOperators = map[FieldType][]string{
	TypeString:    {"=", "!=", "~", "!~"},
	TypeEnum:      {"=", "!=", "~", "!~"},
	TypeBoolean:   {"="},
	TypeNumber:    {"=", "!=", "<", "<=", ">", ">="},
	TypeUUID:      {"=", "!="},
	TypeDuration:  {"<", "<=", ">", ">="},
	TypeTimestamp: {"<", "<=", ">", ">="},
}

// operators returns the field's permitted operators in declared order. An
// explicit list replaces the default entirely.
func (f Field) operators() []string {
	if len(f.Operators) > 0 {
		return f.Operators
	}
	return defaultOperators[f.Type]
}

func (f Field) permits(op string) bool { return contains(f.operators(), op) }

func (f Field) foldsCase(o Options) bool {
	if f.CaseInsensitive != nil {
		return *f.CaseInsensitive
	}
	return o.caseInsensitive()
}

// PublicSchema returns the schema as it is served to a browser: field names,
// types, values and descriptions, with column SQL and sort expressions removed
// and dynamic values resolved (AGENTS.md §4.3). The client drives autocomplete
// with it and has no use for your table aliases.
func (c *Compiler) PublicSchema(dynamic map[string][]string) Schema {
	out := Schema{
		Name:    c.schema.Name,
		Options: c.schema.Options,
		Fields:  make([]Field, 0, len(c.schema.Fields)),
	}
	for _, f := range c.schema.Fields {
		pub := f
		pub.Column = ""
		pub.Emit = nil
		if f.Dynamic {
			pub.Values = append([]string(nil), dynamic[asciiLower(f.Name)]...)
		}
		out.Fields = append(out.Fields, pub)
	}
	for _, s := range c.schema.Sorts {
		out.Sorts = append(out.Sorts, Sort{Key: s.Key})
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// asciiLower lowercases ASCII letters only. Non-ASCII case folding differs
// between every database collation and every language's toLowerCase, and a
// filter bar cannot afford that disagreement (AGENTS.md §8.2).
func asciiLower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

// nearest returns up to 4 field names within Levenshtein distance 3 of name,
// ordered by distance then alphabetically (AGENTS.md §7).
func (c *Compiler) nearest(name string) []string {
	type cand struct {
		name string
		dist int
	}
	var cands []cand
	for _, f := range c.schema.Fields {
		fn := asciiLower(f.Name)
		if d := levenshtein(name, fn); d <= 3 {
			cands = append(cands, cand{fn, d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].name < cands[j].name
	})
	if len(cands) > 4 {
		cands = cands[:4]
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.name)
	}
	return out
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
