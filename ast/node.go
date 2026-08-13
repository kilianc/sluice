package ast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Kind names an AST node type. The spellings are the wire format (AGENTS.md §6).
type Kind string

const (
	KindBinary    Kind = "binary"
	KindNot       Kind = "not"
	KindPredicate Kind = "predicate"
)

// LitType is the type of a literal as written, not of the field it is compared
// against: a duration field receives a string literal.
type LitType string

const (
	LitString  LitType = "string"
	LitNumber  LitType = "number"
	LitBoolean LitType = "boolean"
)

// Literal is a parsed value in predicate position.
type Literal struct {
	Type LitType
	Str  string
	Num  float64
	Bool bool
}

// Value returns the literal as a JSON-encodable Go value.
func (l Literal) Value() any {
	switch l.Type {
	case LitNumber:
		return l.Num
	case LitBoolean:
		return l.Bool
	default:
		return l.Str
	}
}

// String renders the literal the way a user would type it. Used for messages
// only; nothing here reaches SQL.
func (l Literal) String() string {
	switch l.Type {
	case LitNumber:
		return fmt.Sprintf("%v", l.Num)
	case LitBoolean:
		if l.Bool {
			return "true"
		}
		return "false"
	default:
		return `"` + l.Str + `"`
	}
}

// Node is an expression node. It is the wire format between a browser and a
// server, so its JSON encoding is normative (AGENTS.md §6).
//
// A Node reaching a compiler from the network is exactly as untrusted as an
// input string: it names fields, never columns, and every value on it goes
// through the same coercion and the same parameter binding.
type Node struct {
	Kind Kind

	// binary
	Op          string // "and" | "or" for binary; the operator spelling for predicate
	Left, Right *Node

	// not
	Expr *Node

	// predicate
	Field string // lowercased schema field name
	Value Literal

	// Span covers the whole node. FieldSpan, OpSpan and ValueSpan are set by the
	// parser to position resolution diagnostics precisely; a node decoded from
	// JSON has none of them, and diagnostics fall back to Span.
	Span      Span
	FieldSpan Span
	OpSpan    Span
	ValueSpan Span
	parsed    bool
}

// Binary builds an AND/OR node.
func Binary(op string, left, right *Node) *Node {
	return &Node{Kind: KindBinary, Op: op, Left: left, Right: right}
}

// Not builds a negation node.
func Not(expr *Node) *Node { return &Node{Kind: KindNot, Expr: expr} }

// SpanFor returns the most precise span available for the given part of a
// predicate node.
func (n *Node) SpanFor(part string) Span {
	if !n.parsed {
		return n.Span
	}
	switch part {
	case "field":
		return n.FieldSpan
	case "op":
		return n.OpSpan
	case "value":
		return n.ValueSpan
	default:
		return n.Span
	}
}

// Depth returns the expression nesting depth of the tree, counting the node
// itself as 1. Used to apply the maxDepth limit to a decoded AST the same way
// the parser applies it to text (AGENTS.md §6).
func (n *Node) Depth() int {
	if n == nil {
		return 0
	}
	switch n.Kind {
	case KindBinary:
		l, r := n.Left.Depth(), n.Right.Depth()
		if r > l {
			l = r
		}
		return l + 1
	case KindNot:
		return n.Expr.Depth() + 1
	default:
		return 1
	}
}

type jsonNode struct {
	Kind  Kind         `json:"kind"`
	Op    string       `json:"op,omitempty"`
	Left  *Node        `json:"left,omitempty"`
	Right *Node        `json:"right,omitempty"`
	Expr  *Node        `json:"expr,omitempty"`
	Field string       `json:"field,omitempty"`
	Value *jsonLiteral `json:"value,omitempty"`
	Span  *Span        `json:"span,omitempty"`
}

type jsonLiteral struct {
	Type  LitType         `json:"type"`
	Value json.RawMessage `json:"value"`
}

// MarshalJSON emits the normative encoding. Spans are emitted on predicate
// nodes only: they are informational, and keeping them off the other node kinds
// keeps the wire form minimal.
func (n *Node) MarshalJSON() ([]byte, error) {
	out := jsonNode{Kind: n.Kind}
	switch n.Kind {
	case KindBinary:
		out.Op, out.Left, out.Right = n.Op, n.Left, n.Right
	case KindNot:
		out.Expr = n.Expr
	case KindPredicate:
		raw, err := json.Marshal(n.Value.Value())
		if err != nil {
			return nil, err
		}
		out.Op, out.Field = n.Op, n.Field
		out.Value = &jsonLiteral{Type: n.Value.Type, Value: raw}
		span := n.Span
		out.Span = &span
	default:
		return nil, fmt.Errorf("sluice/ast: unknown node kind %q", n.Kind)
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes a node, rejecting anything it does not recognize.
// Structural rejection happens here; schema-dependent rejection (unknown fields,
// nesting depth) happens in the compiler, which is the only thing that knows the
// schema (AGENTS.md §6).
func (n *Node) UnmarshalJSON(data []byte) error {
	var in jsonNode
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return err
	}
	*n = Node{Kind: in.Kind}
	switch in.Kind {
	case KindBinary:
		if in.Op != "and" && in.Op != "or" {
			return fmt.Errorf("sluice/ast: unknown binary operator %q", in.Op)
		}
		if in.Left == nil || in.Right == nil {
			return errors.New("sluice/ast: binary node needs both operands")
		}
		n.Op, n.Left, n.Right = in.Op, in.Left, in.Right
	case KindNot:
		if in.Expr == nil {
			return errors.New("sluice/ast: not node needs an expression")
		}
		n.Expr = in.Expr
	case KindPredicate:
		if in.Field == "" {
			return errors.New("sluice/ast: predicate node needs a field")
		}
		if !IsOperator(in.Op) {
			return fmt.Errorf("sluice/ast: unknown operator %q", in.Op)
		}
		if in.Value == nil {
			return errors.New("sluice/ast: predicate node needs a value")
		}
		lit, err := decodeLiteral(*in.Value)
		if err != nil {
			return err
		}
		n.Field, n.Op, n.Value = in.Field, in.Op, lit
	default:
		return fmt.Errorf("sluice/ast: unknown node kind %q", in.Kind)
	}
	if in.Span != nil {
		n.Span = *in.Span
	}
	return nil
}

func decodeLiteral(in jsonLiteral) (Literal, error) {
	lit := Literal{Type: in.Type}
	switch in.Type {
	case LitString:
		if err := json.Unmarshal(in.Value, &lit.Str); err != nil {
			return lit, err
		}
	case LitNumber:
		if err := json.Unmarshal(in.Value, &lit.Num); err != nil {
			return lit, err
		}
	case LitBoolean:
		if err := json.Unmarshal(in.Value, &lit.Bool); err != nil {
			return lit, err
		}
	default:
		return lit, fmt.Errorf("sluice/ast: unknown literal type %q", in.Type)
	}
	return lit, nil
}

// IsOperator reports whether s is one of the canonical operator spellings.
func IsOperator(s string) bool {
	switch s {
	case "=", "!=", "~", "!~", "<", "<=", ">", ">=":
		return true
	}
	return false
}
