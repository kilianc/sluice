package sluice

import (
	"errors"
	"strings"

	"github.com/kilianc/sluice/ast"
)

// Builder accumulates emitted SQL and its bound arguments. It is what a custom
// emitter writes through (AGENTS.md §8.4).
//
// There is deliberately no method that writes a value into SQL text. WriteSQL
// takes host-authored fragments; Bind takes a value and hands back a
// placeholder. Invariant 1 therefore holds by construction, including for
// host-supplied emitters.
type Builder struct {
	dialect Dialect
	sql     strings.Builder
	args    []any
}

// WriteSQL appends a host-authored SQL fragment verbatim.
func (b *Builder) WriteSQL(s string) { b.sql.WriteString(s) }

// Bind appends an argument and returns the placeholder that references it.
func (b *Builder) Bind(v any) string {
	b.args = append(b.args, v)
	return b.dialect.Placeholder(len(b.args))
}

// Dialect returns the dialect being emitted for, so a custom emitter can spell
// casts and functions correctly.
func (b *Builder) Dialect() Dialect { return b.dialect }

// LikePattern turns a value into a LIKE argument: metacharacters escaped, then
// wrapped in wildcards. The emitted SQL carries an explicit ESCAPE clause.
// Without this, name ~ "%" matches every row (AGENTS.md §8.2).
func LikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// emit walks a resolved tree and produces SQL, arguments and touched fields.
// Resolution runs here too, so an AST that arrived over the network is checked
// exactly as a parsed one is.
func (c *Compiler) emit(node *ast.Node) (Result, []Diagnostic) {
	res := Result{SQL: "", Args: []any{}, Fields: []string{}, AST: node}
	if node == nil {
		// Empty input compiles to empty. The host decides what an absent
		// predicate means; the compiler never invents 1=1 (AGENTS.md §8.5).
		return res, nil
	}

	b := &Builder{dialect: c.dialect}
	var diags []Diagnostic
	seen := map[string]bool{}

	var walk func(n *ast.Node)
	walk = func(n *ast.Node) {
		switch n.Kind {
		case ast.KindBinary:
			op := "AND"
			if n.Op == "or" {
				op = "OR"
			}
			b.WriteSQL("(")
			walk(n.Left)
			b.WriteSQL(" " + op + " ")
			walk(n.Right)
			b.WriteSQL(")")
		case ast.KindNot:
			b.WriteSQL("(NOT ")
			walk(n.Expr)
			b.WriteSQL(")")
		default:
			if d := c.emitPredicate(b, n); d != nil {
				diags = append(diags, *d)
				return
			}
			if !seen[n.Field] {
				seen[n.Field] = true
				res.Fields = append(res.Fields, n.Field)
			}
		}
	}
	walk(node)

	if len(diags) > 0 {
		sortDiagnostics(diags)
		return Result{}, diags
	}
	res.SQL = b.sql.String()
	if b.args != nil {
		res.Args = b.args
	}
	return res, nil
}

// emitPredicate emits one comparison, with no enclosing parentheses.
func (c *Compiler) emitPredicate(b *Builder, n *ast.Node) *Diagnostic {
	f, ok := c.fields[n.Field]
	if !ok {
		d := c.unknownField(n.Field, n.SpanFor("field"))
		return &d
	}
	if !f.permits(n.Op) {
		return &Diagnostic{
			Code: ast.CodeUnknownOperatorForField,
			Message: "field " + f.Name + " does not support " + n.Op +
				"; it supports " + strings.Join(f.operators(), " "),
			Span: n.SpanFor("op"),
		}
	}
	v, msg, err := c.coerce(f, n.Value, c.dynamic)
	if err != nil {
		code := ast.CodeInvalidValueForField
		if errors.Is(err, errInvalidDuration) {
			code = ast.CodeInvalidDuration
		}
		return &Diagnostic{Code: code, Message: "field " + f.Name + " " + msg, Span: n.SpanFor("value")}
	}

	if f.Emit != nil {
		if err := f.Emit(b, Operator(n.Op), v); err != nil {
			return &Diagnostic{
				Code:    ast.CodeInvalidValueForField,
				Message: "field " + f.Name + ": " + err.Error(),
				Span:    n.SpanFor("value"),
			}
		}
		return nil
	}

	col := f.Column
	fold := f.foldsCase(c.schema.Options)

	switch f.Type {
	case TypeString, TypeEnum:
		target := col
		if fold {
			target = "LOWER(" + col + ")"
		}
		switch n.Op {
		case "~", "!~":
			like := "LIKE"
			if n.Op == "!~" {
				like = "NOT LIKE"
			}
			b.WriteSQL(target + " " + like + " ")
			b.WriteSQL(b.Bind(LikePattern(v.String())))
			b.WriteSQL(c.dialect.LikeEscapeClause())
		default:
			b.WriteSQL(target + " " + n.Op + " ")
			b.WriteSQL(b.Bind(v.Arg()))
		}

	case TypeBoolean:
		b.WriteSQL(col + " " + n.Op + " ")
		b.WriteSQL(b.Bind(c.dialect.BoolArg(v.Bool())))

	case TypeNumber:
		b.WriteSQL(col + " " + n.Op + " ")
		b.WriteSQL(b.Bind(v.Number()))

	case TypeUUID:
		b.WriteSQL(col + " " + n.Op + " ")
		b.WriteSQL(b.Bind(v.String()) + c.dialect.UUIDCast())

	case TypeDuration:
		b.WriteSQL(c.dialect.AgeSeconds(col) + " " + n.Op + " ")
		b.WriteSQL(b.Bind(v.Seconds()))

	case TypeTimestamp:
		b.WriteSQL(col + " " + n.Op + " ")
		b.WriteSQL(b.Bind(v.String()) + c.dialect.TimestampCast())
	}
	return nil
}
