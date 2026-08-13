package sluice_test

import (
	"strings"
	"testing"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

func TestLoadSchemaRejectsWhatSection4Forbids(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{
			"reserved field name",
			`{"fields":[{"name":"and","type":"string","column":"inv.a"}]}`,
			"reserved",
		},
		{
			"field name outside the pattern",
			`{"fields":[{"name":"my-field","type":"string","column":"inv.a"}]}`,
			"must match",
		},
		{
			"unknown type",
			`{"fields":[{"name":"a","type":"jsonb","column":"inv.a"}]}`,
			"unknown type",
		},
		{
			"no column and no emitter",
			`{"fields":[{"name":"a","type":"string"}]}`,
			"needs a column",
		},
		{
			"duplicate field",
			`{"fields":[{"name":"a","type":"string","column":"inv.a"},{"name":"A","type":"string","column":"inv.b"}]}`,
			"declared twice",
		},
		{
			"operator outside the type's set",
			`{"fields":[{"name":"a","type":"boolean","column":"inv.a","operators":["~"]}]}`,
			"which is not one of",
		},
		{
			"values on a non-enum",
			`{"fields":[{"name":"a","type":"string","column":"inv.a","values":["x"]}]}`,
			"cannot declare values",
		},
		{
			"sort without sql",
			`{"fields":[{"name":"a","type":"string","column":"inv.a"}],"sorts":[{"key":"a"}]}`,
			"needs an sql expression",
		},
		{
			"fallback field that does not exist",
			`{"options":{"fallbackFields":["nope"]},"fields":[{"name":"a","type":"string","column":"inv.a"}]}`,
			"not a declared field",
		},
		{
			"unknown key",
			`{"fields":[{"name":"a","type":"string","column":"inv.a","sql":"1=1"}]}`,
			"not valid JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sluice.LoadSchema([]byte(tc.json))
			if err == nil {
				t.Fatalf("schema was accepted, want %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadSchemaReportsEveryProblemAtOnce(t *testing.T) {
	_, err := sluice.LoadSchema([]byte(
		`{"fields":[{"name":"or","type":"string","column":"inv.a"},{"name":"b","type":"nope"}]}`))
	var se *sluice.SchemaError
	if !asSchemaError(err, &se) {
		t.Fatalf("error %v is not a *sluice.SchemaError", err)
	}
	if len(se.Diagnostics) < 2 {
		t.Errorf("diagnostics = %+v, want a host to see all of them at once", se.Diagnostics)
	}
	for _, d := range se.Diagnostics {
		if d.Code != "schema_invalid" {
			t.Errorf("code = %s, want schema_invalid", d.Code)
		}
	}
}

func asSchemaError(err error, target **sluice.SchemaError) bool {
	e, ok := err.(*sluice.SchemaError)
	if ok {
		*target = e
	}
	return ok
}

func TestNewRejectsAnInvalidSchema(t *testing.T) {
	_, err := sluice.New(sluice.Schema{
		Fields: []sluice.Field{{Name: "not", Type: sluice.TypeString, Column: "inv.a"}},
	}, postgres.Dialect)
	if err == nil {
		t.Fatal("New accepted a schema with a reserved field name")
	}
}

func TestExplicitOperatorsReplaceTheDefault(t *testing.T) {
	c, err := sluice.New(sluice.Schema{
		Fields: []sluice.Field{
			{Name: "name", Type: sluice.TypeString, Column: "inv.name", Operators: []string{"="}},
		},
	}, postgres.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile(`name ~ "x"`); err == nil {
		t.Error("~ was accepted although the schema permits only =")
	}
	if _, err := c.Compile(`name = "x"`); err != nil {
		t.Errorf("= was rejected: %v", err)
	}
}

func TestOrderByUsesSchemaExpressionsOnly(t *testing.T) {
	c := machines(t, postgres.Dialect)

	got, err := c.OrderBy("name", sluice.Asc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ORDER BY inv.name ASC NULLS LAST"; got != want {
		t.Errorf("order by = %q, want %q", got, want)
	}

	got, err = c.OrderBy("phase", sluice.Desc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ORDER BY inv.phase DESC NULLS LAST"; got != want {
		t.Errorf("order by = %q, want %q", got, want)
	}

	if got, err := c.OrderBy("", sluice.Asc); err != nil || got != "" {
		t.Errorf("empty key = %q, %v, want no clause and no error", got, err)
	}

	_, err = c.OrderBy("inv.name; DROP TABLE machine", sluice.Asc)
	if d := diagnosticOf(t, err); d.Code != "unknown_sort_key" {
		t.Errorf("diagnostic = %+v, want unknown_sort_key", d)
	}
}
