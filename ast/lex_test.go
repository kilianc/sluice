package ast

import "testing"

func kinds(toks []Token) []TokenKind {
	out := make([]TokenKind, 0, len(toks))
	for _, t := range toks {
		out = append(out, t.Kind)
	}
	return out
}

func TestLexOperatorsAreMatchedLongestFirst(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"a!=1", "!="},
		{"a!~1", "!~"},
		{"a<=1", "<="},
		{"a>=1", ">="},
		{"a<1", "<"},
		{"a=1", "="},
		{"a~1", "~"},
	} {
		toks, diags := Lex(tc.input)
		if len(diags) != 0 {
			t.Fatalf("%q: unexpected diagnostics %+v", tc.input, diags)
		}
		if got := toks[1].Text; got != tc.want {
			t.Errorf("%q: operator = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLexBareBangIsUnexpected(t *testing.T) {
	_, diags := Lex("a ! b")
	if len(diags) != 1 || diags[0].Code != CodeUnexpectedToken {
		t.Fatalf("diagnostics = %+v, want one unexpected_token", diags)
	}
	if diags[0].Span != (Span{2, 3}) {
		t.Errorf("span = %v, want [2,3)", diags[0].Span)
	}
}

func TestLexSpansArePerCodepointNotPerByte(t *testing.T) {
	// "é" is two bytes and one codepoint; the token after it must not shift.
	toks, _ := Lex(`name = "é" AND a`)
	last := toks[len(toks)-2]
	if last.Span != (Span{15, 16}) {
		t.Errorf("span of trailing ident = %v, want [15,16)", last.Span)
	}
}

func TestLexStringRecoversAndStillYieldsAToken(t *testing.T) {
	toks, diags := Lex(`name = "abc`)
	if len(diags) != 1 || diags[0].Code != CodeUnterminatedString {
		t.Fatalf("diagnostics = %+v, want one unterminated_string", diags)
	}
	if diags[0].Span != (Span{7, 11}) {
		t.Errorf("span = %v, want [7,11)", diags[0].Span)
	}
	// Recovery keeps parsing meaningful: the value is still available.
	if toks[2].Kind != STRING || toks[2].Str != "abc" {
		t.Errorf("token = %+v, want STRING abc", toks[2])
	}
}

func TestLexEscapes(t *testing.T) {
	toks, diags := Lex(`"a\"b\\c\nd\te\rf"`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics %+v", diags)
	}
	if want := "a\"b\\c\nd\te\rf"; toks[0].Str != want {
		t.Errorf("value = %q, want %q", toks[0].Str, want)
	}
}

func TestLexInvalidEscape(t *testing.T) {
	_, diags := Lex(`name = "a\qb"`)
	if len(diags) != 1 || diags[0].Code != CodeInvalidEscape {
		t.Fatalf("diagnostics = %+v, want one invalid_escape", diags)
	}
	if diags[0].Span != (Span{9, 11}) {
		t.Errorf("span = %v, want [9,11)", diags[0].Span)
	}
}

func TestLexSingleQuoteIsNeverAStringDelimiter(t *testing.T) {
	// A SQL quoting habit must not silently produce a valid-looking query.
	toks, diags := Lex(`name = 'x'`)
	if len(diags) != 2 {
		t.Fatalf("diagnostics = %+v, want two unexpected_token", diags)
	}
	for _, tok := range toks {
		if tok.Kind == STRING {
			t.Fatalf("single quotes produced a STRING token: %+v", tok)
		}
	}
}

func TestLexKeywordsAreReservedRegardlessOfCase(t *testing.T) {
	toks, _ := Lex("a AND b Or NOT true FALSE")
	want := []TokenKind{IDENT, AND, IDENT, OR, NOT, TRUE, FALSE, EOF}
	got := kinds(toks)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	if toks[1].Text != "AND" || toks[3].Text != "Or" {
		t.Errorf("keyword text should preserve the user's casing: %q %q", toks[1].Text, toks[3].Text)
	}
}

func TestLexNumbers(t *testing.T) {
	toks, diags := Lex("cores>=32 AND cores<-1.5")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics %+v", diags)
	}
	if toks[2].Num != 32 || toks[6].Num != -1.5 {
		t.Errorf("numbers = %v %v, want 32 -1.5", toks[2].Num, toks[6].Num)
	}
}

func TestLexNothingUnrecognizedEntersTheTokenStream(t *testing.T) {
	// Invariant 2 starts in the lexer: no passthrough token exists.
	toks, diags := Lex(`name = "x"; DROP TABLE machine -- c`)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for ; and --")
	}
	for _, tok := range toks {
		switch tok.Text {
		case ";", "-", "--":
			t.Errorf("unrecognized text became a token: %+v", tok)
		}
	}
}
