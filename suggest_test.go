package sluice_test

import (
	"strings"
	"testing"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

func texts(sugg []sluice.Suggestion) string {
	out := make([]string, 0, len(sugg))
	for _, s := range sugg {
		out = append(out, s.Text)
	}
	return strings.Join(out, " ")
}

func TestSuggestWorksOnInputThatDoesNotParse(t *testing.T) {
	// An editor asks for completions precisely when the query is half-written.
	c := machines(t, postgres.Dialect)
	for _, tc := range []struct {
		input  string
		cursor int
		want   string
	}{
		{"phase = ", 8, "in-use not-in-use maintenance"},
		{"phase = \"in-use\" AND onl", 24, "online"},
		{"NOT ", 4, "cores id name online os_age phase rack"},
		{"(phase = \"in-use\" OR ", 21, "cores id name online os_age phase rack"},
		{"phase = \"in-use\" AND (", 22, "cores id name online os_age phase rack"},
	} {
		got := texts(c.Suggest(tc.input, tc.cursor))
		if got != tc.want {
			t.Errorf("Suggest(%q, %d) = %q, want %q", tc.input, tc.cursor, got, tc.want)
		}
	}
}

func TestSuggestFieldOrderIsExactThenPrefixThenSubstring(t *testing.T) {
	c := machines(t, postgres.Dialect)
	if got := texts(c.Suggest("o", 1)); got != "online os_age cores" {
		t.Errorf("Suggest(\"o\") = %q", got)
	}
	if got := texts(c.Suggest("name", 4)); got != "name" {
		t.Errorf("an exact match should come first: %q", got)
	}
	if got := texts(c.Suggest("ame", 3)); got != "name" {
		t.Errorf("substring matches should still be offered: %q", got)
	}
}

func TestSuggestCarriesTheFieldDescription(t *testing.T) {
	c := machines(t, postgres.Dialect)
	sugg := c.Suggest("ph", 2)
	if len(sugg) != 1 || sugg[0].Detail != "Lifecycle phase" {
		t.Errorf("suggestion = %+v, want the schema description as detail", sugg)
	}
}

func TestSuggestReplaceSpanCoversAnOpeningQuote(t *testing.T) {
	c := machines(t, postgres.Dialect)
	sugg := c.Suggest(`phase = "not`, 12)
	if len(sugg) != 1 {
		t.Fatalf("suggestions = %+v", sugg)
	}
	if sugg[0].ReplaceSpan != (sluice.Span{Start: 8, End: 12}) {
		t.Errorf("replaceSpan = %v, want [8,12)", sugg[0].ReplaceSpan)
	}
}

func TestSuggestClosingParenOnlyWhileOneIsOpen(t *testing.T) {
	c := machines(t, postgres.Dialect)
	if got := texts(c.Suggest(`phase = "in-use" `, 17)); got != "AND OR" {
		t.Errorf("suggestions = %q, want AND OR", got)
	}
	if got := texts(c.Suggest(`(phase = "in-use" `, 18)); got != "AND OR )" {
		t.Errorf("suggestions = %q, want AND OR )", got)
	}
	if got := texts(c.Suggest(`(phase = "in-use") `, 19)); got != "AND OR" {
		t.Errorf("suggestions = %q, want AND OR once the paren is closed", got)
	}
}

func TestSuggestBareValueFallback(t *testing.T) {
	c := machines(t, postgres.Dialect)
	got := texts(c.Suggest("web-1", 5))
	if want := `name = "web-1" name ~ "web-1"`; got != want {
		t.Errorf("suggestions = %q, want %q", got, want)
	}

	const id = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	got = texts(c.Suggest(id, len(id)))
	want := `id = "` + id + `" name = "` + id + `" name ~ "` + id + `"`
	if got != want {
		t.Errorf("uuid suggestions = %q, want %q", got, want)
	}
}

func TestSuggestFallbackQuotesTheValue(t *testing.T) {
	c := machines(t, postgres.Dialect)
	sugg := c.Suggest(`a"b`, 3)
	if len(sugg) == 0 || !strings.Contains(sugg[0].Text, `\"`) {
		t.Errorf("suggestion = %+v, want the inner quote escaped", sugg)
	}
}

func TestSuggestOperatorsPreserveDeclaredOrder(t *testing.T) {
	c := machines(t, postgres.Dialect)
	if got := texts(c.Suggest("phase ", 6)); got != "= != ~ !~" {
		t.Errorf("suggestions = %q, want the declared order", got)
	}
	if got := texts(c.Suggest("online ", 7)); got != "=" {
		t.Errorf("suggestions = %q, want only =", got)
	}
	if got := texts(c.Suggest("os_age ", 7)); got != "< <= > >=" {
		t.Errorf("suggestions = %q", got)
	}
}

func TestSuggestFreeTextValuesOfferNothing(t *testing.T) {
	c := machines(t, postgres.Dialect)
	if got := c.Suggest("name = ", 7); len(got) != 0 {
		t.Errorf("suggestions = %+v, want none", got)
	}
	if got := c.Suggest("cores > ", 8); len(got) != 0 {
		t.Errorf("suggestions = %+v, want none", got)
	}
}

func TestSuggestCursorIsClamped(t *testing.T) {
	c := machines(t, postgres.Dialect)
	if got := c.Suggest("phase", -5); len(got) == 0 {
		t.Error("a negative cursor should behave as 0")
	}
	if got := c.Suggest("phase", 500); len(got) == 0 {
		t.Error("a cursor past the end should behave as the end")
	}
}
