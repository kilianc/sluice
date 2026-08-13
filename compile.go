package sluice

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/kilianc/sluice/ast"
)

// Diagnostic is a problem found in an input, positioned in it. Codes are stable
// API; messages are for display and are free to improve (AGENTS.md §9).
type Diagnostic = ast.Diagnostic

// Span is a half-open range of 0-based codepoint offsets.
type Span = ast.Span

// Error is returned by Compile and OrderBy. It carries the first diagnostic;
// use Validate to get all of them.
type Error struct {
	Diagnostic Diagnostic
}

func (e *Error) Error() string {
	if e.Diagnostic.Message == "" {
		return "sluice: " + e.Diagnostic.Code
	}
	return "sluice: " + e.Diagnostic.Code + ": " + e.Diagnostic.Message
}

// Compiler compiles inputs against one schema and one dialect. It is immutable
// and safe for concurrent use; WithDynamic returns a view rather than mutating.
type Compiler struct {
	schema  Schema
	dialect Dialect
	fields  map[string]*Field
	sorts   map[string]Sort
	dynamic map[string][]string
}

// New validates the schema and binds it to a dialect.
func New(schema Schema, dialect Dialect) (*Compiler, error) {
	if dialect == nil {
		return nil, errors.New("sluice: dialect is required")
	}
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	c := &Compiler{
		schema:  schema,
		dialect: dialect,
		fields:  make(map[string]*Field, len(schema.Fields)),
		sorts:   make(map[string]Sort, len(schema.Sorts)),
	}
	for i := range c.schema.Fields {
		f := &c.schema.Fields[i]
		c.fields[asciiLower(f.Name)] = f
	}
	for _, s := range schema.Sorts {
		c.sorts[s.Key] = s
	}
	return c, nil
}

// Schema returns the compiler's schema.
func (c *Compiler) Schema() Schema { return c.schema }

// Dialect returns the compiler's dialect.
func (c *Compiler) Dialect() Dialect { return c.dialect }

// WithDynamic returns a view of the compiler that resolves dynamic enum values
// from the given map for the duration of a request. The values are not cached
// on the compiler (AGENTS.md §4.4).
func (c *Compiler) WithDynamic(values map[string][]string) *Compiler {
	view := *c
	view.dynamic = make(map[string][]string, len(values))
	for k, v := range values {
		view.dynamic[asciiLower(k)] = v
	}
	return &view
}

// Result is a compiled predicate.
type Result struct {
	// SQL is a WHERE fragment, empty for empty input. It contains no value
	// from the input — only schema-supplied SQL and placeholders.
	SQL string
	// Args are the bound parameters, in placeholder order.
	Args []any
	// Fields names the schema fields the predicate touched, in traversal
	// order, deduplicated — enough to prune joins.
	Fields []string
	// AST is the parsed tree, suitable for transport (AGENTS.md §12 Mode B).
	AST *ast.Node
}

// Compile parses, resolves and emits an input string. It returns the first
// diagnostic as an error and, in that case, no SQL.
func (c *Compiler) Compile(input string) (Result, error) {
	node, diags := c.parse(input)
	if len(diags) == 0 {
		var res Result
		res, diags = c.emit(node)
		if len(diags) == 0 {
			return res, nil
		}
	}
	return Result{}, &Error{Diagnostic: diags[0]}
}

// CompileAST compiles an AST that did not come from this process — the
// untrusted-AST entry point of AGENTS.md §12 Mode B. Decoding an untrusted AST
// is subject to exactly the same validation as parsing untrusted text: the node
// names fields, never columns, and every value goes through the same coercion.
func (c *Compiler) CompileAST(node *ast.Node) (Result, error) {
	if d := c.checkAST(node); d != nil {
		return Result{}, &Error{Diagnostic: *d}
	}
	res, diags := c.emit(node)
	if len(diags) > 0 {
		return Result{}, &Error{Diagnostic: diags[0]}
	}
	return res, nil
}

func (c *Compiler) checkAST(node *ast.Node) *Diagnostic {
	if node.Depth() > c.schema.Options.maxDepth() {
		return &Diagnostic{
			Code:    ast.CodeDepthExceeded,
			Message: "expression nests deeper than the schema permits",
			Span:    node.Span,
		}
	}
	var found *Diagnostic
	reject := func(n *ast.Node, msg string) {
		if found == nil {
			found = &Diagnostic{Code: ast.CodeUnexpectedToken, Message: msg, Span: n.Span}
		}
	}
	// A node assembled in code is checked as strictly as one decoded from JSON:
	// emission must never meet a shape it would have to guess about.
	var walk func(*ast.Node)
	walk = func(n *ast.Node) {
		if n == nil || found != nil {
			return
		}
		switch n.Kind {
		case ast.KindBinary:
			if n.Op != "and" && n.Op != "or" {
				reject(n, "unknown binary operator "+n.Op)
				return
			}
			if n.Left == nil || n.Right == nil {
				reject(n, "binary node is missing an operand")
				return
			}
			walk(n.Left)
			walk(n.Right)
		case ast.KindNot:
			if n.Expr == nil {
				reject(n, "not node is missing its expression")
				return
			}
			walk(n.Expr)
		case ast.KindPredicate:
			if !ast.IsOperator(n.Op) {
				reject(n, "unknown operator "+n.Op)
			}
		default:
			reject(n, "unknown node kind "+string(n.Kind))
		}
	}
	walk(node)
	return found
}

// Parse lexes and parses an input under the schema's limits, without resolving
// it against the fields. The resulting AST is the wire format of AGENTS.md §6.
func (c *Compiler) Parse(input string) ast.Result {
	if n := utf8.RuneCountInString(input); n > c.schema.Options.maxLength() {
		return ast.Result{Diagnostics: []Diagnostic{{
			Code:    ast.CodeInputTooLong,
			Message: "input is longer than the schema permits",
			Span:    Span{Start: 0, End: n},
		}}}
	}
	return ast.ParseString(input, c.limits())
}

func (c *Compiler) limits() ast.Limits {
	return ast.Limits{
		MaxDepth:      c.schema.Options.maxDepth(),
		MaxPredicates: c.schema.Options.maxPredicates(),
	}
}

// Validate returns every independent diagnostic, so an editor can underline all
// of them in one pass (AGENTS.md §9).
func (c *Compiler) Validate(input string) []Diagnostic {
	_, diags := c.parse(input)
	return diags
}

// parse runs the input through lexing, parsing and resolution, returning the
// tree and every diagnostic found, ordered by position.
func (c *Compiler) parse(input string) (*ast.Node, []Diagnostic) {
	if n := utf8.RuneCountInString(input); n > c.schema.Options.maxLength() {
		return nil, []Diagnostic{{
			Code:    ast.CodeInputTooLong,
			Message: "input is longer than the schema permits",
			Span:    Span{Start: 0, End: n},
		}}
	}
	res := ast.ParseString(input, c.limits())
	diags := res.Diagnostics
	diags = append(diags, c.resolve(res.Node)...)
	// An identifier in field position whose predicate never parsed is still
	// checked, so `EXISTS (SELECT 1 FROM t)` reports unknown_field on EXISTS.
	for _, ref := range res.Orphans {
		if _, ok := c.fields[ref.Name]; !ok {
			diags = append(diags, c.unknownField(ref.Name, ref.Span))
		}
	}
	sortDiagnostics(diags)
	return res.Node, diags
}

// resolve binds every predicate to a schema field, checking the operator and
// coercing the literal (AGENTS.md §7).
func (c *Compiler) resolve(n *ast.Node) []Diagnostic {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case ast.KindBinary:
		return append(c.resolve(n.Left), c.resolve(n.Right)...)
	case ast.KindNot:
		return c.resolve(n.Expr)
	}

	f, ok := c.fields[n.Field]
	if !ok {
		return []Diagnostic{c.unknownField(n.Field, n.SpanFor("field"))}
	}
	if !f.permits(n.Op) {
		return []Diagnostic{{
			Code: ast.CodeUnknownOperatorForField,
			Message: "field " + f.Name + " does not support " + n.Op +
				"; it supports " + strings.Join(f.operators(), " "),
			Span:        n.SpanFor("op"),
			Suggestions: append([]string(nil), f.operators()...),
		}}
	}
	if _, msg, err := c.coerce(f, n.Value, c.dynamic); err != nil {
		code := ast.CodeInvalidValueForField
		if errors.Is(err, errInvalidDuration) {
			code = ast.CodeInvalidDuration
		}
		return []Diagnostic{{
			Code:    code,
			Message: "field " + f.Name + " " + msg,
			Span:    n.SpanFor("value"),
		}}
	}
	return nil
}

func (c *Compiler) unknownField(name string, span Span) Diagnostic {
	d := Diagnostic{
		Code:        ast.CodeUnknownField,
		Message:     "unknown field " + name,
		Span:        span,
		Suggestions: c.nearest(name),
	}
	if len(d.Suggestions) > 0 {
		d.Message += "; did you mean " + strings.Join(d.Suggestions, ", ") + "?"
	}
	return d
}

// sortDiagnostics puts diagnostics in position order, so "the first
// diagnostic" is the leftmost problem whichever stage found it.
func sortDiagnostics(d []Diagnostic) { ast.SortDiagnostics(d) }
