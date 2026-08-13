package ast

import (
	"encoding/json"
	"strings"
	"testing"
)

var testLimits = Limits{MaxDepth: 16, MaxPredicates: 64}

func parse(t *testing.T, input string) Result {
	t.Helper()
	return ParseString(input, testLimits)
}

func codes(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

func TestParseEmptyInputIsValid(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t"} {
		res := parse(t, in)
		if res.Node != nil {
			t.Errorf("%q parsed to %+v, want no expression", in, res.Node)
		}
		if len(res.Diagnostics) != 0 {
			t.Errorf("%q produced %v", in, codes(res.Diagnostics))
		}
	}
}

func TestParsePrecedenceAndAssociativity(t *testing.T) {
	res := parse(t, `a = "1" OR b = "2" AND c = "3"`)
	if res.Node.Kind != KindBinary || res.Node.Op != "or" {
		t.Fatalf("root = %+v, want an OR", res.Node)
	}
	if res.Node.Right.Op != "and" {
		t.Fatalf("right = %+v, want an AND", res.Node.Right)
	}

	res = parse(t, `a = "1" AND b = "2" AND c = "3"`)
	if res.Node.Left.Kind != KindBinary {
		t.Fatalf("AND should be left-associative, got %+v", res.Node)
	}
}

func TestParseNotBindsTighterThanAnd(t *testing.T) {
	res := parse(t, `NOT a = "1" AND b = "2"`)
	if res.Node.Kind != KindBinary || res.Node.Left.Kind != KindNot {
		t.Fatalf("tree = %+v, want (NOT a) AND b", res.Node)
	}
}

func TestParseBareIdentifierIsNotAValue(t *testing.T) {
	// The single rule that removes the injection surface (AGENTS.md §5).
	res := parse(t, `phase = in-use`)
	if len(res.Diagnostics) == 0 || res.Diagnostics[0].Code != CodeUnexpectedToken {
		t.Fatalf("diagnostics = %v, want unexpected_token first", codes(res.Diagnostics))
	}
	if res.Node != nil {
		t.Errorf("a rejected predicate must not produce a node: %+v", res.Node)
	}
}

func TestParseImplicitConjunctionIsNotSupported(t *testing.T) {
	res := parse(t, `a = "1" b = "2"`)
	if len(res.Diagnostics) == 0 || res.Diagnostics[0].Code != CodeUnexpectedToken {
		t.Fatalf("diagnostics = %v, want unexpected_token", codes(res.Diagnostics))
	}
}

func TestParseUnbalancedParens(t *testing.T) {
	res := parse(t, `(a = "1"`)
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != CodeUnbalancedParen {
		t.Fatalf("diagnostics = %v, want one unbalanced_paren", codes(res.Diagnostics))
	}
	if res.Diagnostics[0].Span != (Span{0, 1}) {
		t.Errorf("span = %v, want the opening paren", res.Diagnostics[0].Span)
	}

	res = parse(t, `a = "1")`)
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != CodeUnbalancedParen {
		t.Fatalf("diagnostics = %v, want one unbalanced_paren", codes(res.Diagnostics))
	}
}

func TestParseDepthIsBoundedBeforeRecursingFurther(t *testing.T) {
	res := ParseString(strings.Repeat("(", 40)+`a = "1"`, Limits{MaxDepth: 4, MaxPredicates: 64})
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != CodeDepthExceeded {
		t.Fatalf("diagnostics = %v, want one depth_exceeded", codes(res.Diagnostics))
	}
	if res.Diagnostics[0].Span != (Span{4, 5}) {
		t.Errorf("span = %v, want the paren that exceeded the limit", res.Diagnostics[0].Span)
	}
}

func TestParsePredicateCountIsBounded(t *testing.T) {
	input := strings.TrimSuffix(strings.Repeat(`a = "1" AND `, 5), " AND ")
	res := ParseString(input, Limits{MaxDepth: 16, MaxPredicates: 4})
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != CodeTooManyPredicates {
		t.Fatalf("diagnostics = %v, want one too_many_predicates", codes(res.Diagnostics))
	}
}

func TestParseRecoversAndReportsEveryProblem(t *testing.T) {
	// Recovery exists so an editor can underline everything in one pass.
	res := parse(t, `a = 1x OR b = ) OR c = "3"`)
	if len(res.Diagnostics) < 2 {
		t.Fatalf("diagnostics = %v, want more than one", codes(res.Diagnostics))
	}
}

func TestParseDiagnosticsAreOrderedByPosition(t *testing.T) {
	res := parse(t, `name = 'x' OR 1=1`)
	prev := -1
	for _, d := range res.Diagnostics {
		if d.Span.Start < prev {
			t.Fatalf("diagnostics are not ordered: %+v", res.Diagnostics)
		}
		prev = d.Span.Start
	}
}

func TestParseOrphanFieldsAreReportedForFailedPredicates(t *testing.T) {
	// EXISTS never becomes a predicate, but resolution still needs to see it so
	// the user gets unknown_field rather than only a syntax error further right.
	res := parse(t, `phase = "in-use" OR EXISTS (SELECT 1 FROM machine)`)
	if len(res.Orphans) != 1 || res.Orphans[0].Name != "exists" {
		t.Fatalf("orphans = %+v, want exists", res.Orphans)
	}
	if res.Orphans[0].Span != (Span{20, 26}) {
		t.Errorf("span = %v, want [20,26)", res.Orphans[0].Span)
	}
}

func TestParseFieldNamesNormalizeToLowercase(t *testing.T) {
	res := parse(t, `PHASE = "x"`)
	if res.Node.Field != "phase" {
		t.Errorf("field = %q, want phase", res.Node.Field)
	}
}

func TestASTJSONRoundTrip(t *testing.T) {
	res := parse(t, `NOT (a = "1" AND b > 3) OR c = true`)
	data, err := json.Marshal(res.Node)
	if err != nil {
		t.Fatal(err)
	}
	var back Node
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(&back)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(again) {
		t.Errorf("round trip changed the tree:\n%s\n%s", data, again)
	}
}

func TestASTEncodingIsTheNormativeShape(t *testing.T) {
	res := parse(t, `phase = "in-use"`)
	data, err := json.Marshal(res.Node)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"predicate","op":"=","field":"phase","value":{"type":"string","value":"in-use"},"span":[0,16]}`
	if string(data) != want {
		t.Errorf("encoding =\n%s\nwant\n%s", data, want)
	}
}

func TestASTDecoderRejectsWhatItDoesNotRecognize(t *testing.T) {
	// Decoding an untrusted AST is subject to the same rules as parsing text.
	for _, in := range []string{
		`{"kind":"raw","sql":"1=1"}`,
		`{"kind":"binary","op":"union","left":null,"right":null}`,
		`{"kind":"predicate","field":"a","op":"DROP","value":{"type":"string","value":"x"}}`,
		`{"kind":"predicate","field":"a","op":"=","value":{"type":"column","value":"inv.id"}}`,
		`{"kind":"predicate","op":"=","value":{"type":"string","value":"x"}}`,
		`{"kind":"not"}`,
		`{"kind":"predicate","field":"a","op":"=","value":{"type":"string","value":"x"},"sql":"1=1"}`,
	} {
		var n Node
		if err := json.Unmarshal([]byte(in), &n); err == nil {
			t.Errorf("decoder accepted %s", in)
		}
	}
}

func TestNodeDepth(t *testing.T) {
	res := parse(t, `a = "1" AND (b = "2" OR NOT c = "3")`)
	if got := res.Node.Depth(); got != 4 {
		t.Errorf("depth = %d, want 4", got)
	}
}
