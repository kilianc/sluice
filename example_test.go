package sluice_test

import (
	"fmt"
	"log"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

// The schema is the whole configuration: one declaration drives the compiler
// and the editor. This is the example in the README, executable so the output
// it advertises cannot drift from what the compiler emits.
const exampleSchema = `{
  "name": "documents",
  "options": { "fallbackFields": ["name"] },
  "fields": [
    { "name": "name",  "type": "string",  "column": "doc.name",         "description": "Document title" },
    { "name": "state", "type": "enum",    "column": "doc.state", "values": ["shared", "restricted"] },
    { "name": "words", "type": "number",  "column": "doc.words" },
    { "name": "team",  "type": "enum",    "column": "grp.name",      "dynamic": true }
  ],
  "sorts": [ { "key": "name", "sql": "doc.name" } ]
}`

func Example() {
	schema, err := sluice.LoadSchema([]byte(exampleSchema))
	if err != nil {
		log.Fatal(err)
	}
	c, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		log.Fatal(err)
	}

	res, err := c.Compile(`state = "shared" AND team ~ "desi"`)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res.SQL)
	fmt.Println(res.Args)
	fmt.Println(res.Fields)

	// Output:
	// (LOWER(doc.state) = $1 AND LOWER(grp.name) LIKE $2 ESCAPE '\')
	// [shared %desi%]
	// [state team]
}

// Compile returns the first diagnostic and no SQL. Validate returns all of
// them, positioned, which is what an editor underlines.
func ExampleCompiler_Validate() {
	schema, _ := sluice.LoadSchema([]byte(exampleSchema))
	c, _ := sluice.New(schema, postgres.Dialect)

	for _, d := range c.Validate(`stat = "x" OR words ~ 4`) {
		fmt.Printf("%d:%d %s: %s\n", d.Span.Start, d.Span.End, d.Code, d.Message)
	}

	// Output:
	// 0:4 unknown_field: unknown field stat; did you mean state, team?
	// 20:21 unknown_operator_for_field: field words does not support ~; it supports = != < <= > >=
}

// Suggest works on input that does not parse, because that is the state an
// editor is in while someone types.
func ExampleCompiler_Suggest() {
	schema, _ := sluice.LoadSchema([]byte(exampleSchema))
	c, _ := sluice.New(schema, postgres.Dialect)

	for _, s := range c.Suggest(`state = `, 8) {
		fmt.Printf("%s (%s)\n", s.Text, s.Kind)
	}
	// A prefix that matches no field is wrapped into whole predicates against
	// the schema's fallback fields.
	for _, s := range c.Suggest(`web-1`, 5) {
		fmt.Printf("%s (%s)\n", s.Text, s.Kind)
	}

	// Output:
	// shared (value)
	// restricted (value)
	// name = "web-1" (expression)
	// name ~ "web-1" (expression)
}

// Dynamic enum values are supplied per request and are never cached on the
// compiler, which stays immutable and safe for concurrent use.
func ExampleCompiler_WithDynamic() {
	schema, _ := sluice.LoadSchema([]byte(exampleSchema))
	c, _ := sluice.New(schema, postgres.Dialect)

	req := c.WithDynamic(map[string][]string{"team": {"design-a", "design-b"}})
	res, err := req.Compile(`team = "design-a"`)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res.SQL, res.Args)

	order, err := c.OrderBy("name", sluice.Desc)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(order)

	// Output:
	// LOWER(grp.name) = $1 [design-a]
	// ORDER BY doc.name DESC NULLS LAST
}
