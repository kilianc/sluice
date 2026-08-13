package ast

import (
	"sort"
	"strconv"
	"strings"
)

// Limits bound the work a single input may cause (AGENTS.md §4.2). They are
// enforced by the parser, never by the caller.
type Limits struct {
	MaxDepth      int
	MaxPredicates int
}

// FieldRef is an identifier that appeared in field position but whose predicate
// did not parse. Resolution still checks it, so that `EXISTS (SELECT 1 FROM t)`
// reports unknown_field on EXISTS rather than only a syntax error further right.
type FieldRef struct {
	Name string
	Span Span
}

// Result is everything a parse produces.
type Result struct {
	Node        *Node
	Orphans     []FieldRef
	Diagnostics []Diagnostic
}

// Parse builds an AST from a token stream (AGENTS.md §5).
//
// Parsing recovers: after a failed predicate it skips to the next AND, OR or
// closing parenthesis and resumes, so Validate can report every independent
// problem in one pass (AGENTS.md §9). Recovery never invents a node — an input
// that produced a diagnostic never compiles.
func Parse(toks []Token, lim Limits) Result {
	p := &parser{toks: toks, lim: lim}
	node := p.parseQuery()
	SortDiagnostics(p.diags)
	return Result{Node: node, Orphans: p.orphans, Diagnostics: p.diags}
}

// ParseString lexes and parses in one step.
func ParseString(input string, lim Limits) Result {
	toks, diags := Lex(input)
	res := Parse(toks, lim)
	res.Diagnostics = append(diags, res.Diagnostics...)
	SortDiagnostics(res.Diagnostics)
	return res
}

// SortDiagnostics orders diagnostics by position so that "the first diagnostic"
// means the leftmost problem regardless of which stage found it.
func SortDiagnostics(d []Diagnostic) {
	sort.SliceStable(d, func(i, j int) bool {
		if d[i].Span.Start != d[j].Span.Start {
			return d[i].Span.Start < d[j].Span.Start
		}
		return d[i].Span.End < d[j].Span.End
	})
}

type parser struct {
	toks    []Token
	pos     int
	lim     Limits
	depth   int
	preds   int
	diags   []Diagnostic
	orphans []FieldRef
	fatal   bool
}

func (p *parser) peek() Token { return p.toks[p.pos] }

func (p *parser) next() Token {
	t := p.toks[p.pos]
	if t.Kind != EOF {
		p.pos++
	}
	return t
}

func (p *parser) diag(code string, span Span, msg string) {
	p.diags = append(p.diags, Diagnostic{Code: code, Message: msg, Span: span})
}

func (p *parser) parseQuery() *Node {
	if p.peek().Kind == EOF {
		return nil
	}
	node := p.parseExpr()
	for !p.fatal && p.peek().Kind != EOF {
		t := p.peek()
		switch t.Kind {
		case RPAREN:
			p.diag(CodeUnbalancedParen, t.Span, "no matching '('")
			p.next()
		case AND, OR:
			// Recovered mid-query: keep parsing for diagnostics, but the
			// partial tree is not usable, so its value is discarded.
			p.next()
			p.parseExpr()
		default:
			p.diag(CodeUnexpectedToken, t.Span, unexpectedMessage(t, "AND, OR or end of input"))
			p.next()
			p.recover()
		}
	}
	return node
}

func (p *parser) parseExpr() *Node { return p.parseOr() }

func (p *parser) parseOr() *Node {
	left := p.parseAnd()
	for !p.fatal && p.peek().Kind == OR {
		p.next()
		left = join("or", left, p.parseAnd())
	}
	return left
}

func (p *parser) parseAnd() *Node {
	left := p.parseUnary()
	for !p.fatal && p.peek().Kind == AND {
		p.next()
		left = join("and", left, p.parseUnary())
	}
	return left
}

// join builds a binary node, tolerating a failed operand so that recovery can
// continue without fabricating one.
func join(op string, left, right *Node) *Node {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	default:
		return Binary(op, left, right)
	}
}

func (p *parser) parseUnary() *Node {
	if p.peek().Kind == NOT {
		p.next()
		expr := p.parseUnary()
		if expr == nil {
			return nil
		}
		return Not(expr)
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() *Node {
	if p.peek().Kind != LPAREN {
		return p.parsePredicate()
	}
	open := p.next()
	if p.depth+1 > p.lim.MaxDepth {
		p.diag(CodeDepthExceeded, open.Span,
			"nesting deeper than "+strconv.Itoa(p.lim.MaxDepth)+" levels")
		p.fatal = true
		return nil
	}
	p.depth++
	inner := p.parseExpr()
	if p.peek().Kind == RPAREN {
		p.next()
	} else if !p.fatal {
		p.diag(CodeUnbalancedParen, open.Span, "no matching ')'")
	}
	p.depth--
	return inner
}

func (p *parser) parsePredicate() *Node {
	first := p.peek()
	if first.Kind != IDENT {
		p.fail(first, "a field name")
		return nil
	}
	p.next()

	opTok := p.peek()
	if opTok.Kind != OP {
		p.orphan(first)
		p.fail(opTok, "an operator")
		return nil
	}
	p.next()

	valTok := p.peek()
	if !valTok.IsValue() {
		p.orphan(first)
		p.fail(valTok, "a quoted string, number or boolean")
		return nil
	}
	p.next()

	node := &Node{
		Kind:      KindPredicate,
		Field:     strings.ToLower(first.Text),
		Op:        opTok.Text,
		Value:     literalOf(valTok),
		Span:      Span{first.Span.Start, valTok.Span.End},
		FieldSpan: first.Span,
		OpSpan:    opTok.Span,
		ValueSpan: valTok.Span,
		parsed:    true,
	}

	p.preds++
	if p.preds > p.lim.MaxPredicates {
		p.diag(CodeTooManyPredicates, node.Span,
			"more than "+strconv.Itoa(p.lim.MaxPredicates)+" predicates")
		p.fatal = true
		return nil
	}
	return node
}

func literalOf(t Token) Literal {
	switch t.Kind {
	case NUMBER:
		return Literal{Type: LitNumber, Num: t.Num}
	case TRUE:
		return Literal{Type: LitBoolean, Bool: true}
	case FALSE:
		return Literal{Type: LitBoolean, Bool: false}
	default:
		return Literal{Type: LitString, Str: t.Str}
	}
}

// fail reports a token that cannot continue the current production and skips to
// the next recovery point.
func (p *parser) fail(t Token, want string) {
	if t.Kind == EOF {
		p.diag(CodeUnexpectedEOF, t.Span, "expected "+want+", found end of input")
		p.fatal = true
		return
	}
	p.diag(CodeUnexpectedToken, t.Span, unexpectedMessage(t, want))
	p.recover()
}

// recover skips tokens until the next AND, OR or ')' — the points at which the
// grammar can resume — leaving that token for the caller (AGENTS.md §9).
func (p *parser) recover() {
	for {
		switch p.peek().Kind {
		case AND, OR, RPAREN, EOF:
			return
		}
		p.next()
	}
}

func (p *parser) orphan(t Token) {
	p.orphans = append(p.orphans, FieldRef{Name: strings.ToLower(t.Text), Span: t.Span})
}

func unexpectedMessage(t Token, want string) string {
	got := t.Text
	if t.Kind == STRING {
		got = strconv.Quote(t.Str)
	}
	return "expected " + want + ", found " + got
}
