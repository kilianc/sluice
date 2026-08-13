package ast

import (
	"strconv"
	"strings"
)

// Lex converts input into a token stream terminated by an EOF token, plus any
// lexical diagnostics (AGENTS.md §3).
//
// Lexing recovers rather than stopping: a malformed string literal still yields
// a STRING token so the parser can carry on and report the problems further
// along the line, and an unrecognized character is reported and skipped. Nothing
// unrecognized is ever carried into the token stream — invariant 2 starts here.
func Lex(input string) ([]Token, []Diagnostic) {
	l := &lexer{src: []rune(input)}
	l.run()
	return l.toks, l.diags
}

type lexer struct {
	src   []rune
	pos   int
	toks  []Token
	diags []Diagnostic
}

func (l *lexer) run() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case isSpace(c):
			l.pos++
		case c == '(':
			l.emit(LPAREN, "(", l.pos, l.pos+1)
			l.pos++
		case c == ')':
			l.emit(RPAREN, ")", l.pos, l.pos+1)
			l.pos++
		case c == '"':
			l.lexString()
		case isIdentStart(c):
			l.lexIdent()
		case isDigit(c) || (c == '-' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])):
			l.lexNumber()
		default:
			if n := l.operatorLen(); n > 0 {
				l.emit(OP, string(l.src[l.pos:l.pos+n]), l.pos, l.pos+n)
				l.pos += n
				continue
			}
			l.diag(CodeUnexpectedToken, Span{l.pos, l.pos + 1},
				"unexpected character "+strconv.QuoteRune(c))
			l.pos++
		}
	}
	l.emit(EOF, "", len(l.src), len(l.src))
}

// operatorLen returns the length of the operator at the cursor, matching
// longest-first so that != , !~ and <= are never split (AGENTS.md §3.1).
func (l *lexer) operatorLen() int {
	rest := l.src[l.pos:]
	if len(rest) >= 2 {
		switch string(rest[:2]) {
		case "!=", "!~", "<=", ">=":
			return 2
		}
	}
	switch rest[0] {
	case '=', '~', '<', '>':
		return 1
	}
	return 0
}

func (l *lexer) lexIdent() {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	text := string(l.src[start:l.pos])
	kind := IDENT
	switch strings.ToLower(text) {
	case "and":
		kind = AND
	case "or":
		kind = OR
	case "not":
		kind = NOT
	case "true":
		kind = TRUE
	case "false":
		kind = FALSE
	}
	l.emit(kind, text, start, l.pos)
}

func (l *lexer) lexNumber() {
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	if l.pos+1 < len(l.src) && l.src[l.pos] == '.' && isDigit(l.src[l.pos+1]) {
		l.pos++
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	text := string(l.src[start:l.pos])
	n, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// Unreachable for the pattern above, but a malformed number must not
		// become a passthrough token.
		l.diag(CodeUnexpectedToken, Span{start, l.pos}, "invalid number "+text)
		return
	}
	tok := Token{Kind: NUMBER, Text: text, Num: n, Span: Span{start, l.pos}}
	l.toks = append(l.toks, tok)
}

func (l *lexer) lexString() {
	start := l.pos
	l.pos++ // opening quote
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) {
			l.diag(CodeUnterminatedString, Span{start, len(l.src)},
				"string literal is not closed")
			l.emitString(sb.String(), start, len(l.src))
			return
		}
		c := l.src[l.pos]
		if c == '"' {
			l.pos++
			l.emitString(sb.String(), start, l.pos)
			return
		}
		if c != '\\' {
			sb.WriteRune(c)
			l.pos++
			continue
		}
		if l.pos+1 >= len(l.src) {
			l.diag(CodeUnterminatedString, Span{start, len(l.src)},
				"string literal is not closed")
			l.emitString(sb.String(), start, len(l.src))
			l.pos = len(l.src)
			return
		}
		switch e := l.src[l.pos+1]; e {
		case '"', '\\':
			sb.WriteRune(e)
		case 'n':
			sb.WriteRune('\n')
		case 't':
			sb.WriteRune('\t')
		case 'r':
			sb.WriteRune('\r')
		default:
			l.diag(CodeInvalidEscape, Span{l.pos, l.pos + 2},
				"unknown escape \\"+string(e))
			sb.WriteRune(e) // recovery: keep the character, keep parsing
		}
		l.pos += 2
	}
}

func (l *lexer) emitString(value string, start, end int) {
	l.toks = append(l.toks, Token{
		Kind: STRING,
		Text: string(l.src[start:end]),
		Str:  value,
		Span: Span{start, end},
	})
}

func (l *lexer) emit(kind TokenKind, text string, start, end int) {
	l.toks = append(l.toks, Token{Kind: kind, Text: text, Span: Span{start, end}})
}

func (l *lexer) diag(code string, span Span, msg string) {
	l.diags = append(l.diags, Diagnostic{Code: code, Message: msg, Span: span})
}

func isSpace(c rune) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
func isDigit(c rune) bool { return c >= '0' && c <= '9' }

func isIdentStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c rune) bool { return isIdentStart(c) || isDigit(c) || c == '.' }
